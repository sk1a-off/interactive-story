-- +goose Up
CREATE TABLE ai_config_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    revision bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    story_llm_provider text NOT NULL,
    story_llm_model text NOT NULL,
    story_llm_profile text NOT NULL,
    embedding_provider text NOT NULL,
    embedding_model text NOT NULL,
    image_provider text NOT NULL,
    image_model text NOT NULL,
    settings jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE generation_jobs (
    id uuid PRIMARY KEY,
    story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    timeline_id uuid NOT NULL REFERENCES timelines(id) ON DELETE CASCADE,
    expected_head_event_seq bigint NOT NULL CHECK (expected_head_event_seq >= 0),
    config_revision_id uuid NOT NULL REFERENCES ai_config_revisions(id) ON DELETE RESTRICT,
    status text NOT NULL CHECK (status IN ('queued','running','completed','failed','cancelled')),
    provider_kind text NOT NULL CHECK (provider_kind IN ('story_llm','embedding','image')),
    provider_name text NOT NULL,
    model_name text NOT NULL,
    profile_name text NOT NULL DEFAULT '',
    request_id text NOT NULL DEFAULT '',
    error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz
);
CREATE INDEX generation_jobs_timeline_status_idx ON generation_jobs(timeline_id, status, created_at DESC);

CREATE TABLE generation_attempts (
    id uuid PRIMARY KEY,
    generation_job_id uuid NOT NULL REFERENCES generation_jobs(id) ON DELETE CASCADE,
    attempt_no integer NOT NULL CHECK (attempt_no > 0),
    role text NOT NULL,
    phase text NOT NULL,
    prompt_version text NOT NULL DEFAULT '',
    provider_name text NOT NULL,
    model_name text NOT NULL,
    profile_name text NOT NULL DEFAULT '',
    latency_ms bigint CHECK (latency_ms IS NULL OR latency_ms >= 0),
    input_tokens bigint CHECK (input_tokens IS NULL OR input_tokens >= 0),
    output_tokens bigint CHECK (output_tokens IS NULL OR output_tokens >= 0),
    repair_count integer NOT NULL DEFAULT 0 CHECK (repair_count >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(generation_job_id, attempt_no, role, phase)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_generation_provenance_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'generation provenance is immutable';
END $$;
-- +goose StatementEnd
CREATE TRIGGER generation_jobs_immutable_provenance
BEFORE UPDATE OF config_revision_id, provider_kind, provider_name, model_name, profile_name, story_id, timeline_id, expected_head_event_seq
ON generation_jobs FOR EACH ROW EXECUTE FUNCTION reject_generation_provenance_update();
CREATE TRIGGER generation_attempts_immutable
BEFORE UPDATE OR DELETE ON generation_attempts
FOR EACH ROW EXECUTE FUNCTION reject_generation_provenance_update();

-- +goose Down
DROP TRIGGER IF EXISTS generation_attempts_immutable ON generation_attempts;
DROP TRIGGER IF EXISTS generation_jobs_immutable_provenance ON generation_jobs;
DROP FUNCTION IF EXISTS reject_generation_provenance_update();
DROP TABLE IF EXISTS generation_attempts;
DROP TABLE IF EXISTS generation_jobs;
DROP TABLE IF EXISTS ai_config_revisions;
