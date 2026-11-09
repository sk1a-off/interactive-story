-- +goose Up
ALTER TABLE memory_embeddings
 ADD COLUMN source_key text,
 ADD COLUMN document_class text CHECK(document_class IN ('historical_beat','branch_state')),
 ADD COLUMN content_hash text CHECK(content_hash IS NULL OR length(content_hash)=64),
 ADD COLUMN index_job_id uuid REFERENCES memory_index_jobs(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX memory_embeddings_active_source_uq
 ON memory_embeddings(timeline_id,owner_type,owner_id,source_key,embedding_provider,embedding_model,embedding_revision)
 WHERE source_key IS NOT NULL AND valid_to_event_seq IS NULL;

CREATE TABLE memory_index_publications (
 job_id uuid PRIMARY KEY REFERENCES memory_index_jobs(id) ON DELETE CASCADE,
 timeline_id uuid NOT NULL REFERENCES timelines(id) ON DELETE CASCADE,
 provider_name text NOT NULL,
 model_name text NOT NULL,
 embedding_revision bigint NOT NULL CHECK(embedding_revision>0),
 event_seq bigint NOT NULL CHECK(event_seq>=0),
 published_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE memory_index_publication_state (
 timeline_id uuid NOT NULL REFERENCES timelines(id) ON DELETE CASCADE,
 provider_name text NOT NULL,
 model_name text NOT NULL,
 embedding_revision bigint NOT NULL CHECK(embedding_revision>0),
 published_event_seq bigint NOT NULL CHECK(published_event_seq>=0),
 PRIMARY KEY(timeline_id,provider_name,model_name,embedding_revision)
);

-- +goose Down
DROP TABLE memory_index_publication_state;
DROP TABLE memory_index_publications;
DROP INDEX memory_embeddings_active_source_uq;
ALTER TABLE memory_embeddings DROP COLUMN index_job_id,DROP COLUMN content_hash,DROP COLUMN document_class,DROP COLUMN source_key;
