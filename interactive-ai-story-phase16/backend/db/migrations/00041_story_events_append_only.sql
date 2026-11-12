-- +goose Up
-- Story Canon is append-only. Rows may be added, never rewritten or removed,
-- except by an explicit maintenance session that sets
-- `SET LOCAL story.allow_event_rewrite = 'on'` inside its transaction
-- (for example a repair migration). Dropping the table (down migrations)
-- is unaffected because row triggers do not fire for DROP TABLE.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION story_events_append_only() RETURNS trigger AS $$
BEGIN
  IF COALESCE(current_setting('story.allow_event_rewrite', true), '') IN ('on', 'true', '1') THEN
    IF TG_OP = 'DELETE' THEN
      RETURN OLD;
    END IF;
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'story_events is append-only: % on timeline % seq % is not allowed (set story.allow_event_rewrite for maintenance)',
    TG_OP, OLD.timeline_id, OLD.seq
    USING ERRCODE = 'integrity_constraint_violation';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER story_events_no_update
BEFORE UPDATE ON story_events
FOR EACH ROW EXECUTE FUNCTION story_events_append_only();

CREATE TRIGGER story_events_no_delete
BEFORE DELETE ON story_events
FOR EACH ROW EXECUTE FUNCTION story_events_append_only();

-- +goose Down
DROP TRIGGER IF EXISTS story_events_no_update ON story_events;
DROP TRIGGER IF EXISTS story_events_no_delete ON story_events;
DROP FUNCTION IF EXISTS story_events_append_only();
