-- +goose Up
CREATE TABLE memory_embeddings (
    id uuid PRIMARY KEY,
    story_id uuid NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
    timeline_id uuid NOT NULL REFERENCES timelines(id) ON DELETE CASCADE,
    owner_type text NOT NULL CHECK (owner_type IN ('story','character','timeline')),
    owner_id uuid NOT NULL,
    kind text NOT NULL,
    content text NOT NULL,
    embedding vector(1024) NOT NULL,
    embedding_provider text NOT NULL,
    embedding_model text NOT NULL,
    embedding_revision bigint NOT NULL CHECK (embedding_revision > 0),
    valid_from_event_seq bigint NOT NULL CHECK (valid_from_event_seq >= 0),
    valid_to_event_seq bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (valid_to_event_seq IS NULL OR valid_to_event_seq >= valid_from_event_seq)
);
CREATE INDEX memory_embeddings_scope_idx
ON memory_embeddings(story_id,timeline_id,owner_type,owner_id,valid_from_event_seq,valid_to_event_seq);
CREATE INDEX memory_embeddings_vector_hnsw
ON memory_embeddings USING hnsw (embedding vector_cosine_ops);

CREATE TABLE embedding_reindex_jobs (
    id uuid PRIMARY KEY,
    story_id uuid REFERENCES stories(id) ON DELETE CASCADE,
    provider_name text NOT NULL,
    model_name text NOT NULL,
    target_revision bigint NOT NULL CHECK(target_revision>0),
    status text NOT NULL CHECK(status IN ('queued','running','completed','failed','cancelled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION memory_embedding_timeline_story_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM timelines t WHERE t.id=NEW.timeline_id AND t.story_id=NEW.story_id) THEN
   RAISE EXCEPTION 'memory timeline does not belong to story';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER memory_embedding_scope_guard
BEFORE INSERT OR UPDATE OF story_id,timeline_id ON memory_embeddings
FOR EACH ROW EXECUTE FUNCTION memory_embedding_timeline_story_guard();

-- +goose Down
DROP TRIGGER IF EXISTS memory_embedding_scope_guard ON memory_embeddings;
DROP FUNCTION IF EXISTS memory_embedding_timeline_story_guard();
DROP TABLE IF EXISTS embedding_reindex_jobs;
DROP TABLE IF EXISTS memory_embeddings;
