-- +goose Up
CREATE TABLE generation_run_metrics (
    id uuid PRIMARY KEY,
    generation_job_id uuid NOT NULL REFERENCES generation_jobs(id) ON DELETE CASCADE,
    attempt_no integer NOT NULL CHECK(attempt_no > 0),
    config_revision_id uuid NOT NULL REFERENCES ai_config_revisions(id) ON DELETE RESTRICT,
    prompt_set_revision_id uuid NULL REFERENCES prompt_set_revisions(id) ON DELETE RESTRICT,
    context_mode text NOT NULL,
    queue_wait_ms bigint NOT NULL CHECK(queue_wait_ms >= 0),
    target_load_ms bigint NOT NULL CHECK(target_load_ms >= 0),
    context_build_ms bigint NOT NULL CHECK(context_build_ms >= 0),
    planner_ms bigint NOT NULL CHECK(planner_ms >= 0),
    writer_ms bigint NOT NULL CHECK(writer_ms >= 0),
    post_writer_ms bigint NOT NULL CHECK(post_writer_ms >= 0),
    validation_ms bigint NOT NULL CHECK(validation_ms >= 0),
    commit_ms bigint NOT NULL CHECK(commit_ms >= 0),
    unattributed_ms bigint NOT NULL CHECK(unattributed_ms >= 0),
    total_ms bigint NOT NULL CHECK(total_ms >= 0),
    time_to_first_story_text_ms bigint NOT NULL CHECK(time_to_first_story_text_ms >= 0),
    succeeded boolean NOT NULL,
    error_text text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(generation_job_id, attempt_no)
);

CREATE TRIGGER generation_run_metrics_immutable
BEFORE UPDATE OR DELETE ON generation_run_metrics
FOR EACH ROW EXECUTE FUNCTION reject_generation_provenance_update();

-- +goose Down
DROP TRIGGER IF EXISTS generation_run_metrics_immutable ON generation_run_metrics;
DROP TABLE IF EXISTS generation_run_metrics;
