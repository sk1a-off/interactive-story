-- +goose Up
ALTER TABLE generation_run_metrics
    ADD COLUMN action_intent_fallback_used boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE generation_run_metrics
    DROP COLUMN IF EXISTS action_intent_fallback_used;
