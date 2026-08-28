-- +goose Up
-- A lost HTTP response or a very fast double tap must never create two active
-- continuations for the same Canon head. Retire any pre-existing duplicates
-- before installing the invariant.
WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY timeline_id, expected_head_event_seq
               ORDER BY created_at, id
           ) AS position
    FROM generation_jobs
    WHERE timeline_id IS NOT NULL
      AND status IN ('queued', 'running')
), cancelled AS (
    UPDATE generation_jobs AS job
    SET status = 'cancelled',
        completed_at = now(),
        lease_owner = NULL,
        lease_expires_at = NULL,
        heartbeat_at = now(),
        error_code = 'superseded_duplicate',
        last_error = 'duplicate active action for the same timeline head'
    FROM ranked
    WHERE job.id = ranked.id
      AND ranked.position > 1
    RETURNING job.id
)
INSERT INTO generation_updates(generation_job_id, sequence, phase, text_delta, error_text)
SELECT cancelled.id,
       COALESCE(MAX(generation_updates.sequence), 0) + 1,
       'failed',
       '',
       'duplicate active action for the same timeline head'
FROM cancelled
LEFT JOIN generation_updates ON generation_updates.generation_job_id = cancelled.id
GROUP BY cancelled.id;

CREATE UNIQUE INDEX generation_jobs_one_active_head_idx
ON generation_jobs(timeline_id, expected_head_event_seq)
WHERE timeline_id IS NOT NULL AND status IN ('queued', 'running');

-- +goose Down
DROP INDEX IF EXISTS generation_jobs_one_active_head_idx;
