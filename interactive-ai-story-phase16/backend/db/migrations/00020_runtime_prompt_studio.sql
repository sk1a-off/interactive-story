-- +goose Up
CREATE TABLE prompt_set_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    revision bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    prompts jsonb NOT NULL,
    role_settings jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(prompts)='object'),
    CHECK (jsonb_typeof(role_settings)='object')
);

CREATE TABLE prompt_active_set (
    singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
    prompt_set_revision_id uuid NOT NULL REFERENCES prompt_set_revisions(id) ON DELETE RESTRICT,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Prompt revisions are immutable. Editing in Prompt Studio always creates a
-- new revision and only moves the active pointer for future work.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_prompt_set_revision_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'prompt set revisions are immutable';
END $$;
-- +goose StatementEnd
CREATE TRIGGER prompt_set_revisions_immutable
BEFORE UPDATE OR DELETE ON prompt_set_revisions
FOR EACH ROW EXECUTE FUNCTION reject_prompt_set_revision_update();

-- Nullable is intentional for historical rows created before Prompt Studio.
-- New jobs/generations created by the application always pin a revision.
ALTER TABLE generation_jobs
    ADD COLUMN prompt_set_revision_id uuid NULL REFERENCES prompt_set_revisions(id) ON DELETE RESTRICT;
ALTER TABLE image_generations
    ADD COLUMN prompt_set_revision_id uuid NULL REFERENCES prompt_set_revisions(id) ON DELETE RESTRICT;

-- Extend immutable generation-job provenance with the pinned prompt revision.
-- generation_attempts keeps using the original always-reject trigger function.
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
    THEN RAISE EXCEPTION 'generation provenance is immutable'; END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER generation_jobs_immutable_provenance
BEFORE UPDATE OF config_revision_id,prompt_set_revision_id,provider_kind,provider_name,model_name,profile_name,story_id,timeline_id,expected_head_event_seq
ON generation_jobs FOR EACH ROW EXECUTE FUNCTION reject_generation_job_provenance_update();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_image_generation_provenance_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.story_id IS DISTINCT FROM OLD.story_id OR NEW.scene_id IS DISTINCT FROM OLD.scene_id
 OR NEW.source_beat_id IS DISTINCT FROM OLD.source_beat_id OR NEW.moment_index IS DISTINCT FROM OLD.moment_index
 OR NEW.anchor_paragraph IS DISTINCT FROM OLD.anchor_paragraph
 OR NEW.config_revision_id IS DISTINCT FROM OLD.config_revision_id
 OR NEW.prompt_set_revision_id IS DISTINCT FROM OLD.prompt_set_revision_id
 OR NEW.provider_name IS DISTINCT FROM OLD.provider_name OR NEW.model_name IS DISTINCT FROM OLD.model_name
 OR NEW.profile_name IS DISTINCT FROM OLD.profile_name OR NEW.prompt IS DISTINCT FROM OLD.prompt
 OR NEW.negative_prompt IS DISTINCT FROM OLD.negative_prompt OR NEW.style IS DISTINCT FROM OLD.style
 OR NEW.style_prompt IS DISTINCT FROM OLD.style_prompt OR NEW.style_negative_prompt IS DISTINCT FROM OLD.style_negative_prompt
 OR NEW.width IS DISTINCT FROM OLD.width OR NEW.height IS DISTINCT FROM OLD.height OR NEW.image_count IS DISTINCT FROM OLD.image_count
 THEN RAISE EXCEPTION 'image generation provenance is immutable'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd

-- +goose Down
-- Drop the Prompt-Studio job trigger before its referenced column.
DROP TRIGGER IF EXISTS generation_jobs_immutable_provenance ON generation_jobs;
DROP FUNCTION IF EXISTS reject_generation_job_provenance_update();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_image_generation_provenance_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.story_id IS DISTINCT FROM OLD.story_id OR NEW.scene_id IS DISTINCT FROM OLD.scene_id
 OR NEW.source_beat_id IS DISTINCT FROM OLD.source_beat_id OR NEW.moment_index IS DISTINCT FROM OLD.moment_index
 OR NEW.anchor_paragraph IS DISTINCT FROM OLD.anchor_paragraph
 OR NEW.config_revision_id IS DISTINCT FROM OLD.config_revision_id
 OR NEW.provider_name IS DISTINCT FROM OLD.provider_name OR NEW.model_name IS DISTINCT FROM OLD.model_name
 OR NEW.profile_name IS DISTINCT FROM OLD.profile_name OR NEW.prompt IS DISTINCT FROM OLD.prompt
 OR NEW.negative_prompt IS DISTINCT FROM OLD.negative_prompt OR NEW.style IS DISTINCT FROM OLD.style
 OR NEW.style_prompt IS DISTINCT FROM OLD.style_prompt OR NEW.style_negative_prompt IS DISTINCT FROM OLD.style_negative_prompt
 OR NEW.width IS DISTINCT FROM OLD.width OR NEW.height IS DISTINCT FROM OLD.height OR NEW.image_count IS DISTINCT FROM OLD.image_count
 THEN RAISE EXCEPTION 'image generation provenance is immutable'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd


ALTER TABLE image_generations DROP COLUMN IF EXISTS prompt_set_revision_id;
ALTER TABLE generation_jobs DROP COLUMN IF EXISTS prompt_set_revision_id;

-- Restore the pre-Prompt-Studio generation-job trigger.
CREATE TRIGGER generation_jobs_immutable_provenance
BEFORE UPDATE OF config_revision_id, provider_kind, provider_name, model_name, profile_name, story_id, timeline_id, expected_head_event_seq
ON generation_jobs FOR EACH ROW EXECUTE FUNCTION reject_generation_provenance_update();

DROP TRIGGER IF EXISTS prompt_set_revisions_immutable ON prompt_set_revisions;
DROP FUNCTION IF EXISTS reject_prompt_set_revision_update();
DROP TABLE IF EXISTS prompt_active_set;
DROP TABLE IF EXISTS prompt_set_revisions;
