-- +goose Up
ALTER TABLE generation_jobs
    ADD COLUMN action_hash text;

-- Backfill historical jobs so an idempotent replay after deployment can still
-- distinguish the original action from a conflicting payload.
UPDATE generation_jobs
SET action_hash = encode(digest(convert_to(lower(regexp_replace(trim(COALESCE(request_payload->>'text', '')), '[[:space:]]+', ' ', 'g')), 'UTF8'), 'sha256'), 'hex');

ALTER TABLE generation_jobs
    ALTER COLUMN action_hash SET NOT NULL;

ALTER TABLE generation_attempts
    ADD COLUMN call_sequence integer NOT NULL DEFAULT 1 CHECK(call_sequence > 0),
    ADD COLUMN context_mode text NOT NULL DEFAULT 'full-v1',
    ADD COLUMN retry_count integer NOT NULL DEFAULT 0 CHECK(retry_count >= 0),
    ADD COLUMN escalated_from text NOT NULL DEFAULT '',
    ADD COLUMN error_text text NOT NULL DEFAULT '';

ALTER TABLE generation_attempts
    DROP CONSTRAINT generation_attempts_generation_job_id_attempt_no_role_phase_key;
ALTER TABLE generation_attempts
    ADD CONSTRAINT generation_attempts_job_attempt_call_unique
    UNIQUE(generation_job_id, attempt_no, call_sequence);

DROP TRIGGER IF EXISTS generation_jobs_immutable_provenance ON generation_jobs;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_generation_job_provenance_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.config_revision_id IS DISTINCT FROM OLD.config_revision_id
    OR NEW.prompt_set_revision_id IS DISTINCT FROM OLD.prompt_set_revision_id
    OR NEW.provider_kind IS DISTINCT FROM OLD.provider_kind
    OR NEW.provider_name IS DISTINCT FROM OLD.provider_name
    OR NEW.model_name IS DISTINCT FROM OLD.model_name
    OR NEW.profile_name IS DISTINCT FROM OLD.profile_name
    OR NEW.story_id IS DISTINCT FROM OLD.story_id
    OR NEW.timeline_id IS DISTINCT FROM OLD.timeline_id
    OR NEW.expected_head_event_seq IS DISTINCT FROM OLD.expected_head_event_seq
    OR NEW.action_hash IS DISTINCT FROM OLD.action_hash
    THEN RAISE EXCEPTION 'generation provenance is immutable'; END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER generation_jobs_immutable_provenance
BEFORE UPDATE OF config_revision_id,prompt_set_revision_id,provider_kind,provider_name,model_name,profile_name,story_id,timeline_id,expected_head_event_seq,action_hash
ON generation_jobs FOR EACH ROW EXECUTE FUNCTION reject_generation_job_provenance_update();

-- +goose Down
DROP TRIGGER IF EXISTS generation_jobs_immutable_provenance ON generation_jobs;

ALTER TABLE generation_attempts
    DROP CONSTRAINT IF EXISTS generation_attempts_job_attempt_call_unique;
ALTER TABLE generation_attempts
    DROP COLUMN IF EXISTS error_text,
    DROP COLUMN IF EXISTS escalated_from,
    DROP COLUMN IF EXISTS retry_count,
    DROP COLUMN IF EXISTS context_mode,
    DROP COLUMN IF EXISTS call_sequence;
ALTER TABLE generation_attempts
    ADD CONSTRAINT generation_attempts_generation_job_id_attempt_no_role_phase_key
    UNIQUE(generation_job_id, attempt_no, role, phase);

ALTER TABLE generation_jobs DROP COLUMN IF EXISTS action_hash;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_generation_job_provenance_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.config_revision_id IS DISTINCT FROM OLD.config_revision_id
    OR NEW.prompt_set_revision_id IS DISTINCT FROM OLD.prompt_set_revision_id
    OR NEW.provider_kind IS DISTINCT FROM OLD.provider_kind
    OR NEW.provider_name IS DISTINCT FROM OLD.provider_name
    OR NEW.model_name IS DISTINCT FROM OLD.model_name
    OR NEW.profile_name IS DISTINCT FROM OLD.profile_name
    OR NEW.story_id IS DISTINCT FROM OLD.story_id
    OR NEW.timeline_id IS DISTINCT FROM OLD.timeline_id
    OR NEW.expected_head_event_seq IS DISTINCT FROM OLD.expected_head_event_seq
    THEN RAISE EXCEPTION 'generation provenance is immutable'; END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER generation_jobs_immutable_provenance
BEFORE UPDATE OF config_revision_id,prompt_set_revision_id,provider_kind,provider_name,model_name,profile_name,story_id,timeline_id,expected_head_event_seq
ON generation_jobs FOR EACH ROW EXECUTE FUNCTION reject_generation_job_provenance_update();
