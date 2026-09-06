-- +goose Up
ALTER TABLE generation_run_metrics
    ADD COLUMN context_shadow_ms bigint NOT NULL DEFAULT 0 CHECK(context_shadow_ms >= 0),
    ADD COLUMN shadow_retrieved_count integer NOT NULL DEFAULT 0 CHECK(shadow_retrieved_count >= 0),
    ADD COLUMN shadow_retrieval_error text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE generation_run_metrics
    DROP COLUMN IF EXISTS shadow_retrieval_error,
    DROP COLUMN IF EXISTS shadow_retrieved_count,
    DROP COLUMN IF EXISTS context_shadow_ms;
