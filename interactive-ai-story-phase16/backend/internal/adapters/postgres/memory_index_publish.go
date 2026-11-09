package postgres

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	domain "github.com/local/interactive-ai-story/backend/internal/domain/memory"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryindexrepo"
)

func validMemoryProfile(profile repo.Profile) bool {
	return strings.TrimSpace(profile.Provider) != "" && strings.TrimSpace(profile.Model) != "" && profile.Revision > 0
}

func (s *MemoryIndexStore) Changed(ctx context.Context, job repo.Job, profile repo.Profile, documents []domain.Document) ([]domain.Document, error) {
	if !validMemoryProfile(profile) {
		return nil, errors.New("invalid memory embedding profile")
	}
	keys := make([]string, len(documents))
	for index, document := range documents {
		keys[index] = document.Key
	}
	rows, err := s.db.Query(ctx, `SELECT source_key,content_hash FROM memory_embeddings
 WHERE timeline_id=$1 AND embedding_provider=$2 AND embedding_model=$3 AND embedding_revision=$4
 AND source_key=ANY($5::text[]) AND valid_to_event_seq IS NULL`, job.TimelineID, profile.Provider, profile.Model, profile.Revision, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	current := map[string]string{}
	for rows.Next() {
		var key, hash string
		if err = rows.Scan(&key, &hash); err != nil {
			return nil, err
		}
		current[key] = hash
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var changed []domain.Document
	for _, document := range documents {
		if current[document.Key] != document.Hash() {
			changed = append(changed, document)
		}
	}
	return changed, nil
}

type activeMemory struct {
	hash  string
	from  int64
	class string
}

func validMemoryVector(vector []float32) bool {
	if len(vector) != 384 {
		return false
	}
	norm := float64(0)
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return false
		}
		norm += float64(value) * float64(value)
	}
	return norm > 0
}

func (s *MemoryIndexStore) Publish(ctx context.Context, job repo.Job, profile repo.Profile, documents []domain.Document, vectors map[string][]float32, now time.Time) error {
	if !validMemoryProfile(profile) {
		return errors.New("invalid memory embedding profile")
	}
	wanted := map[string]domain.Document{}
	keys := make([]string, 0, len(documents))
	for _, document := range documents {
		if document.Key == "" || document.Content == "" || document.Kind == "" || document.FromSeq != job.SourceEventSeq || document.OwnerID.IsZero() ||
			(document.Class != "branch_state" && document.Class != "historical_beat") ||
			(document.OwnerType != "timeline" && document.OwnerType != "character") ||
			(document.OwnerType == "timeline" && document.OwnerID != id.ID(job.TimelineID)) {
			return errors.New("invalid memory document scope")
		}
		if _, exists := wanted[document.Key]; exists {
			return errors.New("duplicate memory document key")
		}
		wanted[document.Key] = document
		keys = append(keys, document.Key)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status, owner, token string
	var until *time.Time
	var sourceSeq int64
	var indexRevision int64
	if err = tx.QueryRow(ctx, `SELECT status,COALESCE(lease_owner,''),COALESCE(lease_token::text,''),lease_until,source_event_seq,COALESCE(index_revision,0)
 FROM memory_index_jobs WHERE id=$1 AND story_id=$2 AND timeline_id=$3 AND source_kind=$4 AND COALESCE(source_event_id,source_snapshot_id)=$5 FOR UPDATE`, job.ID, job.StoryID, job.TimelineID, job.SourceKind, job.SourceID).Scan(&status, &owner, &token, &until, &sourceSeq, &indexRevision); err != nil {
		return err
	}
	if indexRevision != job.IndexRevision || (indexRevision > 0 && indexRevision != profile.Revision) {
		return repo.ErrLeaseLost
	}
	if status == "completed" {
		var matches bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memory_index_publications WHERE job_id=$1 AND provider_name=$2 AND model_name=$3 AND embedding_revision=$4 AND event_seq=$5)`, job.ID, profile.Provider, profile.Model, profile.Revision, job.SourceEventSeq).Scan(&matches); err != nil {
			return err
		}
		if matches {
			return tx.Commit(ctx)
		}
		return repo.ErrLeaseLost
	}
	if indexRevision > 0 {
		var matches bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memory_index_profiles WHERE revision=$1 AND provider_name=$2 AND model_name=$3 AND status IN ('building','serving'))`, indexRevision, profile.Provider, profile.Model).Scan(&matches); err != nil {
			return err
		}
		if !matches {
			return repo.ErrLeaseLost
		}
	}
	if status != "running" || owner != job.LeaseOwner || token != job.LeaseToken.String() || until == nil || !until.After(now) || sourceSeq != job.SourceEventSeq {
		return repo.ErrLeaseLost
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "memory-publish:"+string(job.TimelineID)+":"+profile.Provider+":"+profile.Model); err != nil {
		return err
	}
	var published int64 = -1
	err = tx.QueryRow(ctx, `SELECT published_event_seq FROM memory_index_publication_state WHERE timeline_id=$1 AND provider_name=$2 AND model_name=$3 AND embedding_revision=$4`, job.TimelineID, profile.Provider, profile.Model, profile.Revision).Scan(&published)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT source_key,content_hash,valid_from_event_seq,document_class FROM memory_embeddings
 WHERE timeline_id=$1 AND embedding_provider=$2 AND embedding_model=$3 AND embedding_revision=$4
 AND source_key IS NOT NULL AND valid_to_event_seq IS NULL AND (document_class='branch_state' OR source_key=ANY($5::text[]))`, job.TimelineID, profile.Provider, profile.Model, profile.Revision, keys)
	if err != nil {
		return err
	}
	active := map[string]activeMemory{}
	for rows.Next() {
		var key string
		var value activeMemory
		if err = rows.Scan(&key, &value.hash, &value.from, &value.class); err != nil {
			rows.Close()
			return err
		}
		if _, exists := active[key]; exists {
			rows.Close()
			return errors.New("duplicate active memory source")
		}
		active[key] = value
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if published <= job.SourceEventSeq {
		for key, value := range active {
			document, present := wanted[key]
			if present && document.Hash() == value.hash {
				continue
			}
			// Historical event text never expires because a later state snapshot
			// lacks that event. Only versioned branch-state rows are closed.
			if value.class != "branch_state" {
				continue
			}
			if value.from >= job.SourceEventSeq {
				return errors.New("conflicting memory state at the same sequence")
			}
			if _, err = tx.Exec(ctx, `UPDATE memory_embeddings SET valid_to_event_seq=$6 WHERE timeline_id=$1 AND embedding_provider=$2 AND embedding_model=$3 AND embedding_revision=$4 AND source_key=$5 AND valid_to_event_seq IS NULL`, job.TimelineID, profile.Provider, profile.Model, profile.Revision, key, job.SourceEventSeq-1); err != nil {
				return err
			}
		}
	}
	for _, document := range documents {
		if document.Class == "branch_state" && published > job.SourceEventSeq {
			continue
		}
		if previous, exists := active[document.Key]; exists && previous.hash == document.Hash() {
			continue
		}
		if previous, exists := active[document.Key]; exists && previous.class == "historical_beat" {
			return errors.New("historical memory source is immutable")
		}
		vector, exists := vectors[document.Key]
		if !exists || !validMemoryVector(vector) {
			return errors.New("missing or invalid memory embedding")
		}
		memoryID, err := id.New()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO memory_embeddings(id,story_id,timeline_id,owner_type,owner_id,kind,content,embedding,embedding_provider,embedding_model,embedding_revision,valid_from_event_seq,source_key,document_class,content_hash,index_job_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8::vector,$9,$10,$11,$12,$13,$14,$15,$16)`, memoryID, job.StoryID, job.TimelineID, document.OwnerType, document.OwnerID, document.Kind, document.Content, vectorLiteral(vector), profile.Provider, profile.Model, profile.Revision, document.FromSeq, document.Key, document.Class, document.Hash(), job.ID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memory_index_publications(job_id,timeline_id,provider_name,model_name,embedding_revision,event_seq,published_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, job.ID, job.TimelineID, profile.Provider, profile.Model, profile.Revision, job.SourceEventSeq, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO memory_index_publication_state(timeline_id,provider_name,model_name,embedding_revision,published_event_seq) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(timeline_id,provider_name,model_name,embedding_revision) DO UPDATE SET published_event_seq=GREATEST(memory_index_publication_state.published_event_seq,EXCLUDED.published_event_seq)`, job.TimelineID, profile.Provider, profile.Model, profile.Revision, job.SourceEventSeq); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE memory_index_jobs SET status='completed',completed_at=$4,lease_owner=NULL,lease_token=NULL,lease_until=NULL,last_error=''
 WHERE id=$1 AND lease_owner=$2 AND lease_token=$3 AND status='running' AND lease_until>$4 AND lease_until>clock_timestamp()`, job.ID, job.LeaseOwner, job.LeaseToken, now)
	if err = leaseResult(tag, err); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var _ repo.Store = (*MemoryIndexStore)(nil)
