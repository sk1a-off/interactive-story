-- +goose Up
CREATE TABLE timelines (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    story_id uuid NOT NULL REFERENCES stories(id),
    parent_timeline_id uuid NULL REFERENCES timelines(id),
    name text NOT NULL CHECK (length(btrim(name)) > 0),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived','deleted')),
    head_event_seq bigint NOT NULL DEFAULT 0 CHECK (head_event_seq >= 0),
    head_snapshot_id uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT timelines_id_story_unique UNIQUE(id, story_id)
);

CREATE INDEX timelines_story_updated_idx ON timelines(story_id, updated_at DESC);
CREATE INDEX timelines_parent_idx ON timelines(parent_timeline_id);

CREATE TABLE story_events (
    id uuid NOT NULL DEFAULT gen_random_uuid(),
    timeline_id uuid NOT NULL REFERENCES timelines(id),
    seq bigint NOT NULL CHECK (seq > 0),
    event_type text NOT NULL CHECK (length(btrim(event_type)) > 0),
    schema_version integer NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    payload jsonb NOT NULL,
    generation_id uuid NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (timeline_id, seq),
    UNIQUE (id)
);

CREATE INDEX story_events_timeline_type_idx ON story_events(timeline_id, event_type, seq DESC);
CREATE INDEX story_events_generation_idx ON story_events(generation_id) WHERE generation_id IS NOT NULL;

-- +goose Down
DROP TABLE story_events;
DROP TABLE timelines;
