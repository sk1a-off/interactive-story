-- +goose Up
-- Inline setup generation used the request context as its lifecycle. Browser
-- refreshes therefore left historical rows in running state. Retire those
-- rows before adding the per-story invariant.
UPDATE generation_jobs
SET status = 'failed',
    completed_at = now(),
    lease_owner = NULL,
    lease_expires_at = NULL,
    heartbeat_at = now(),
    error_code = 'interrupted_setup_generation',
    last_error = 'setup generation was interrupted before durable phase tracking'
WHERE timeline_id IS NULL
  AND provider_kind = 'story_llm'
  AND status = 'running';

CREATE UNIQUE INDEX generation_jobs_one_active_setup_idx
ON generation_jobs(story_id)
WHERE timeline_id IS NULL
  AND provider_kind = 'story_llm'
  AND status = 'running';

-- +goose Down
DROP INDEX IF EXISTS generation_jobs_one_active_setup_idx;
