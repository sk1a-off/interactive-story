-- +goose Up
CREATE TABLE image_prompt_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    timeline_id uuid NOT NULL REFERENCES timelines(id) ON DELETE CASCADE,
    scene_id uuid NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    source_beat_id uuid NOT NULL REFERENCES beats(id) ON DELETE CASCADE,
    force_illustration boolean NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','running','completed','failed')),
    attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
    max_attempts integer NOT NULL DEFAULT 3 CHECK(max_attempts BETWEEN 1 AND 5),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_owner text NULL,
    lease_until timestamptz NULL,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz NULL,
    completed_at timestamptz NULL,
    UNIQUE(source_beat_id),
    CHECK ((status='running') = (lease_owner IS NOT NULL AND lease_until IS NOT NULL))
);

CREATE INDEX image_prompt_jobs_claim_idx
ON image_prompt_jobs(status,available_at,created_at)
WHERE status IN ('queued','running');

-- The outbox row is inserted in the same database transaction as Canon. A
-- process crash after commit therefore cannot lose the illustration prompt.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enqueue_image_prompt_job() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v_beat_id uuid;
    v_scene_id uuid;
    v_story_id uuid;
    v_position integer;
BEGIN
    IF NEW.event_type <> 'beat_committed' THEN
        RETURN NEW;
    END IF;
    v_beat_id := NULLIF(NEW.payload->>'beatId','')::uuid;
    v_scene_id := NULLIF(NEW.payload->>'sceneId','')::uuid;
    v_position := COALESCE(NULLIF(NEW.payload->>'position','')::integer, 0);
    IF v_beat_id IS NULL OR v_scene_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT t.story_id INTO v_story_id FROM timelines t WHERE t.id=NEW.timeline_id;
    INSERT INTO image_prompt_jobs(story_id,timeline_id,scene_id,source_beat_id,force_illustration)
    VALUES(v_story_id,NEW.timeline_id,v_scene_id,v_beat_id,v_position=1)
    ON CONFLICT(source_beat_id) DO NOTHING;
    RETURN NEW;
END $$;
-- +goose StatementEnd

CREATE TRIGGER story_event_image_prompt_outbox
AFTER INSERT ON story_events
FOR EACH ROW EXECUTE FUNCTION enqueue_image_prompt_job();

-- +goose Down
DROP TRIGGER IF EXISTS story_event_image_prompt_outbox ON story_events;
DROP FUNCTION IF EXISTS enqueue_image_prompt_job();
DROP TABLE IF EXISTS image_prompt_jobs;
