package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryruntimerepo"
)

type memoryRuntimeDB interface {
	Begin(context.Context) (pgx.Tx, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}
type MemoryRuntimeRepository struct {
	db      memoryRuntimeDB
	OwnerID id.ID
}

func NewMemoryRuntimeRepository(db memoryRuntimeDB) *MemoryRuntimeRepository {
	return &MemoryRuntimeRepository{db: db}
}

const profileColumns = `revision,config_revision_id,signature,provider_name,model_name,status`

func scanProfile(row pgx.Row) (repo.Profile, error) {
	var p repo.Profile
	err := row.Scan(&p.Revision, &p.ConfigID, &p.Signature, &p.Provider, &p.Model, &p.State)
	return p, err
}

func (r *MemoryRuntimeRepository) Ensure(ctx context.Context, config id.ID, signature, provider, model string, force bool) (repo.Profile, error) {
	if r.OwnerID.IsZero() || config.IsZero() || signature == "" || provider == "" || model == "" {
		return repo.Profile{}, errors.New("invalid memory profile")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return repo.Profile{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('memory-profile-lifecycle',0))`); err != nil {
		return repo.Profile{}, err
	}
	p, err := scanProfile(tx.QueryRow(ctx, `SELECT `+profileColumns+` FROM memory_index_profiles WHERE signature=$1 AND owner_id=$2 ORDER BY (status='building') DESC,revision DESC LIMIT 1`, signature, r.OwnerID))
	if err == nil && (p.State == "building" || !force) {
		return p, tx.Commit(ctx)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return repo.Profile{}, err
	}
	// Capture old Canon sources too. Fanout and creation share this lock.
	for _, sql := range []string{
		`INSERT INTO memory_index_jobs(story_id,timeline_id,source_kind,source_event_id,source_event_seq) SELECT t.story_id,e.timeline_id,'event',e.id,e.seq FROM story_events e JOIN timelines t ON t.id=e.timeline_id JOIN stories s ON s.id=t.story_id WHERE s.owner_id=$1 ON CONFLICT DO NOTHING`,
		`INSERT INTO memory_index_jobs(story_id,timeline_id,source_kind,source_snapshot_id,source_event_seq) SELECT t.story_id,s.timeline_id,'snapshot',s.id,s.event_seq FROM snapshots s JOIN timelines t ON t.id=s.timeline_id JOIN stories st ON st.id=t.story_id WHERE st.owner_id=$1 ON CONFLICT DO NOTHING`,
		`INSERT INTO memory_index_state(story_id,timeline_id,observed_event_seq) SELECT j.story_id,j.timeline_id,max(j.source_event_seq) FROM memory_index_jobs j JOIN stories s ON s.id=j.story_id WHERE j.index_revision IS NULL AND s.owner_id=$1 GROUP BY j.story_id,j.timeline_id ON CONFLICT(timeline_id) DO UPDATE SET observed_event_seq=GREATEST(memory_index_state.observed_event_seq,EXCLUDED.observed_event_seq)`,
	} {
		if _, err = tx.Exec(ctx, sql, r.OwnerID); err != nil {
			return repo.Profile{}, err
		}
	}
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('memory_index_profiles','revision'))`).Scan(&revision); err != nil {
		return repo.Profile{}, err
	}
	job, err := id.New()
	if err != nil {
		return repo.Profile{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO embedding_reindex_jobs(id,provider_name,model_name,target_revision,status) VALUES($1,$2,$3,$4,'running')`, job, provider, model, revision); err != nil {
		return repo.Profile{}, err
	}
	p, err = scanProfile(tx.QueryRow(ctx, `INSERT INTO memory_index_profiles(revision,config_revision_id,signature,provider_name,model_name,reindex_job_id,status,owner_id) VALUES($1,$2,$3,$4,$5,$6,'building',$7) RETURNING `+profileColumns, revision, config, signature, provider, model, job, r.OwnerID))
	if err != nil {
		return repo.Profile{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memory_index_jobs(story_id,timeline_id,source_kind,source_event_id,source_snapshot_id,source_event_seq,index_revision)
 SELECT j.story_id,j.timeline_id,j.source_kind,j.source_event_id,j.source_snapshot_id,j.source_event_seq,$1 FROM memory_index_jobs j JOIN stories s ON s.id=j.story_id WHERE j.index_revision IS NULL AND s.owner_id=$2 ON CONFLICT DO NOTHING`, revision, r.OwnerID); err != nil {
		return repo.Profile{}, err
	}
	return p, tx.Commit(ctx)
}
func (r *MemoryRuntimeRepository) Serving(ctx context.Context, sig string) (repo.Profile, error) {
	return scanProfile(r.db.QueryRow(ctx, `SELECT `+profileColumns+` FROM memory_index_profiles WHERE signature=$1 AND owner_id=$2 AND status='serving'`, sig, r.OwnerID))
}
func (r *MemoryRuntimeRepository) Work(ctx context.Context, sig string) ([]repo.Profile, error) {
	rows, err := r.db.Query(ctx, `SELECT `+profileColumns+` FROM memory_index_profiles WHERE signature=$1 AND owner_id=$2 AND status IN ('serving','building') ORDER BY (status='serving') DESC,revision`, sig, r.OwnerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []repo.Profile
	for rows.Next() {
		var p repo.Profile
		if err = rows.Scan(&p.Revision, &p.ConfigID, &p.Signature, &p.Provider, &p.Model, &p.State); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *MemoryRuntimeRepository) Finalize(ctx context.Context, revision int64) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('memory-profile-lifecycle',0))`); err != nil {
		return err
	}
	p, err := scanProfile(tx.QueryRow(ctx, `SELECT `+profileColumns+` FROM memory_index_profiles WHERE revision=$1 AND owner_id=$2 FOR UPDATE`, revision, r.OwnerID))
	if err != nil {
		return err
	}
	if p.State != "building" {
		return tx.Commit(ctx)
	}
	var pending, failed bool
	if err = tx.QueryRow(ctx, `SELECT COALESCE(bool_or(j.status IN ('queued','running')),false),COALESCE(bool_or(j.status='failed'),false)
 FROM memory_index_jobs j JOIN timelines t ON t.id=j.timeline_id JOIN stories s ON s.id=j.story_id WHERE j.index_revision=$1 AND t.status<>'deleted' AND s.status<>'deleted'`, revision).Scan(&pending, &failed); err != nil {
		return err
	}
	if failed {
		if _, err = tx.Exec(ctx, `UPDATE memory_index_profiles SET status='failed' WHERE revision=$1`, revision); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE embedding_reindex_jobs SET status='failed',completed_at=now() WHERE id=(SELECT reindex_job_id FROM memory_index_profiles WHERE revision=$1)`, revision); err != nil {
			return err
		}
	} else if !pending {
		if _, err = tx.Exec(ctx, `UPDATE memory_index_profiles SET status='retired' WHERE signature=$1 AND owner_id=$2 AND status='serving'`, p.Signature, r.OwnerID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE memory_index_profiles SET status='serving' WHERE revision=$1`, revision); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE embedding_reindex_jobs SET status='completed',completed_at=now() WHERE id=(SELECT reindex_job_id FROM memory_index_profiles WHERE revision=$1)`, revision); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (r *MemoryRuntimeRepository) Status(ctx context.Context, sig string) (repo.Status, error) {
	s := repo.Status{State: "idle"}
	err := r.db.QueryRow(ctx, `SELECT status,revision FROM memory_index_profiles WHERE signature=$1 AND owner_id=$2 ORDER BY (status='building') DESC,revision DESC LIMIT 1`, sig, r.OwnerID).Scan(&s.State, &s.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = r.db.QueryRow(ctx, `SELECT COALESCE(max(revision),0) FROM memory_index_profiles WHERE signature=$1 AND owner_id=$2 AND status='serving'`, sig, r.OwnerID).Scan(&s.ServingRevision); err != nil {
		return s, err
	}
	err = r.db.QueryRow(ctx, `SELECT count(*) FILTER(WHERE j.status='queued'),count(*) FILTER(WHERE j.status='running'),count(*) FILTER(WHERE j.status='completed'),count(*) FILTER(WHERE j.status='failed')
 FROM memory_index_jobs j JOIN timelines t ON t.id=j.timeline_id JOIN stories s ON s.id=j.story_id WHERE j.index_revision=$1 AND t.status<>'deleted' AND s.status<>'deleted'`, s.Revision).Scan(&s.Queued, &s.Running, &s.Completed, &s.Failed)
	if s.State == "serving" {
		if s.Failed > 0 {
			s.State = "degraded"
		} else if s.Queued+s.Running > 0 {
			s.State = "updating"
		}
	}
	return s, err
}
