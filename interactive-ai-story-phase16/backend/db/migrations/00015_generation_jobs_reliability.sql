-- +goose Up
ALTER TABLE generation_jobs
    ADD CONSTRAINT generation_jobs_timeline_story_fk
    FOREIGN KEY (timeline_id,story_id) REFERENCES timelines(id,story_id);

ALTER TABLE generation_jobs
    ADD COLUMN attempt_count integer NOT NULL DEFAULT 0 CHECK(attempt_count >= 0),
    ADD COLUMN max_attempts integer NOT NULL DEFAULT 3 CHECK(max_attempts > 0),
    ADD COLUMN available_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN lease_owner text NULL,
    ADD COLUMN lease_expires_at timestamptz NULL,
    ADD COLUMN cancel_requested_at timestamptz NULL,
    ADD COLUMN heartbeat_at timestamptz NULL,
    ADD COLUMN last_error text NULL,
    ADD COLUMN request_payload jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE UNIQUE INDEX generation_jobs_request_idempotency_idx
ON generation_jobs(timeline_id,request_id)
WHERE timeline_id IS NOT NULL AND request_id <> '';

CREATE INDEX generation_jobs_claim_idx
ON generation_jobs(status, available_at, created_at)
WHERE status IN ('queued','running');

CREATE INDEX generation_jobs_lease_idx
ON generation_jobs(lease_expires_at)
WHERE status='running';

ALTER TABLE generation_jobs
    ADD CONSTRAINT generation_jobs_lease_shape CHECK (
      (status='running' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
      OR status<>'running'
    );

-- Existing immutable provenance trigger intentionally does not include lease,
-- status, retry or cancellation columns: operational lifecycle is mutable,
-- provenance is not.

-- +goose Down
ALTER TABLE generation_jobs DROP CONSTRAINT IF EXISTS generation_jobs_lease_shape;
ALTER TABLE generation_jobs DROP CONSTRAINT IF EXISTS generation_jobs_timeline_story_fk;
DROP INDEX IF EXISTS generation_jobs_lease_idx;
DROP INDEX IF EXISTS generation_jobs_claim_idx;
DROP INDEX IF EXISTS generation_jobs_request_idempotency_idx;
ALTER TABLE generation_jobs
    DROP COLUMN IF EXISTS request_payload,
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS heartbeat_at,
    DROP COLUMN IF EXISTS cancel_requested_at,
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS lease_owner,
    DROP COLUMN IF EXISTS available_at,
    DROP COLUMN IF EXISTS max_attempts,
    DROP COLUMN IF EXISTS attempt_count;
