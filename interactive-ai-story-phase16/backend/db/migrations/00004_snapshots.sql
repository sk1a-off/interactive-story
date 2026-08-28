-- +goose Up
CREATE TABLE snapshots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    timeline_id uuid NOT NULL REFERENCES timelines(id),
    event_seq bigint NOT NULL CHECK (event_seq >= 0),
    state jsonb NOT NULL CHECK (jsonb_typeof(state) = 'object'),
    state_hash text NOT NULL CHECK (length(state_hash) = 64),
    schema_version integer NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    serializer_version integer NOT NULL DEFAULT 1 CHECK (serializer_version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (timeline_id, event_seq),
    CONSTRAINT snapshots_id_timeline_seq_unique UNIQUE(id, timeline_id, event_seq)
);

CREATE INDEX snapshots_timeline_seq_idx ON snapshots(timeline_id, event_seq DESC);

ALTER TABLE timelines
    ADD CONSTRAINT timelines_head_snapshot_fk
    FOREIGN KEY (head_snapshot_id)
    REFERENCES snapshots(id);

-- +goose Down
ALTER TABLE timelines DROP CONSTRAINT timelines_head_snapshot_fk;
DROP TABLE snapshots;
