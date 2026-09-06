package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/local/interactive-ai-story/backend/internal/domain/generation"
	"github.com/local/interactive-ai-story/backend/internal/domain/id"
	ai "github.com/local/interactive-ai-story/backend/internal/ports/ai"
	"github.com/local/interactive-ai-story/backend/internal/ports/jobqueue"
	"time"
)

type JobQueue struct{ pool *pgxpool.Pool }

func NewJobQueue(p *pgxpool.Pool) *JobQueue { return &JobQueue{pool: p} }

func (q *JobQueue) Enqueue(ctx context.Context, j generation.Job, maxAttempts int) error {
	if maxAttempts < 1 {
		maxAttempts = 3
	}
	_, e := q.pool.Exec(ctx, `INSERT INTO generation_jobs(
 id,story_id,timeline_id,expected_head_event_seq,config_revision_id,prompt_set_revision_id,status,provider_kind,provider_name,model_name,profile_name,request_id,max_attempts,available_at,request_payload,action_hash)
 VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,NULLIF($6,'')::uuid,'queued',$7,$8,$9,$10,$11,$12,now(),$13,$14)`,
		j.ID, j.StoryID, j.TimelineID, j.ExpectedHeadEventSeq, j.ConfigRevisionID, j.PromptSetRevisionID, string(j.Provider.Kind), j.Provider.Provider, j.Provider.Model, j.Provider.Profile, j.RequestID, maxAttempts, []byte(j.RequestPayload), j.ActionHash)
	return e
}

func (q *JobQueue) ClaimNext(ctx context.Context, owner string, now time.Time, ttl time.Duration) (jobqueue.ClaimedJob, error) {
	tx, e := q.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return jobqueue.ClaimedJob{}, e
	}
	defer tx.Rollback(ctx)
	var out jobqueue.ClaimedJob
	var timeline *id.ID
	var cancelAt *time.Time
	var leaseExpires time.Time
	var lastError *string
	// Multiple hosted workers may claim concurrently, but claim decisions are
	// serialized briefly so two local jobs cannot both observe an idle single-GPU
	// provider before either row becomes running.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('story-generation-provider-claim',0))`); e != nil {
		return jobqueue.ClaimedJob{}, e
	}

	// Collapse accidental double-click/backlog duplicates before doing any LLM
	// work. For the same timeline/head only the oldest queued intent survives.
	if _, e = tx.Exec(ctx, `WITH ranked AS (
 SELECT id,row_number() OVER (PARTITION BY timeline_id,expected_head_event_seq ORDER BY created_at,id) AS rn
 FROM generation_jobs
 WHERE status='queued' AND timeline_id IS NOT NULL
), cancelled AS (
UPDATE generation_jobs j
SET status='cancelled',completed_at=$1::timestamptz,error_code='superseded_duplicate',last_error='duplicate queued action for the same timeline head'
FROM ranked r WHERE j.id=r.id AND r.rn>1
RETURNING j.id
)
INSERT INTO generation_updates(generation_job_id,sequence,phase,text_delta,error_text)
SELECT cancelled.id,COALESCE(MAX(generation_updates.sequence),0)+1,'failed','','duplicate queued action for the same timeline head'
FROM cancelled LEFT JOIN generation_updates ON generation_updates.generation_job_id=cancelled.id
GROUP BY cancelled.id`, now); e != nil {
		return jobqueue.ClaimedJob{}, e
	}

	// Drop queued actions whose optimistic-concurrency head is already stale.
	// This prevents old button clicks from consuming LLM time after another action
	// has already advanced the timeline.
	if _, e = tx.Exec(ctx, `WITH cancelled AS (
 UPDATE generation_jobs j
 SET status='cancelled',completed_at=$1::timestamptz,error_code='stale_head',last_error='timeline head moved before execution'
 FROM timelines t
 WHERE j.timeline_id=t.id AND j.status='queued' AND j.expected_head_event_seq<>t.head_event_seq
 RETURNING j.id
)
INSERT INTO generation_updates(generation_job_id,sequence,phase,text_delta,error_text)
SELECT cancelled.id,COALESCE(MAX(generation_updates.sequence),0)+1,'failed','','timeline head moved before execution'
FROM cancelled LEFT JOIN generation_updates ON generation_updates.generation_job_id=cancelled.id
GROUP BY cancelled.id`, now); e != nil {
		return jobqueue.ClaimedJob{}, e
	}

	e = tx.QueryRow(ctx, `
 WITH candidate AS (
  SELECT id FROM generation_jobs
  WHERE status='queued' AND available_at <= $1 AND attempt_count < max_attempts
    AND (provider_name='google_gemini' OR NOT EXISTS (
      SELECT 1 FROM generation_jobs active
      WHERE active.status='running' AND active.provider_name<>'google_gemini'
    ))
  ORDER BY created_at
  FOR UPDATE SKIP LOCKED
  LIMIT 1
 )
 UPDATE generation_jobs j
 SET status='running',attempt_count=attempt_count+1,lease_owner=$2,lease_expires_at=$3,heartbeat_at=$1,started_at=COALESCE(started_at,$1)
 FROM candidate c WHERE j.id=c.id
 RETURNING j.id,j.story_id,j.timeline_id,j.expected_head_event_seq,j.config_revision_id,COALESCE(j.prompt_set_revision_id::text,''),j.status,
 j.provider_kind,j.provider_name,j.model_name,j.profile_name,j.request_id,j.created_at,
 j.attempt_count,j.max_attempts,j.available_at,j.lease_expires_at,j.cancel_requested_at,j.last_error,j.request_payload,j.action_hash`,
		now, owner, now.Add(ttl)).
		Scan(&out.Job.ID, &out.Job.StoryID, &timeline, &out.Job.ExpectedHeadEventSeq, &out.Job.ConfigRevisionID, &out.Job.PromptSetRevisionID, &out.Job.Status,
			&out.Job.Provider.Kind, &out.Job.Provider.Provider, &out.Job.Provider.Model, &out.Job.Provider.Profile, &out.Job.RequestID, &out.Job.CreatedAt,
			&out.Reliability.AttemptCount, &out.Reliability.MaxAttempts, &out.Reliability.AvailableAt, &leaseExpires, &cancelAt, &lastError, &out.Job.RequestPayload, &out.Job.ActionHash)
	if errors.Is(e, pgx.ErrNoRows) {
		return jobqueue.ClaimedJob{}, jobqueue.ErrNoJob
	}
	if e != nil {
		return jobqueue.ClaimedJob{}, e
	}
	if timeline != nil {
		out.Job.TimelineID = *timeline
	}
	out.Job.Provider.Kind = ai.ProviderKind(out.Job.Provider.Kind)
	out.Reliability.Lease = &generation.Lease{Owner: owner, ExpiresAt: leaseExpires}
	out.Reliability.CancelRequestedAt = cancelAt
	if lastError != nil {
		out.Reliability.LastError = *lastError
	}
	if e = tx.Commit(ctx); e != nil {
		return jobqueue.ClaimedJob{}, e
	}
	return out, nil
}

func (q *JobQueue) Heartbeat(ctx context.Context, jid id.ID, owner string, now time.Time, ttl time.Duration) error {
	tag, e := q.pool.Exec(ctx, `UPDATE generation_jobs SET lease_expires_at=$4,heartbeat_at=$3 WHERE id=$1 AND status='running' AND lease_owner=$2 AND lease_expires_at>$3`, jid, owner, now, now.Add(ttl))
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return generation.ErrLeaseLost
	}
	return nil
}
func (q *JobQueue) Complete(ctx context.Context, jid id.ID, owner string, now time.Time) error {
	tag, e := q.pool.Exec(ctx, `UPDATE generation_jobs SET
status=CASE WHEN cancel_requested_at IS NULL THEN 'completed' ELSE 'cancelled' END,
completed_at=$3::timestamptz,lease_owner=NULL,lease_expires_at=NULL,heartbeat_at=$3::timestamptz,
error_code=CASE WHEN cancel_requested_at IS NULL THEN NULL ELSE error_code END,
last_error=CASE WHEN cancel_requested_at IS NULL THEN NULL ELSE last_error END
WHERE id=$1 AND status='running' AND lease_owner=$2`, jid, owner, now)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return generation.ErrLeaseLost
	}
	return nil
}
func (q *JobQueue) FailOrRetry(ctx context.Context, jid id.ID, owner string, now time.Time, backoff time.Duration, msg string) error {
	tag, e := q.pool.Exec(ctx, `UPDATE generation_jobs SET
 status=CASE WHEN cancel_requested_at IS NOT NULL THEN 'cancelled' WHEN attempt_count>=max_attempts THEN 'failed' ELSE 'queued' END,
 available_at=CASE WHEN attempt_count>=max_attempts THEN available_at ELSE $4 END,
 completed_at=CASE WHEN cancel_requested_at IS NOT NULL OR attempt_count>=max_attempts THEN $3::timestamptz ELSE NULL::timestamptz END,
 lease_owner=NULL,lease_expires_at=NULL,heartbeat_at=$3::timestamptz,last_error=$5,error_code=CASE WHEN attempt_count>=max_attempts THEN 'attempts_exhausted' ELSE error_code END
 WHERE id=$1 AND status='running' AND lease_owner=$2`, jid, owner, now, now.Add(backoff), msg)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return generation.ErrLeaseLost
	}
	return nil
}
func (q *JobQueue) RequestCancel(ctx context.Context, jid id.ID, now time.Time) error {
	_, e := q.pool.Exec(ctx, `UPDATE generation_jobs SET cancel_requested_at=COALESCE(cancel_requested_at,$2),
 status=CASE WHEN status='queued' THEN 'cancelled' ELSE status END,
 completed_at=CASE WHEN status='queued' THEN $2::timestamptz ELSE completed_at END
 WHERE id=$1 AND status IN('queued','running')`, jid, now)
	return e
}
func (q *JobQueue) IsCancelRequested(ctx context.Context, jid id.ID) (bool, error) {
	var yes bool
	e := q.pool.QueryRow(ctx, `SELECT cancel_requested_at IS NOT NULL FROM generation_jobs WHERE id=$1`, jid).Scan(&yes)
	return yes, e
}
func (q *JobQueue) RequeueExpired(ctx context.Context, now time.Time) (int64, error) {
	tag, e := q.pool.Exec(ctx, `UPDATE generation_jobs SET status=CASE WHEN cancel_requested_at IS NOT NULL THEN 'cancelled' WHEN attempt_count>=max_attempts THEN 'failed' ELSE 'queued' END,
 lease_owner=NULL,lease_expires_at=NULL,available_at=$1::timestamptz,completed_at=CASE WHEN cancel_requested_at IS NOT NULL OR attempt_count>=max_attempts THEN $1::timestamptz ELSE NULL::timestamptz END,
 error_code=CASE WHEN attempt_count>=max_attempts THEN 'attempts_exhausted' ELSE error_code END,last_error=COALESCE(last_error,'lease expired')
 WHERE status='running' AND lease_expires_at <= $1`, now)
	if e != nil {
		return 0, e
	}
	return tag.RowsAffected(), nil
}
func (q *JobQueue) FindByRequest(ctx context.Context, timelineID id.ID, key string) (jobqueue.ExistingJob, bool, error) {
	if timelineID.IsZero() || key == "" {
		return jobqueue.ExistingJob{}, false, nil
	}
	var existing jobqueue.ExistingJob
	e := q.pool.QueryRow(ctx, `SELECT id,action_hash FROM generation_jobs WHERE timeline_id=$1 AND request_id=$2 ORDER BY created_at LIMIT 1`, timelineID, key).Scan(&existing.ID, &existing.ActionHash)
	if errors.Is(e, pgx.ErrNoRows) {
		return jobqueue.ExistingJob{}, false, nil
	}
	if e != nil {
		return jobqueue.ExistingJob{}, false, e
	}
	return existing, true, nil
}

func (q *JobQueue) FindActiveByTimelineHead(ctx context.Context, timelineID id.ID, expectedHead int64) (jobqueue.ExistingJob, bool, error) {
	if timelineID.IsZero() || expectedHead < 0 {
		return jobqueue.ExistingJob{}, false, nil
	}
	var existing jobqueue.ExistingJob
	e := q.pool.QueryRow(ctx, `SELECT id,action_hash FROM generation_jobs
WHERE timeline_id=$1 AND expected_head_event_seq=$2 AND status IN ('queued','running')
ORDER BY created_at,id LIMIT 1`, timelineID, expectedHead).Scan(&existing.ID, &existing.ActionHash)
	if errors.Is(e, pgx.ErrNoRows) {
		return jobqueue.ExistingJob{}, false, nil
	}
	if e != nil {
		return jobqueue.ExistingJob{}, false, e
	}
	return existing, true, nil
}
