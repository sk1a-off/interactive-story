-- +goose Up
CREATE TABLE ai_active_config (
    singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
    config_revision_id uuid NOT NULL REFERENCES ai_config_revisions(id) ON DELETE RESTRICT,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Config revisions are immutable configuration/provenance records. Switching
-- active config changes only the singleton pointer used for future jobs.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION reject_ai_config_revision_update()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ai config revisions are immutable';
END $$;
-- +goose StatementEnd
CREATE TRIGGER ai_config_revisions_immutable
BEFORE UPDATE OR DELETE ON ai_config_revisions
FOR EACH ROW EXECUTE FUNCTION reject_ai_config_revision_update();

INSERT INTO ai_active_config(singleton,config_revision_id)
SELECT true,id FROM ai_config_revisions ORDER BY revision DESC LIMIT 1
ON CONFLICT(singleton) DO NOTHING;

-- +goose Down
DROP TRIGGER IF EXISTS ai_config_revisions_immutable ON ai_config_revisions;
DROP FUNCTION IF EXISTS reject_ai_config_revision_update();
DROP TABLE IF EXISTS ai_active_config;
