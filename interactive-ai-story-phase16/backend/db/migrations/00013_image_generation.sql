-- +goose Up
CREATE TABLE image_assets (
    id uuid PRIMARY KEY,
    story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    timeline_id uuid NULL REFERENCES timelines(id) ON DELETE CASCADE,
    beat_id uuid NULL REFERENCES beats(id) ON DELETE SET NULL,
    generation_job_id uuid NULL REFERENCES generation_jobs(id) ON DELETE SET NULL,
    purpose text NOT NULL CHECK (purpose IN ('beat','save_thumbnail','story_cover','character')),
    status text NOT NULL CHECK (status IN ('queued','running','ready','failed')),
    artifact_ref text NULL,
    prompt_summary text NOT NULL DEFAULT '',
    provider_name text NOT NULL,
    model_name text NOT NULL,
    profile_name text NOT NULL DEFAULT '',
    config_revision_id uuid NOT NULL REFERENCES ai_config_revisions(id) ON DELETE RESTRICT,
    error_code text NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NULL,
    CHECK ((status='ready') = (artifact_ref IS NOT NULL))
);
CREATE INDEX image_assets_timeline_created_idx ON image_assets(timeline_id,created_at DESC);
CREATE INDEX image_assets_beat_idx ON image_assets(beat_id) WHERE beat_id IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_image_provenance_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.provider_name IS DISTINCT FROM OLD.provider_name
 OR NEW.model_name IS DISTINCT FROM OLD.model_name
 OR NEW.profile_name IS DISTINCT FROM OLD.profile_name
 OR NEW.config_revision_id IS DISTINCT FROM OLD.config_revision_id
 OR NEW.story_id IS DISTINCT FROM OLD.story_id
 OR NEW.timeline_id IS DISTINCT FROM OLD.timeline_id
 OR NEW.beat_id IS DISTINCT FROM OLD.beat_id
 OR NEW.generation_job_id IS DISTINCT FROM OLD.generation_job_id
 THEN RAISE EXCEPTION 'image provenance is immutable';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER image_assets_immutable_provenance
BEFORE UPDATE ON image_assets FOR EACH ROW EXECUTE FUNCTION reject_image_provenance_update();

-- +goose Down
DROP TRIGGER IF EXISTS image_assets_immutable_provenance ON image_assets;
DROP FUNCTION IF EXISTS reject_image_provenance_update();
DROP TABLE IF EXISTS image_assets;
