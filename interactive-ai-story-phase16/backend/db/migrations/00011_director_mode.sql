-- +goose Up
CREATE TABLE director_audit_log (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    timeline_id uuid NOT NULL REFERENCES timelines(id) ON DELETE CASCADE,
    save_point_id uuid NULL REFERENCES save_points(id) ON DELETE SET NULL,
    command_type text NOT NULL,
    target_type text NOT NULL,
    target_id uuid NULL,
    before_state jsonb NULL,
    after_state jsonb NULL,
    note text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX director_audit_timeline_idx ON director_audit_log(timeline_id, created_at DESC);

-- Director exact edits must participate in stale-write protection even when
-- they do not append an AI-generated Story event.
ALTER TABLE timelines ADD COLUMN IF NOT EXISTS semantic_revision bigint NOT NULL DEFAULT 1 CHECK(semantic_revision > 0);

-- +goose Down
ALTER TABLE timelines DROP COLUMN IF EXISTS semantic_revision;
DROP TABLE director_audit_log;
