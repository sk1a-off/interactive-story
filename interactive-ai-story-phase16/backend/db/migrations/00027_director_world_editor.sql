-- +goose Up
-- Definitions remain story-scoped, while these state rows make Director edits
-- branch-local and replayable. "Deleting" an entity means setting active=false;
-- historical beats and foreign keys therefore remain valid.
ALTER TABLE character_states
    ADD COLUMN IF NOT EXISTS director_profile jsonb NOT NULL DEFAULT '{}';

ALTER TABLE characters DROP CONSTRAINT IF EXISTS characters_adult_age_check;
ALTER TABLE characters ADD CONSTRAINT characters_age_check CHECK(adult_age BETWEEN 1 AND 150);

CREATE TABLE location_states (
    timeline_id uuid NOT NULL REFERENCES timelines(id) ON DELETE CASCADE,
    location_id uuid NOT NULL REFERENCES locations(id),
    name_override text NOT NULL DEFAULT '',
    description_override text NOT NULL DEFAULT '',
    visual_profile_override jsonb NOT NULL DEFAULT '{}',
    active boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1 CHECK(version > 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(timeline_id, location_id)
);

-- Materialize setup entities for stories that were started before the live
-- world editor existed. Preserve the complete setup object in `core` so role,
-- personality, relationship and future fields are not discarded.
WITH player_rows AS (
    SELECT DISTINCT ON (c.story_id)
           c.story_id,
           NULLIF(BTRIM(c.payload->>'name'), '') AS name,
           LEAST(150, GREATEST(1, CASE WHEN COALESCE(c.payload->>'age','') ~ '^[0-9]+$' THEN (c.payload->>'age')::int ELSE 18 END)) AS age,
           c.payload AS core,
           COALESCE(c.payload->>'visualAnchorEn', '') AS visual_anchor
    FROM story_setup_components c
    WHERE c.component_key='player' AND jsonb_typeof(c.payload)='object'
    ORDER BY c.story_id, c.revision DESC
)
INSERT INTO characters(story_id,kind,name,adult_age,core,visual_profile)
SELECT p.story_id,'player',p.name,p.age,p.core,
       jsonb_build_object('anchorEn',p.visual_anchor)
FROM player_rows p
WHERE p.name IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM characters x WHERE x.story_id=p.story_id AND x.kind='player');

WITH cast_rows AS (
    SELECT DISTINCT c.story_id, entry.value AS profile,
           NULLIF(BTRIM(entry.value->>'name'), '') AS name,
           LEAST(150, GREATEST(1, CASE WHEN COALESCE(entry.value->>'age','') ~ '^[0-9]+$' THEN (entry.value->>'age')::int ELSE 18 END)) AS age
    FROM story_setup_components c
    CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(c.payload->'characters')='array' THEN c.payload->'characters' ELSE '[]'::jsonb END) entry(value)
    WHERE c.component_key='initial_cast' AND jsonb_typeof(entry.value)='object'
)
INSERT INTO characters(story_id,kind,name,adult_age,core,visual_profile)
SELECT r.story_id,'persistent_npc',r.name,r.age,r.profile,
       jsonb_build_object('anchorEn',COALESCE(r.profile->>'visualAnchorEn',''))
FROM cast_rows r
WHERE r.name IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM characters x
      WHERE x.story_id=r.story_id AND lower(x.name)=lower(r.name)
  );

INSERT INTO character_states(timeline_id,character_id,mood,current_goal,active,current_appearance,director_profile,version)
SELECT t.id,c.id,'','',true,'{}'::jsonb,c.core,1
FROM timelines t
JOIN characters c ON c.story_id=t.story_id
ON CONFLICT(timeline_id,character_id) DO NOTHING;

WITH location_rows AS (
    SELECT DISTINCT c.story_id, entry.value AS profile,
           NULLIF(BTRIM(entry.value->>'name'), '') AS name,
           COALESCE(entry.value->>'description','') AS description
    FROM story_setup_components c
    CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(c.payload->'locations')='array' THEN c.payload->'locations' ELSE '[]'::jsonb END) entry(value)
    WHERE c.component_key='world' AND jsonb_typeof(entry.value)='object'
)
INSERT INTO locations(story_id,name,description,visual_profile)
SELECT r.story_id,r.name,r.description,
       jsonb_build_object('anchorEn',COALESCE(r.profile->>'visualAnchorEn',''))
FROM location_rows r
WHERE r.name IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM locations x
      WHERE x.story_id=r.story_id AND lower(x.name)=lower(r.name)
  );

INSERT INTO location_states(timeline_id,location_id,name_override,description_override,visual_profile_override,active,version)
SELECT t.id,l.id,l.name,l.description,l.visual_profile,true,1
FROM timelines t
JOIN locations l ON l.story_id=t.story_id
ON CONFLICT(timeline_id,location_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS location_states;
ALTER TABLE character_states DROP COLUMN IF EXISTS director_profile;
ALTER TABLE characters DROP CONSTRAINT IF EXISTS characters_age_check;
ALTER TABLE characters ADD CONSTRAINT characters_adult_age_check CHECK(adult_age BETWEEN 18 AND 150);
