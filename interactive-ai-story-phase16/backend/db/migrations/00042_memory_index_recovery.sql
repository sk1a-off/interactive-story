-- +goose Up
-- Failed indexing jobs are retried after an escalating cooldown instead of
-- blocking their timeline forever. `recoveries` bounds how many times a job
-- that exhausted its attempts may be requeued.
ALTER TABLE memory_index_jobs
    ADD COLUMN recoveries integer NOT NULL DEFAULT 0 CHECK (recoveries >= 0);

CREATE INDEX memory_index_jobs_failed_idx ON memory_index_jobs(index_revision, completed_at) WHERE status='failed';

-- +goose Down
DROP INDEX IF EXISTS memory_index_jobs_failed_idx;
ALTER TABLE memory_index_jobs DROP COLUMN recoveries;
