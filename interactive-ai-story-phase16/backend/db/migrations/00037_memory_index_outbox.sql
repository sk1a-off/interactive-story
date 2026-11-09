-- +goose Up
CREATE TABLE memory_index_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    timeline_id uuid NOT NULL,
    source_kind text NOT NULL CHECK(source_kind IN ('event','snapshot')),
    source_event_id uuid UNIQUE REFERENCES story_events(id) ON DELETE CASCADE,
    source_snapshot_id uuid UNIQUE REFERENCES snapshots(id) ON DELETE CASCADE,
    source_event_seq bigint NOT NULL CHECK(source_event_seq>=0),
    status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','running','completed','failed')),
    attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),
    max_attempts integer NOT NULL DEFAULT 5 CHECK(max_attempts BETWEEN 1 AND 5),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_owner text,
    lease_token uuid,
    lease_until timestamptz,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    FOREIGN KEY(timeline_id,story_id) REFERENCES timelines(id,story_id) ON DELETE CASCADE,
    CHECK ((source_kind='event' AND source_event_id IS NOT NULL AND source_snapshot_id IS NULL)
        OR (source_kind='snapshot' AND source_snapshot_id IS NOT NULL AND source_event_id IS NULL)),
    CHECK ((status='running' AND lease_owner IS NOT NULL AND lease_token IS NOT NULL AND lease_until IS NOT NULL)
        OR (status<>'running' AND lease_owner IS NULL AND lease_token IS NULL AND lease_until IS NULL))
);
CREATE INDEX memory_index_jobs_claim_idx ON memory_index_jobs(available_at,created_at,id) WHERE status='queued';
CREATE INDEX memory_index_jobs_timeline_order_idx ON memory_index_jobs(timeline_id,source_event_seq,created_at,id) WHERE status<>'completed';

-- A committed change is visible here before its asynchronous embedding. Search
-- must skip branch-state memory while its published cursor trails this value.
CREATE TABLE memory_index_state (
    timeline_id uuid PRIMARY KEY,
    story_id uuid NOT NULL,
    observed_event_seq bigint NOT NULL CHECK(observed_event_seq>=0),
    FOREIGN KEY(timeline_id,story_id) REFERENCES timelines(id,story_id) ON DELETE CASCADE
);

-- +goose StatementBegin
CREATE FUNCTION guard_memory_index_source() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_timeline uuid; source_seq bigint;
BEGIN
    IF TG_OP='UPDATE' THEN
        IF ROW(NEW.story_id,NEW.timeline_id,NEW.source_kind,NEW.source_event_id,NEW.source_snapshot_id,NEW.source_event_seq,NEW.created_at)
            IS DISTINCT FROM ROW(OLD.story_id,OLD.timeline_id,OLD.source_kind,OLD.source_event_id,OLD.source_snapshot_id,OLD.source_event_seq,OLD.created_at) THEN
            RAISE EXCEPTION 'memory index source is immutable';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.source_kind='event' THEN
        SELECT timeline_id,seq INTO source_timeline,source_seq FROM story_events WHERE id=NEW.source_event_id;
    ELSE
        SELECT timeline_id,event_seq INTO source_timeline,source_seq FROM snapshots WHERE id=NEW.source_snapshot_id;
    END IF;
    IF source_timeline IS DISTINCT FROM NEW.timeline_id OR source_seq IS DISTINCT FROM NEW.source_event_seq THEN
        RAISE EXCEPTION 'memory index source scope/sequence mismatch';
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER memory_index_source_guard BEFORE INSERT OR UPDATE ON memory_index_jobs FOR EACH ROW EXECUTE FUNCTION guard_memory_index_source();

-- +goose StatementBegin
CREATE FUNCTION enqueue_memory_index_job() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_story uuid; source_seq bigint; source_kind text;
BEGIN
    SELECT story_id INTO source_story FROM timelines WHERE id=NEW.timeline_id;
    IF TG_TABLE_NAME='story_events' THEN
        source_seq := NEW.seq;
        source_kind := 'event';
        INSERT INTO memory_index_jobs(story_id,timeline_id,source_kind,source_event_id,source_event_seq)
        VALUES(source_story,NEW.timeline_id,source_kind,NEW.id,source_seq) ON CONFLICT(source_event_id) DO NOTHING;
    ELSE
        source_seq := NEW.event_seq;
        source_kind := 'snapshot';
        INSERT INTO memory_index_jobs(story_id,timeline_id,source_kind,source_snapshot_id,source_event_seq)
        VALUES(source_story,NEW.timeline_id,source_kind,NEW.id,source_seq) ON CONFLICT(source_snapshot_id) DO NOTHING;
    END IF;
    INSERT INTO memory_index_state(timeline_id,story_id,observed_event_seq) VALUES(NEW.timeline_id,source_story,source_seq)
    ON CONFLICT(timeline_id) DO UPDATE SET observed_event_seq=GREATEST(memory_index_state.observed_event_seq,EXCLUDED.observed_event_seq);
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER story_event_memory_outbox AFTER INSERT ON story_events FOR EACH ROW EXECUTE FUNCTION enqueue_memory_index_job();
CREATE TRIGGER snapshot_memory_outbox AFTER INSERT ON snapshots FOR EACH ROW EXECUTE FUNCTION enqueue_memory_index_job();

-- +goose Down
DROP TRIGGER snapshot_memory_outbox ON snapshots;
DROP TRIGGER story_event_memory_outbox ON story_events;
DROP FUNCTION enqueue_memory_index_job();
DROP TRIGGER memory_index_source_guard ON memory_index_jobs;
DROP FUNCTION guard_memory_index_source();
DROP TABLE memory_index_state;
DROP TABLE memory_index_jobs;
