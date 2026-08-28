-- +goose Up
CREATE TABLE image_generations (
    id uuid PRIMARY KEY,
    story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    scene_id uuid NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    source_beat_id uuid NULL REFERENCES beats(id) ON DELETE SET NULL,
    config_revision_id uuid NOT NULL REFERENCES ai_config_revisions(id) ON DELETE RESTRICT,
    provider_name text NOT NULL,
    model_name text NOT NULL,
    profile_name text NOT NULL DEFAULT '',
    prompt text NOT NULL,
    negative_prompt text NOT NULL DEFAULT '',
    style text NOT NULL DEFAULT 'digital-painting',
    style_prompt text NOT NULL DEFAULT 'breathtaking digital art, trending on artstation, by atey ghailan, by greg rutkowski, by greg tocchini, by james gilleard, 8k, high resolution, best quality',
    style_negative_prompt text NOT NULL DEFAULT 'low-quality, deformed, signature watermark text, poorly drawn',
    width integer NOT NULL DEFAULT 768 CHECK(width=768),
    height integer NOT NULL DEFAULT 768 CHECK(height=768),
    image_count smallint NOT NULL DEFAULT 2 CHECK(image_count=2),
    trigger_kind text NOT NULL CHECK(trigger_kind IN ('automatic','manual')),
    status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','done','failed')),
    attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
    max_attempts integer NOT NULL DEFAULT 3 CHECK(max_attempts BETWEEN 1 AND 3),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_owner text NULL,
    lease_until timestamptz NULL,
    error_code text NULL,
    error_detail text NULL,
    provider_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz NULL,
    finished_at timestamptz NULL,
    CHECK ((status='running') = (lease_owner IS NOT NULL AND lease_until IS NOT NULL))
);
CREATE INDEX image_generations_claim_idx
ON image_generations(status,available_at,created_at)
WHERE status IN ('pending','running');
CREATE INDEX image_generations_scene_idx ON image_generations(scene_id,created_at DESC);

CREATE TABLE scene_images (
    id uuid PRIMARY KEY,
    story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    scene_id uuid NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    generation_id uuid NOT NULL REFERENCES image_generations(id) ON DELETE CASCADE,
    variant smallint NOT NULL CHECK(variant IN (1,2)),
    url text NOT NULL,
    content_type text NOT NULL CHECK(content_type IN ('image/jpeg','image/png','image/webp')),
    byte_size bigint NOT NULL CHECK(byte_size>0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(generation_id,variant)
);
ALTER TABLE scenes ADD COLUMN selected_image_id uuid NULL REFERENCES scene_images(id) ON DELETE SET NULL;

CREATE TABLE image_worker_state (
    worker_id text PRIMARY KEY,
    last_seen_at timestamptz NOT NULL,
    active_generation_id uuid NULL REFERENCES image_generations(id) ON DELETE SET NULL
);


-- Upgrade only the old placeholder image baseline to the new Perchance browser
-- provider. User-selected non-placeholder providers are left untouched.
WITH active AS (
  SELECT c.*
  FROM ai_active_config a JOIN ai_config_revisions c ON c.id=a.config_revision_id
  WHERE a.singleton=true AND c.image_provider='manual'
), inserted AS (
  INSERT INTO ai_config_revisions(
    story_llm_provider,story_llm_model,story_llm_profile,
    embedding_provider,embedding_model,image_provider,image_model,settings)
  SELECT story_llm_provider,story_llm_model,story_llm_profile,
         embedding_provider,embedding_model,'perchance_browser','text-to-image-plugin',
         settings || '{"image_provider_mode":"perchance_browser","image_style":"digital-painting","image_resolution":"768x768","image_count":2}'::jsonb
  FROM active
  RETURNING id
)
UPDATE ai_active_config a SET config_revision_id=i.id,updated_at=now()
FROM inserted i WHERE a.singleton=true;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_image_generation_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE scene_story uuid; beat_scene uuid;
BEGIN
 SELECT t.story_id INTO scene_story
 FROM scenes s JOIN chapters c ON c.id=s.chapter_id JOIN timelines t ON t.id=c.timeline_id
 WHERE s.id=NEW.scene_id;
 IF scene_story IS DISTINCT FROM NEW.story_id THEN RAISE EXCEPTION 'image generation story/scene scope mismatch'; END IF;
 IF NEW.source_beat_id IS NOT NULL THEN
   SELECT scene_id INTO beat_scene FROM beats WHERE id=NEW.source_beat_id;
   IF beat_scene IS DISTINCT FROM NEW.scene_id THEN RAISE EXCEPTION 'image generation beat/scene scope mismatch'; END IF;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER image_generation_scope BEFORE INSERT OR UPDATE OF story_id,scene_id,source_beat_id
ON image_generations FOR EACH ROW EXECUTE FUNCTION enforce_image_generation_scope();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_scene_image_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE g_story uuid; g_scene uuid;
BEGIN
 SELECT story_id,scene_id INTO g_story,g_scene FROM image_generations WHERE id=NEW.generation_id;
 IF g_story IS DISTINCT FROM NEW.story_id OR g_scene IS DISTINCT FROM NEW.scene_id
 THEN RAISE EXCEPTION 'scene image generation scope mismatch'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER scene_image_scope BEFORE INSERT OR UPDATE ON scene_images
FOR EACH ROW EXECUTE FUNCTION enforce_scene_image_scope();


-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enforce_scene_selected_image_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE image_scene uuid;
BEGIN
 IF NEW.selected_image_id IS NULL THEN RETURN NEW; END IF;
 SELECT scene_id INTO image_scene FROM scene_images WHERE id=NEW.selected_image_id;
 IF image_scene IS DISTINCT FROM NEW.id THEN RAISE EXCEPTION 'selected image belongs to another scene'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER scene_selected_image_scope BEFORE INSERT OR UPDATE OF selected_image_id
ON scenes FOR EACH ROW EXECUTE FUNCTION enforce_scene_selected_image_scope();

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
CREATE TRIGGER image_generation_immutable_provenance BEFORE UPDATE ON image_generations
FOR EACH ROW EXECUTE FUNCTION reject_image_generation_provenance_update();

-- +goose Down
DROP TRIGGER IF EXISTS image_generation_immutable_provenance ON image_generations;
DROP FUNCTION IF EXISTS reject_image_generation_provenance_update();
DROP TRIGGER IF EXISTS scene_selected_image_scope ON scenes;
DROP FUNCTION IF EXISTS enforce_scene_selected_image_scope();
DROP TRIGGER IF EXISTS scene_image_scope ON scene_images;
DROP FUNCTION IF EXISTS enforce_scene_image_scope();
DROP TRIGGER IF EXISTS image_generation_scope ON image_generations;
DROP FUNCTION IF EXISTS enforce_image_generation_scope();
DROP TABLE IF EXISTS image_worker_state;
ALTER TABLE scenes DROP COLUMN IF EXISTS selected_image_id;
DROP TABLE IF EXISTS scene_images;
DROP TABLE IF EXISTS image_generations;
