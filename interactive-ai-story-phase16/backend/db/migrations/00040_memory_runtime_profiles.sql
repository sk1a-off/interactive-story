-- +goose Up
CREATE TABLE memory_index_profiles (
 revision bigserial PRIMARY KEY,
 owner_id uuid NOT NULL REFERENCES users(id),
 config_revision_id uuid NOT NULL REFERENCES ai_config_revisions(id),
 signature text NOT NULL,
 provider_name text NOT NULL,
 model_name text NOT NULL,
 reindex_job_id uuid NOT NULL UNIQUE REFERENCES embedding_reindex_jobs(id),
 status text NOT NULL CHECK(status IN ('building','serving','retired','failed')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX memory_profile_building_uq ON memory_index_profiles(owner_id,signature) WHERE status='building';
CREATE UNIQUE INDEX memory_profile_serving_uq ON memory_index_profiles(owner_id,signature) WHERE status='serving';
SELECT setval(pg_get_serial_sequence('memory_index_profiles','revision'),GREATEST(
 COALESCE((SELECT max(embedding_revision) FROM memory_embeddings),0),
 COALESCE((SELECT max(target_revision) FROM embedding_reindex_jobs),0),1000));
ALTER TABLE memory_index_jobs ADD COLUMN index_revision bigint REFERENCES memory_index_profiles(revision);
ALTER TABLE memory_index_jobs DROP CONSTRAINT memory_index_jobs_source_event_id_key,
 DROP CONSTRAINT memory_index_jobs_source_snapshot_id_key;
-- Keep legacy NULL profile jobs unique too; omit NULL event/snapshot sources.
CREATE UNIQUE INDEX memory_job_event_profile_uq ON memory_index_jobs(source_event_id,COALESCE(index_revision,0)) WHERE source_event_id IS NOT NULL;
CREATE UNIQUE INDEX memory_job_snapshot_profile_uq ON memory_index_jobs(source_snapshot_id,COALESCE(index_revision,0)) WHERE source_snapshot_id IS NOT NULL;
CREATE INDEX memory_jobs_profile_order_idx ON memory_index_jobs(index_revision,timeline_id,source_event_seq,created_at,id) WHERE status<>'completed';

-- +goose StatementBegin
CREATE FUNCTION fence_memory_index_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.index_revision IS DISTINCT FROM OLD.index_revision THEN RAISE EXCEPTION 'memory index profile is immutable'; END IF;
 IF NEW.index_revision IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM memory_index_profiles p JOIN stories s ON s.owner_id=p.owner_id
  WHERE p.revision=NEW.index_revision AND s.id=NEW.story_id
 ) THEN RAISE EXCEPTION 'memory index profile owner mismatch'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER memory_index_revision_guard BEFORE INSERT OR UPDATE OF index_revision ON memory_index_jobs
 FOR EACH ROW EXECUTE FUNCTION fence_memory_index_revision();

-- +goose StatementBegin
CREATE FUNCTION fanout_memory_index_job() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.index_revision IS NOT NULL THEN RETURN NEW; END IF;
 -- Shared with profile creation/publication: no event can fall between seed
 -- enumeration and a new profile becoming visible to the outbox.
 PERFORM pg_advisory_xact_lock(hashtextextended('memory-profile-lifecycle',0));
 INSERT INTO memory_index_jobs(story_id,timeline_id,source_kind,source_event_id,source_snapshot_id,source_event_seq,index_revision)
 SELECT NEW.story_id,NEW.timeline_id,NEW.source_kind,NEW.source_event_id,NEW.source_snapshot_id,NEW.source_event_seq,p.revision
 FROM memory_index_profiles p WHERE p.status IN ('building','serving')
 AND p.owner_id=(SELECT owner_id FROM stories WHERE id=NEW.story_id) ON CONFLICT DO NOTHING;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER memory_index_job_fanout AFTER INSERT ON memory_index_jobs FOR EACH ROW EXECUTE FUNCTION fanout_memory_index_job();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enqueue_memory_index_job() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_story uuid; source_seq bigint;
BEGIN
 SELECT story_id INTO source_story FROM timelines WHERE id=NEW.timeline_id;
 IF TG_TABLE_NAME='story_events' THEN
  source_seq:=NEW.seq;
  INSERT INTO memory_index_jobs(story_id,timeline_id,source_kind,source_event_id,source_event_seq)
  VALUES(source_story,NEW.timeline_id,'event',NEW.id,source_seq) ON CONFLICT DO NOTHING;
 ELSE
  source_seq:=NEW.event_seq;
  INSERT INTO memory_index_jobs(story_id,timeline_id,source_kind,source_snapshot_id,source_event_seq)
  VALUES(source_story,NEW.timeline_id,'snapshot',NEW.id,source_seq) ON CONFLICT DO NOTHING;
 END IF;
 INSERT INTO memory_index_state(timeline_id,story_id,observed_event_seq) VALUES(NEW.timeline_id,source_story,source_seq)
 ON CONFLICT(timeline_id) DO UPDATE SET observed_event_seq=GREATEST(memory_index_state.observed_event_seq,EXCLUDED.observed_event_seq);
 RETURN NEW;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER memory_index_job_fanout ON memory_index_jobs;
DROP FUNCTION fanout_memory_index_job();
DROP TRIGGER memory_index_revision_guard ON memory_index_jobs;
DROP FUNCTION fence_memory_index_revision();
DELETE FROM memory_index_jobs WHERE index_revision IS NOT NULL;
DROP INDEX memory_job_event_profile_uq;
DROP INDEX memory_job_snapshot_profile_uq;
ALTER TABLE memory_index_jobs DROP COLUMN index_revision;
ALTER TABLE memory_index_jobs ADD UNIQUE(source_event_id),ADD UNIQUE(source_snapshot_id);
DROP TABLE memory_index_profiles;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enqueue_memory_index_job() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_story uuid; source_seq bigint;
BEGIN
 SELECT story_id INTO source_story FROM timelines WHERE id=NEW.timeline_id;
 IF TG_TABLE_NAME='story_events' THEN
  source_seq:=NEW.seq;
  INSERT INTO memory_index_jobs(story_id,timeline_id,source_kind,source_event_id,source_event_seq)
  VALUES(source_story,NEW.timeline_id,'event',NEW.id,source_seq) ON CONFLICT(source_event_id) DO NOTHING;
 ELSE
  source_seq:=NEW.event_seq;
  INSERT INTO memory_index_jobs(story_id,timeline_id,source_kind,source_snapshot_id,source_event_seq)
  VALUES(source_story,NEW.timeline_id,'snapshot',NEW.id,source_seq) ON CONFLICT(source_snapshot_id) DO NOTHING;
 END IF;
 INSERT INTO memory_index_state(timeline_id,story_id,observed_event_seq) VALUES(NEW.timeline_id,source_story,source_seq)
 ON CONFLICT(timeline_id) DO UPDATE SET observed_event_seq=GREATEST(memory_index_state.observed_event_seq,EXCLUDED.observed_event_seq);
 RETURN NEW;
END $$;
-- +goose StatementEnd
