-- +goose Up
CREATE TABLE generation_updates (
    generation_job_id uuid NOT NULL REFERENCES generation_jobs(id) ON DELETE CASCADE,
    sequence bigint NOT NULL CHECK(sequence > 0),
    phase text NOT NULL,
    text_delta text NOT NULL DEFAULT '',
    error_text text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(generation_job_id,sequence)
);
CREATE INDEX generation_updates_created_idx ON generation_updates(generation_job_id,created_at);

-- +goose Down
DROP TABLE IF EXISTS generation_updates;
