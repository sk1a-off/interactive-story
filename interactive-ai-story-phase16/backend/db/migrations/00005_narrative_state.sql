-- +goose Up
CREATE TABLE characters (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), story_id uuid NOT NULL REFERENCES stories(id),
 kind text NOT NULL CHECK(kind IN ('player','persistent_npc','temporary_promoted')), name text NOT NULL,
 adult_age smallint NOT NULL CHECK(adult_age BETWEEN 18 AND 150), core jsonb NOT NULL DEFAULT '{}',
 voice_profile jsonb NOT NULL DEFAULT '{}', visual_profile jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(id,story_id));
CREATE UNIQUE INDEX one_player_per_story_idx ON characters(story_id) WHERE kind='player';
CREATE TABLE locations (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), story_id uuid NOT NULL REFERENCES stories(id), name text NOT NULL, description text NOT NULL DEFAULT '', visual_profile jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE character_states (timeline_id uuid NOT NULL REFERENCES timelines(id), character_id uuid NOT NULL REFERENCES characters(id), location_id uuid NULL REFERENCES locations(id), mood text NOT NULL DEFAULT '', current_goal text NOT NULL DEFAULT '', active boolean NOT NULL DEFAULT true, current_appearance jsonb NOT NULL DEFAULT '{}', version bigint NOT NULL DEFAULT 1 CHECK(version>0), updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(timeline_id,character_id));
CREATE TABLE relationships (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), timeline_id uuid NOT NULL REFERENCES timelines(id), from_character_id uuid NOT NULL REFERENCES characters(id), to_character_id uuid NOT NULL REFERENCES characters(id), status text NOT NULL DEFAULT '', summary text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(timeline_id,from_character_id,to_character_id), CHECK(from_character_id<>to_character_id));
CREATE TABLE stats (timeline_id uuid NOT NULL REFERENCES timelines(id), owner_type text NOT NULL CHECK(owner_type IN ('character','relationship','world','item','scene')), owner_id uuid NOT NULL, key text NOT NULL, value_type text NOT NULL CHECK(value_type IN ('number','bool','string','enum','tags')), value jsonb NOT NULL, target_value jsonb NULL, evolution_mode text NOT NULL DEFAULT 'none' CHECK(evolution_mode IN ('none','immediate','retcon','gradual')), evolution_meta jsonb NOT NULL DEFAULT '{}', updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(timeline_id,owner_type,owner_id,key));
CREATE TABLE chapters (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), timeline_id uuid NOT NULL REFERENCES timelines(id), number integer NOT NULL CHECK(number>0), title text NOT NULL DEFAULT '', goal text NOT NULL DEFAULT '', tone text NOT NULL DEFAULT '', status text NOT NULL CHECK(status IN ('planned','active','completing','completed','superseded')), summary jsonb NULL, started_at timestamptz NOT NULL DEFAULT now(), ended_at timestamptz NULL, UNIQUE(timeline_id,number));
CREATE TABLE scenes (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), chapter_id uuid NOT NULL REFERENCES chapters(id), number integer NOT NULL CHECK(number>0), location_id uuid NULL REFERENCES locations(id), story_time jsonb NOT NULL DEFAULT '{}', mood text NOT NULL DEFAULT '', goal text NOT NULL DEFAULT '', status text NOT NULL CHECK(status IN ('planned','active','awaiting_player','completing','completed','superseded')), started_at timestamptz NOT NULL DEFAULT now(), ended_at timestamptz NULL, UNIQUE(chapter_id,number));
CREATE TABLE beats (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), scene_id uuid NOT NULL REFERENCES scenes(id), position integer NOT NULL CHECK(position>0), kind text NOT NULL CHECK(kind IN ('image','dialogue','description','mixed','reaction','transition','choice','continue')), content jsonb NOT NULL, status text NOT NULL CHECK(status IN ('planned','generated','committed','superseded','rolled_back')), is_active boolean NOT NULL DEFAULT true, generation_id uuid NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE UNIQUE INDEX beats_one_active_position_idx ON beats(scene_id,position) WHERE is_active;
CREATE TABLE scene_participants (scene_id uuid NOT NULL REFERENCES scenes(id) ON DELETE CASCADE, character_id uuid NOT NULL REFERENCES characters(id), role text NOT NULL DEFAULT 'present' CHECK(role IN ('present','focus','background')), joined_at_position integer NULL, left_at_position integer NULL, active boolean NOT NULL DEFAULT true, PRIMARY KEY(scene_id,character_id));
CREATE INDEX scene_participants_active_idx ON scene_participants(scene_id) WHERE active;
CREATE TABLE scene_ephemeral_npcs (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), scene_id uuid NOT NULL REFERENCES scenes(id) ON DELETE CASCADE, name text NOT NULL DEFAULT '', role text NOT NULL DEFAULT '', description text NOT NULL DEFAULT '', state jsonb NOT NULL DEFAULT '{}', promoted_character_id uuid NULL REFERENCES characters(id), created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE facts (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), timeline_id uuid NOT NULL REFERENCES timelines(id), subject_type text NOT NULL, subject_id uuid NOT NULL, predicate text NOT NULL, object jsonb NOT NULL, status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','invalidated','superseded')), source_event_id uuid NULL, valid_from_seq bigint NOT NULL CHECK(valid_from_seq>0), invalidated_at_seq bigint NULL, created_at timestamptz NOT NULL DEFAULT now(), CHECK(invalidated_at_seq IS NULL OR invalidated_at_seq>=valid_from_seq));
CREATE TABLE knowledge_entries (timeline_id uuid NOT NULL REFERENCES timelines(id), fact_id uuid NOT NULL REFERENCES facts(id), knower_character_id uuid NOT NULL REFERENCES characters(id), confidence real NOT NULL DEFAULT 1 CHECK(confidence BETWEEN 0 AND 1), learned_at_event_seq bigint NOT NULL CHECK(learned_at_event_seq>0), invalidated_at_event_seq bigint NULL, PRIMARY KEY(timeline_id,fact_id,knower_character_id));
CREATE TABLE character_beliefs (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), timeline_id uuid NOT NULL REFERENCES timelines(id), character_id uuid NOT NULL REFERENCES characters(id), subject_type text NOT NULL, subject_id uuid NULL, predicate text NOT NULL, object jsonb NOT NULL, stance text NOT NULL CHECK(stance IN ('believes','suspects','doubts','disbelieves')), confidence real NOT NULL CHECK(confidence BETWEEN 0 AND 1), status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','invalidated','superseded')), source_event_seq bigint NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE items (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), story_id uuid NOT NULL REFERENCES stories(id), name text NOT NULL, description text NOT NULL DEFAULT '', visual_profile jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(id,story_id));
CREATE TABLE item_states (timeline_id uuid NOT NULL REFERENCES timelines(id), item_id uuid NOT NULL REFERENCES items(id), owner_character_id uuid NULL REFERENCES characters(id), location_id uuid NULL REFERENCES locations(id), condition text NOT NULL DEFAULT '', state jsonb NOT NULL DEFAULT '{}', version bigint NOT NULL DEFAULT 1 CHECK(version>0), updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(timeline_id,item_id), CHECK(owner_character_id IS NULL OR location_id IS NULL));
CREATE TABLE story_threads (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), timeline_id uuid NOT NULL REFERENCES timelines(id), title text NOT NULL, summary text NOT NULL DEFAULT '', importance real NOT NULL DEFAULT .5 CHECK(importance BETWEEN 0 AND 1), status text NOT NULL CHECK(status IN ('open','dormant','resolving','closed','abandoned')), metadata jsonb NOT NULL DEFAULT '{}', introduced_at_seq bigint NOT NULL CHECK(introduced_at_seq>0), last_touched_at_seq bigint NOT NULL CHECK(last_touched_at_seq>=introduced_at_seq), created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE director_instructions (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), timeline_id uuid NOT NULL REFERENCES timelines(id), instruction_text text NOT NULL, scope text NOT NULL CHECK(scope IN ('next_beat','scene','chapter','temporary','persistent')), priority text NOT NULL DEFAULT 'normal' CHECK(priority IN ('low','normal','high','hard')), status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','completed','expired','cancelled')), created_at_seq bigint NOT NULL CHECK(created_at_seq>=0), expires_at jsonb NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX facts_active_idx ON facts(timeline_id,predicate) WHERE status='active';
CREATE INDEX knowledge_character_idx ON knowledge_entries(timeline_id,knower_character_id);
CREATE INDEX beliefs_character_idx ON character_beliefs(timeline_id,character_id) WHERE status='active';
CREATE INDEX threads_active_idx ON story_threads(timeline_id,importance DESC) WHERE status IN ('open','dormant','resolving');
CREATE INDEX director_active_idx ON director_instructions(timeline_id,priority,created_at DESC) WHERE status='active';

-- Cross-story UUID references are forbidden even when each UUID exists independently.
-- +goose StatementBegin
CREATE FUNCTION enforce_character_timeline_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE timeline_story uuid; character_story uuid;
BEGIN
 SELECT story_id INTO timeline_story FROM timelines WHERE id=NEW.timeline_id;
 SELECT story_id INTO character_story FROM characters WHERE id=NEW.character_id;
 IF timeline_story IS DISTINCT FROM character_story THEN RAISE EXCEPTION 'character/timeline story scope mismatch'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER character_states_scope BEFORE INSERT OR UPDATE ON character_states FOR EACH ROW EXECUTE FUNCTION enforce_character_timeline_scope();
CREATE TRIGGER beliefs_scope BEFORE INSERT OR UPDATE ON character_beliefs FOR EACH ROW EXECUTE FUNCTION enforce_character_timeline_scope();

-- +goose StatementBegin
CREATE FUNCTION enforce_relationship_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE timeline_story uuid; from_story uuid; to_story uuid;
BEGIN
 SELECT story_id INTO timeline_story FROM timelines WHERE id=NEW.timeline_id;
 SELECT story_id INTO from_story FROM characters WHERE id=NEW.from_character_id;
 SELECT story_id INTO to_story FROM characters WHERE id=NEW.to_character_id;
 IF timeline_story IS DISTINCT FROM from_story OR timeline_story IS DISTINCT FROM to_story THEN RAISE EXCEPTION 'relationship story scope mismatch'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER relationships_scope BEFORE INSERT OR UPDATE ON relationships FOR EACH ROW EXECUTE FUNCTION enforce_relationship_scope();

-- +goose StatementBegin
CREATE FUNCTION enforce_item_state_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE timeline_story uuid; item_story uuid; owner_story uuid; location_story uuid;
BEGIN
 SELECT story_id INTO timeline_story FROM timelines WHERE id=NEW.timeline_id;
 SELECT story_id INTO item_story FROM items WHERE id=NEW.item_id;
 IF NEW.owner_character_id IS NOT NULL THEN SELECT story_id INTO owner_story FROM characters WHERE id=NEW.owner_character_id; END IF;
 IF NEW.location_id IS NOT NULL THEN SELECT story_id INTO location_story FROM locations WHERE id=NEW.location_id; END IF;
 IF timeline_story IS DISTINCT FROM item_story OR (NEW.owner_character_id IS NOT NULL AND timeline_story IS DISTINCT FROM owner_story) OR (NEW.location_id IS NOT NULL AND timeline_story IS DISTINCT FROM location_story) THEN RAISE EXCEPTION 'item state story scope mismatch'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER item_states_scope BEFORE INSERT OR UPDATE ON item_states FOR EACH ROW EXECUTE FUNCTION enforce_item_state_scope();

-- +goose StatementBegin
CREATE FUNCTION enforce_knowledge_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE fact_timeline uuid; timeline_story uuid; character_story uuid;
BEGIN
 SELECT timeline_id INTO fact_timeline FROM facts WHERE id=NEW.fact_id;
 SELECT story_id INTO timeline_story FROM timelines WHERE id=NEW.timeline_id;
 SELECT story_id INTO character_story FROM characters WHERE id=NEW.knower_character_id;
 IF fact_timeline IS DISTINCT FROM NEW.timeline_id OR timeline_story IS DISTINCT FROM character_story THEN RAISE EXCEPTION 'knowledge scope mismatch'; END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER knowledge_scope BEFORE INSERT OR UPDATE ON knowledge_entries FOR EACH ROW EXECUTE FUNCTION enforce_knowledge_scope();

-- +goose Down
DROP TABLE director_instructions, story_threads, item_states, items, character_beliefs, knowledge_entries, facts, scene_ephemeral_npcs, scene_participants, beats, scenes, chapters, stats, relationships, character_states, locations, characters;
DROP FUNCTION enforce_knowledge_scope(), enforce_item_state_scope(), enforce_relationship_scope(), enforce_character_timeline_scope();
