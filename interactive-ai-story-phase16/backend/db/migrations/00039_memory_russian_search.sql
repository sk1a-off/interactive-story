-- +goose Up
ALTER TABLE memory_embeddings ADD COLUMN search_text tsvector
 GENERATED ALWAYS AS (to_tsvector('russian'::regconfig, content)) STORED;
CREATE INDEX memory_embeddings_search_text_gin
 ON memory_embeddings USING gin(search_text);

-- +goose Down
DROP INDEX memory_embeddings_search_text_gin;
ALTER TABLE memory_embeddings DROP COLUMN search_text;
