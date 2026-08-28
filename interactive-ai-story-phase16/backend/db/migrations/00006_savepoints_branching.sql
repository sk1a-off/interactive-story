-- +goose Up
ALTER TABLE snapshots ADD CONSTRAINT snapshots_id_timeline_unique UNIQUE (id, timeline_id);

CREATE TABLE save_points (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    story_id uuid NOT NULL REFERENCES stories(id),
    timeline_id uuid NOT NULL,
    snapshot_id uuid NOT NULL,
    event_seq bigint NOT NULL CHECK (event_seq >= 0),
    name text NOT NULL CHECK (length(btrim(name)) > 0),
    note text NOT NULL DEFAULT '',
    kind text NOT NULL DEFAULT 'manual' CHECK (kind IN ('manual','autosave','pre_restore','pre_regeneration','system')),
    pinned boolean NOT NULL DEFAULT false,
    chapter_id uuid NULL REFERENCES chapters(id),
    scene_id uuid NULL REFERENCES scenes(id),
    beat_id uuid NULL REFERENCES beats(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT save_points_timeline_story_fk
        FOREIGN KEY (timeline_id, story_id)
        REFERENCES timelines(id, story_id),
    CONSTRAINT save_points_snapshot_exact_fk
        FOREIGN KEY (snapshot_id, timeline_id, event_seq)
        REFERENCES snapshots(id, timeline_id, event_seq),
    CONSTRAINT save_points_id_timeline_unique UNIQUE (id, timeline_id),
    CONSTRAINT save_points_id_story_unique UNIQUE (id, story_id)
);

CREATE INDEX save_points_timeline_created_idx ON save_points(timeline_id, created_at DESC);
CREATE INDEX save_points_story_created_idx ON save_points(story_id, created_at DESC);
CREATE INDEX save_points_autosave_retention_idx
    ON save_points(timeline_id, created_at DESC)
    WHERE kind = 'autosave' AND pinned = false;

ALTER TABLE timelines DROP CONSTRAINT timelines_parent_timeline_id_fkey;
ALTER TABLE timelines
    ADD CONSTRAINT timelines_parent_same_story_fk
        FOREIGN KEY (parent_timeline_id, story_id)
        REFERENCES timelines(id, story_id);

ALTER TABLE timelines
    ADD COLUMN forked_from_save_id uuid NULL,
    ADD CONSTRAINT timelines_forked_from_save_same_story_fk
        FOREIGN KEY (forked_from_save_id, story_id)
        REFERENCES save_points(id, story_id);


-- +goose StatementBegin
CREATE FUNCTION enforce_savepoint_cursor_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    chapter_timeline uuid;
    scene_timeline uuid;
    beat_timeline uuid;
    scene_chapter uuid;
    beat_scene uuid;
BEGIN
    IF NEW.chapter_id IS NOT NULL THEN
        SELECT timeline_id INTO chapter_timeline FROM chapters WHERE id=NEW.chapter_id;
        IF chapter_timeline IS DISTINCT FROM NEW.timeline_id THEN RAISE EXCEPTION 'save chapter/timeline mismatch'; END IF;
    END IF;
    IF NEW.scene_id IS NOT NULL THEN
        SELECT c.timeline_id, s.chapter_id INTO scene_timeline, scene_chapter
        FROM scenes s JOIN chapters c ON c.id=s.chapter_id WHERE s.id=NEW.scene_id;
        IF scene_timeline IS DISTINCT FROM NEW.timeline_id THEN RAISE EXCEPTION 'save scene/timeline mismatch'; END IF;
        IF NEW.chapter_id IS NOT NULL AND scene_chapter IS DISTINCT FROM NEW.chapter_id THEN RAISE EXCEPTION 'save chapter/scene mismatch'; END IF;
    END IF;
    IF NEW.beat_id IS NOT NULL THEN
        SELECT c.timeline_id, b.scene_id INTO beat_timeline, beat_scene
        FROM beats b JOIN scenes s ON s.id=b.scene_id JOIN chapters c ON c.id=s.chapter_id WHERE b.id=NEW.beat_id;
        IF beat_timeline IS DISTINCT FROM NEW.timeline_id THEN RAISE EXCEPTION 'save beat/timeline mismatch'; END IF;
        IF NEW.scene_id IS NOT NULL AND beat_scene IS DISTINCT FROM NEW.scene_id THEN RAISE EXCEPTION 'save scene/beat mismatch'; END IF;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER save_points_cursor_scope
    BEFORE INSERT OR UPDATE ON save_points
    FOR EACH ROW EXECUTE FUNCTION enforce_savepoint_cursor_scope();

-- +goose StatementBegin
CREATE FUNCTION enforce_savepoint_immutable_reference() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.story_id IS DISTINCT FROM NEW.story_id
       OR OLD.timeline_id IS DISTINCT FROM NEW.timeline_id
       OR OLD.snapshot_id IS DISTINCT FROM NEW.snapshot_id
       OR OLD.event_seq IS DISTINCT FROM NEW.event_seq
       OR OLD.kind IS DISTINCT FROM NEW.kind
       OR OLD.chapter_id IS DISTINCT FROM NEW.chapter_id
       OR OLD.scene_id IS DISTINCT FROM NEW.scene_id
       OR OLD.beat_id IS DISTINCT FROM NEW.beat_id
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'save point canonical reference is immutable';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER save_points_immutable_reference
    BEFORE UPDATE ON save_points
    FOR EACH ROW EXECUTE FUNCTION enforce_savepoint_immutable_reference();

-- +goose StatementBegin
CREATE FUNCTION enforce_timeline_fork_lineage() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    save_timeline uuid;
    save_story uuid;
    save_seq bigint;
BEGIN
    IF TG_OP = 'UPDATE' AND (
        OLD.parent_timeline_id IS DISTINCT FROM NEW.parent_timeline_id OR
        OLD.forked_from_save_id IS DISTINCT FROM NEW.forked_from_save_id OR
        OLD.story_id IS DISTINCT FROM NEW.story_id
    ) THEN
        RAISE EXCEPTION 'timeline lineage is immutable';
    END IF;
    IF NEW.forked_from_save_id IS NOT NULL THEN
        IF NEW.parent_timeline_id IS NULL THEN RAISE EXCEPTION 'fork requires parent timeline'; END IF;
        SELECT timeline_id, story_id, event_seq INTO save_timeline, save_story, save_seq
        FROM save_points WHERE id=NEW.forked_from_save_id;
        IF save_timeline IS DISTINCT FROM NEW.parent_timeline_id OR save_story IS DISTINCT FROM NEW.story_id THEN
            RAISE EXCEPTION 'fork lineage/save mismatch';
        END IF;
        IF TG_OP = 'INSERT' AND NEW.head_event_seq IS DISTINCT FROM save_seq THEN
            RAISE EXCEPTION 'fork head must equal save event sequence';
        END IF;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER timelines_fork_lineage
    BEFORE INSERT OR UPDATE OF parent_timeline_id, forked_from_save_id, story_id ON timelines
    FOR EACH ROW EXECUTE FUNCTION enforce_timeline_fork_lineage();

-- A head snapshot must belong to the same Timeline. The older scalar FK allowed
-- an accidental cross-Timeline snapshot reference despite UUID correctness.
ALTER TABLE timelines DROP CONSTRAINT timelines_head_snapshot_fk;
ALTER TABLE timelines
    ADD CONSTRAINT timelines_head_snapshot_same_timeline_fk
    FOREIGN KEY (head_snapshot_id, id)
    REFERENCES snapshots(id, timeline_id);

-- +goose Down
ALTER TABLE timelines DROP CONSTRAINT timelines_head_snapshot_same_timeline_fk;
DROP TRIGGER timelines_fork_lineage ON timelines;
DROP FUNCTION enforce_timeline_fork_lineage();
DROP TRIGGER save_points_immutable_reference ON save_points;
DROP FUNCTION enforce_savepoint_immutable_reference();
DROP TRIGGER save_points_cursor_scope ON save_points;
DROP FUNCTION enforce_savepoint_cursor_scope();
ALTER TABLE timelines
    ADD CONSTRAINT timelines_head_snapshot_fk
    FOREIGN KEY (head_snapshot_id)
    REFERENCES snapshots(id);
ALTER TABLE timelines DROP CONSTRAINT timelines_forked_from_save_same_story_fk;
ALTER TABLE timelines DROP COLUMN forked_from_save_id;
ALTER TABLE timelines DROP CONSTRAINT timelines_parent_same_story_fk;
ALTER TABLE timelines ADD CONSTRAINT timelines_parent_timeline_id_fkey FOREIGN KEY (parent_timeline_id) REFERENCES timelines(id);
DROP TABLE save_points;
ALTER TABLE snapshots DROP CONSTRAINT snapshots_id_timeline_unique;
