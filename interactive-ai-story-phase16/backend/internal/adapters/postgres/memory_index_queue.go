package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	repo "github.com/local/interactive-ai-story/backend/internal/ports/memoryindexrepo"
)

type memoryQueueDB interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type MemoryIndexQueue struct {
	db memoryQueueDB
	// Optional restriction for a story-specific worker or reindex run.
	StoryID id.ID
}

func NewMemoryIndexQueue(db memoryQueueDB) *MemoryIndexQueue { return &MemoryIndexQueue{db: db} }

func (q *MemoryIndexQueue) Claim(ctx context.Context, owner string, now time.Time, ttl time.Duration) (repo.Job, error) {
	if strings.TrimSpace(owner) == "" || ttl <= 0 {
		return repo.Job{}, errors.New("memory worker owner and positive lease TTL required")
	}
	tx, err := q.db.Begin(ctx)
	if err != nil {
		return repo.Job{}, err
	}
	defer tx.Rollback(ctx)
	// Serialize short claim decisions so two workers cannot both observe an
	// idle timeline and index its versions out of order. No model call is held
	// under this lock/transaction.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('memory-index-claim',0))`); err != nil {
		return repo.Job{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE memory_index_jobs SET
 status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'queued' END,
 lease_owner=NULL,lease_token=NULL,lease_until=NULL,available_at=$1,
 completed_at=CASE WHEN attempts>=max_attempts THEN $1::timestamptz ELSE NULL END,
 last_error='lease expired'
 WHERE status='running' AND lease_until<=$1 AND (NULLIF($2,'')::uuid IS NULL OR story_id=NULLIF($2,'')::uuid)`, now, q.StoryID.String()); err != nil {
		return repo.Job{}, err
	}
	var job repo.Job
	err = tx.QueryRow(ctx, `WITH candidate AS (
 SELECT j.id FROM memory_index_jobs j
 JOIN timelines t ON t.id=j.timeline_id JOIN stories s ON s.id=j.story_id
 WHERE j.status='queued' AND j.available_at<=$1 AND j.attempts<j.max_attempts
 AND (NULLIF($4,'')::uuid IS NULL OR j.story_id=NULLIF($4,'')::uuid)
 AND t.status<>'deleted' AND s.status<>'deleted'
 AND NOT EXISTS(SELECT 1 FROM memory_index_jobs active WHERE active.timeline_id=j.timeline_id AND active.status='running')
 AND NOT EXISTS(SELECT 1 FROM memory_index_jobs preceding WHERE preceding.timeline_id=j.timeline_id
  AND preceding.status<>'completed'
  AND ROW(preceding.source_event_seq,preceding.created_at,preceding.id)<ROW(j.source_event_seq,j.created_at,j.id))
 ORDER BY j.created_at,j.id FOR UPDATE OF j SKIP LOCKED LIMIT 1
)
UPDATE memory_index_jobs j SET status='running',attempts=attempts+1,lease_owner=$2,lease_token=gen_random_uuid(),lease_until=$3,completed_at=NULL
FROM candidate c WHERE j.id=c.id
RETURNING j.id::text,j.story_id::text,j.timeline_id::text,j.source_kind,COALESCE(j.source_event_id,j.source_snapshot_id)::text,
 j.source_event_seq,j.attempts,j.max_attempts,j.lease_owner,j.lease_token::text,j.lease_until`, now, owner, now.Add(ttl), q.StoryID.String()).Scan(
		&job.ID, &job.StoryID, &job.TimelineID, &job.SourceKind, &job.SourceID, &job.SourceEventSeq, &job.Attempts, &job.MaxAttempts, &job.LeaseOwner, &job.LeaseToken, &job.LeaseUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		// Expired terminal jobs must remain recovered even if no new job exists.
		if err = tx.Commit(ctx); err != nil {
			return repo.Job{}, err
		}
		return repo.Job{}, repo.ErrNoJob
	}
	if err != nil {
		return repo.Job{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return repo.Job{}, err
	}
	return job, nil
}

func leaseResult(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return repo.ErrLeaseLost
	}
	return nil
}

func (q *MemoryIndexQueue) Heartbeat(ctx context.Context, job repo.Job, now time.Time, ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("positive memory lease TTL required")
	}
	tag, err := q.db.Exec(ctx, `UPDATE memory_index_jobs SET lease_until=$5 WHERE id=$1 AND lease_owner=$2 AND lease_token=$3 AND status='running' AND lease_until>$4`, job.ID, job.LeaseOwner, job.LeaseToken, now, now.Add(ttl))
	return leaseResult(tag, err)
}

func (q *MemoryIndexQueue) Complete(ctx context.Context, job repo.Job, now time.Time) error {
	tag, err := q.db.Exec(ctx, `UPDATE memory_index_jobs SET status='completed',completed_at=$4,lease_owner=NULL,lease_token=NULL,lease_until=NULL,last_error=''
 WHERE id=$1 AND lease_owner=$2 AND lease_token=$3 AND status='running' AND lease_until>$4`, job.ID, job.LeaseOwner, job.LeaseToken, now)
	return leaseResult(tag, err)
}

func (q *MemoryIndexQueue) Retry(ctx context.Context, job repo.Job, now time.Time, backoff time.Duration, detail string) error {
	if backoff < 0 {
		return errors.New("negative memory retry backoff")
	}
	tag, err := q.db.Exec(ctx, `UPDATE memory_index_jobs SET
 status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'queued' END,
 completed_at=CASE WHEN attempts>=max_attempts THEN $4::timestamptz ELSE NULL END,
 available_at=$5,last_error=left($6,2000),lease_owner=NULL,lease_token=NULL,lease_until=NULL
 WHERE id=$1 AND lease_owner=$2 AND lease_token=$3 AND status='running' AND lease_until>$4`, job.ID, job.LeaseOwner, job.LeaseToken, now, now.Add(backoff), detail)
	return leaseResult(tag, err)
}

var _ repo.Queue = (*MemoryIndexQueue)(nil)
