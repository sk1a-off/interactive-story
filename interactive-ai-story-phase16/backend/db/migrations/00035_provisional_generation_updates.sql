-- +goose Up
ALTER TABLE generation_updates
    ADD COLUMN timeline_id uuid NULL REFERENCES timelines(id) ON DELETE CASCADE,
    ADD COLUMN revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
    ADD COLUMN provisional boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE generation_updates
    DROP COLUMN provisional,
    DROP COLUMN revision,
    DROP COLUMN timeline_id;
