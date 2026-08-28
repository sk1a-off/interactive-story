-- +goose Up
-- Current architecture requires Story Setup generations before a Timeline exists.
ALTER TABLE generation_jobs ALTER COLUMN timeline_id DROP NOT NULL;

-- v1.12 baseline USER2-small embeddings are 384-dimensional.
-- This cast intentionally fails rather than silently truncating incompatible existing vectors.
DROP INDEX IF EXISTS memory_embeddings_vector_hnsw;
ALTER TABLE memory_embeddings
  ALTER COLUMN embedding TYPE vector(384) USING embedding::vector(384);
CREATE INDEX memory_embeddings_vector_hnsw
ON memory_embeddings USING hnsw (embedding vector_cosine_ops);

-- +goose Down
DROP INDEX IF EXISTS memory_embeddings_vector_hnsw;
ALTER TABLE memory_embeddings
  ALTER COLUMN embedding TYPE vector(1024) USING embedding::vector(1024);
CREATE INDEX memory_embeddings_vector_hnsw
ON memory_embeddings USING hnsw (embedding vector_cosine_ops);
ALTER TABLE generation_jobs ALTER COLUMN timeline_id SET NOT NULL;
