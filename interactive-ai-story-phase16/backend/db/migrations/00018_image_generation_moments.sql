-- +goose Up
ALTER TABLE image_generations
  ADD COLUMN moment_index smallint NOT NULL DEFAULT 1
  CHECK (moment_index >= 1 AND moment_index <= 2);

CREATE UNIQUE INDEX image_generations_automatic_moment_uq
  ON image_generations(scene_id, source_beat_id, moment_index)
  WHERE trigger_kind='automatic' AND source_beat_id IS NOT NULL;

-- moment_index is part of immutable generation provenance.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_image_generation_provenance_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.story_id IS DISTINCT FROM OLD.story_id OR NEW.scene_id IS DISTINCT FROM OLD.scene_id
 OR NEW.source_beat_id IS DISTINCT FROM OLD.source_beat_id OR NEW.moment_index IS DISTINCT FROM OLD.moment_index
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

-- +goose Down
DROP INDEX IF EXISTS image_generations_automatic_moment_uq;
ALTER TABLE image_generations DROP COLUMN IF EXISTS moment_index;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_image_generation_provenance_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.story_id IS DISTINCT FROM OLD.story_id OR NEW.scene_id IS DISTINCT FROM OLD.scene_id
 OR NEW.source_beat_id IS DISTINCT FROM OLD.source_beat_id OR NEW.config_revision_id IS DISTINCT FROM OLD.config_revision_id
 OR NEW.provider_name IS DISTINCT FROM OLD.provider_name OR NEW.model_name IS DISTINCT FROM OLD.model_name
 OR NEW.profile_name IS DISTINCT FROM OLD.profile_name OR NEW.prompt IS DISTINCT FROM OLD.prompt
 OR NEW.negative_prompt IS DISTINCT FROM OLD.negative_prompt OR NEW.style IS DISTINCT FROM OLD.style OR NEW.style_prompt IS DISTINCT FROM OLD.style_prompt OR NEW.style_negative_prompt IS DISTINCT FROM OLD.style_negative_prompt
 OR NEW.width IS DISTINCT FROM OLD.width OR NEW.height IS DISTINCT FROM OLD.height OR NEW.image_count IS DISTINCT FROM OLD.image_count
 THEN RAISE EXCEPTION 'image generation provenance is immutable'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
