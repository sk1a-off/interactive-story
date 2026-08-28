-- +goose Up
ALTER TABLE story_setup_components
  DROP CONSTRAINT story_setup_components_component_key_check;
ALTER TABLE story_setup_components
  ADD CONSTRAINT story_setup_components_component_key_check CHECK(component_key IN (
    'story_bible','player','world','world_rules','initial_cast','visual_bible','initial_quests','opening_situation'
  ));

CREATE TABLE world_systems (
    timeline_id uuid NOT NULL REFERENCES timelines(id),
    system_id text NOT NULL CHECK(length(btrim(system_id))>0),
    name text NOT NULL CHECK(length(btrim(name))>0),
    kind text NOT NULL DEFAULT 'other' CHECK(kind IN ('world','magic','neural','technology','divine','mental','social','other')),
    description text NOT NULL DEFAULT '',
    resources jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(resources)='array'),
    status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','pending','archived')),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(timeline_id,system_id)
);

CREATE TABLE world_rules (
    timeline_id uuid NOT NULL REFERENCES timelines(id),
    rule_id text NOT NULL CHECK(length(btrim(rule_id))>0),
    system_id text NOT NULL CHECK(length(btrim(system_id))>0),
    title text NOT NULL CHECK(length(btrim(title))>0),
    category text NOT NULL DEFAULT 'law' CHECK(category IN ('axiom','law','mechanism','limit','cost','progression','exception','social','terminology')),
    severity text NOT NULL DEFAULT 'soft' CHECK(severity IN ('hard','soft','mystery','belief')),
    statement text NOT NULL CHECK(length(btrim(statement))>0),
    preconditions jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(preconditions)='array'),
    costs jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(costs)='array'),
    forbidden_results jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(forbidden_results)='array'),
    exceptions jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(exceptions)='array'),
    tags jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(tags)='array'),
    visibility text NOT NULL DEFAULT 'canon_only' CHECK(visibility IN ('canon_only','known_to_hero','public','hidden')),
    status text NOT NULL DEFAULT 'established' CHECK(status IN ('established','pending','superseded','archived')),
    exception_of text NOT NULL DEFAULT '',
    source text NOT NULL DEFAULT 'setup' CHECK(source IN ('setup','director','generation','migration')),
    evidence text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    established_at_seq bigint NOT NULL DEFAULT 1 CHECK(established_at_seq>0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(timeline_id,rule_id),
    FOREIGN KEY(timeline_id,system_id) REFERENCES world_systems(timeline_id,system_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX world_rules_active_idx ON world_rules(timeline_id,severity,system_id)
  WHERE status='established';
CREATE INDEX world_rules_tags_idx ON world_rules USING gin(tags);

CREATE TABLE world_resource_states (
    timeline_id uuid NOT NULL REFERENCES timelines(id),
    resource_id text NOT NULL CHECK(length(btrim(resource_id))>0),
    system_id text NOT NULL CHECK(length(btrim(system_id))>0),
    owner_type text NOT NULL DEFAULT 'world' CHECK(owner_type IN ('world','hero','character','location','faction')),
    owner_id uuid NULL,
    owner_key text NOT NULL DEFAULT '',
    name text NOT NULL CHECK(length(btrim(name))>0),
    unit text NOT NULL DEFAULT '',
    current_value numeric NOT NULL DEFAULT 0,
    min_value numeric NULL,
    max_value numeric NULL,
    visibility text NOT NULL DEFAULT 'canon_only' CHECK(visibility IN ('canon_only','known_to_hero','public','hidden')),
    version bigint NOT NULL DEFAULT 1 CHECK(version>0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(timeline_id,resource_id,owner_type,owner_key),
    CHECK((owner_type IN ('world','hero') AND owner_id IS NULL) OR (owner_type NOT IN ('world','hero') AND owner_id IS NOT NULL)),
    CHECK(min_value IS NULL OR current_value>=min_value),
    CHECK(max_value IS NULL OR current_value<=max_value),
    CHECK(min_value IS NULL OR max_value IS NULL OR min_value<=max_value),
    FOREIGN KEY(timeline_id,system_id) REFERENCES world_systems(timeline_id,system_id) DEFERRABLE INITIALLY DEFERRED
);

CREATE TABLE world_rule_audit (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    timeline_id uuid NOT NULL REFERENCES timelines(id),
    generation_id uuid NULL,
    beat_id uuid NULL,
    rule_id text NOT NULL DEFAULT '',
    severity text NOT NULL CHECK(severity IN ('hard','soft','info')),
    evidence text NOT NULL DEFAULT '',
    repair_instruction text NOT NULL DEFAULT '',
    status text NOT NULL CHECK(status IN ('detected','repaired','waived','blocking','passed')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX world_rule_audit_timeline_idx ON world_rule_audit(timeline_id,created_at DESC);

-- Preserve the only world laws older setups could express. They become
-- addressable legacy cards instead of remaining untyped prompt prose.
INSERT INTO story_setup_components(story_id,component_key,revision,source,payload,locked,status,generation_id,updated_at)
SELECT s.id,'world_rules',1,'mixed',jsonb_build_object(
  'systems',jsonb_build_array(jsonb_build_object(
    'id','world_foundations',
    'name',COALESCE(NULLIF(btrim(w.payload->>'name'),''),'Основы мира'),
    'kind','world',
    'description',COALESCE(w.payload->>'summary',''),
    'resources','[]'::jsonb
  )),
  'rules',COALESCE((
    SELECT jsonb_agg(jsonb_build_object(
      'id','LEGACY-' || lpad(ordinality::text,3,'0'),
      'systemId','world_foundations',
      'title','Унаследованное правило ' || ordinality,
      'category','law',
      'severity','hard',
      'statement',rule_text,
      'preconditions','[]'::jsonb,
      'costs','[]'::jsonb,
      'forbiddenResults','[]'::jsonb,
      'exceptions','[]'::jsonb,
      'tags','[]'::jsonb,
      'visibility','canon_only',
      'status','established'
    ) ORDER BY ordinality)
    FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(b.payload->'narrativeRules')='array' THEN b.payload->'narrativeRules' ELSE '[]'::jsonb END) WITH ORDINALITY AS legacy(rule_text,ordinality)
  ),jsonb_build_array(jsonb_build_object(
      'id','LEGACY-001','systemId','world_foundations','title','Исходное описание мира',
      'category','law','severity','soft','statement',COALESCE(NULLIF(w.payload->>'summary',''),'Сохранять внутреннюю непротиворечивость мира.'),
      'preconditions','[]'::jsonb,'costs','[]'::jsonb,'forbiddenResults','[]'::jsonb,
      'exceptions','[]'::jsonb,'tags','[]'::jsonb,'visibility','canon_only','status','established'
  ))),
  'glossary','[]'::jsonb
),false,'ready',NULL,now()
FROM stories s
LEFT JOIN story_setup_components w ON w.story_id=s.id AND w.component_key='world'
LEFT JOIN story_setup_components b ON b.story_id=s.id AND b.component_key='story_bible'
ON CONFLICT(story_id,component_key) DO NOTHING;

-- Existing started branches receive the migrated rules as materialized state
-- and Canon events, so replay, savepoints and forks see the same rulebook.
INSERT INTO world_systems(timeline_id,system_id,name,kind,description,resources,status,version)
SELECT t.id,system->>'id',system->>'name',COALESCE(NULLIF(system->>'kind',''),'other'),COALESCE(system->>'description',''),
       COALESCE(system->'resources','[]'::jsonb),'active',1
FROM timelines t
JOIN story_setup_components c ON c.story_id=t.story_id AND c.component_key='world_rules'
CROSS JOIN LATERAL jsonb_array_elements(c.payload->'systems') system
ON CONFLICT(timeline_id,system_id) DO NOTHING;

INSERT INTO world_rules(timeline_id,rule_id,system_id,title,category,severity,statement,preconditions,costs,forbidden_results,exceptions,tags,visibility,status,source,evidence,version,established_at_seq)
SELECT t.id,rule->>'id',rule->>'systemId',rule->>'title',COALESCE(NULLIF(rule->>'category',''),'law'),
       COALESCE(NULLIF(rule->>'severity',''),'soft'),rule->>'statement',COALESCE(rule->'preconditions','[]'::jsonb),
       COALESCE(rule->'costs','[]'::jsonb),COALESCE(rule->'forbiddenResults','[]'::jsonb),COALESCE(rule->'exceptions','[]'::jsonb),
       COALESCE(rule->'tags','[]'::jsonb),COALESCE(NULLIF(rule->>'visibility',''),'canon_only'),
       COALESCE(NULLIF(rule->>'status',''),'established'),'migration','',1,GREATEST(t.head_event_seq,1)
FROM timelines t
JOIN story_setup_components c ON c.story_id=t.story_id AND c.component_key='world_rules'
CROSS JOIN LATERAL jsonb_array_elements(c.payload->'rules') rule
ON CONFLICT(timeline_id,rule_id) DO NOTHING;

CREATE TEMP TABLE world_rule_seed_events ON COMMIT DROP AS
SELECT timeline_id,event_type,payload,
       row_number() OVER(PARTITION BY timeline_id ORDER BY event_order,stable_id) AS event_offset
FROM (
  SELECT ws.timeline_id,0 AS event_order,ws.system_id AS stable_id,'world_system_upserted'::text AS event_type,
         jsonb_build_object('systemId',ws.system_id,'name',ws.name,'kind',ws.kind,'description',ws.description,
                            'resources',ws.resources,'status',ws.status,'version',ws.version) AS payload
  FROM world_systems ws
  UNION ALL
  SELECT wr.timeline_id,1,wr.rule_id,'world_rule_upserted',
         jsonb_build_object('ruleId',wr.rule_id,'systemId',wr.system_id,'title',wr.title,'category',wr.category,
                            'severity',wr.severity,'statement',wr.statement,'preconditions',wr.preconditions,'costs',wr.costs,
                            'forbiddenResults',wr.forbidden_results,'exceptions',wr.exceptions,'tags',wr.tags,
                            'visibility',wr.visibility,'status',wr.status,'exceptionOf',wr.exception_of,
                            'source',wr.source,'evidence',wr.evidence,'version',wr.version)
  FROM world_rules wr
) seeded;

INSERT INTO story_events(id,timeline_id,seq,event_type,schema_version,payload,created_at)
SELECT gen_random_uuid(),seed.timeline_id,t.head_event_seq+seed.event_offset,seed.event_type,1,seed.payload,now()
FROM world_rule_seed_events seed JOIN timelines t ON t.id=seed.timeline_id;

UPDATE timelines t
SET head_event_seq=t.head_event_seq+counts.added,
    semantic_revision=t.semantic_revision+1,
    updated_at=now()
FROM (SELECT timeline_id,count(*) AS added FROM world_rule_seed_events GROUP BY timeline_id) counts
WHERE t.id=counts.timeline_id;

-- +goose Down
-- The migration appends Canon events and is intentionally irreversible.
SELECT 1;
