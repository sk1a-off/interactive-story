-- +goose Up
ALTER TABLE generation_run_metrics
    ADD COLUMN choices_fallback_used boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE generation_run_metrics
    DROP COLUMN IF EXISTS choices_fallback_used;
