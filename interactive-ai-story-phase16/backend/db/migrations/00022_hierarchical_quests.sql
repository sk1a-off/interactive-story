-- +goose Up
-- Objective hierarchy is stored in immutable-event-compatible metadata. New
-- stories write it directly; this migration canonically links legacy minor
-- objectives only when their Timeline has exactly one active major quest.
UPDATE story_threads
SET metadata = metadata || jsonb_build_object(
  'objectiveKind', CASE metadata->>'scope' WHEN 'global' THEN 'quest' ELSE 'task' END
)
WHERE metadata->>'kind'='objective' AND COALESCE(metadata->>'objectiveKind','')='';

CREATE TEMP TABLE quest_legacy_links ON COMMIT DROP AS
WITH single_active_major AS (
  SELECT timeline_id, (array_agg(id ORDER BY id))[1] AS parent_id
  FROM story_threads
  WHERE metadata->>'kind'='objective'
    AND metadata->>'scope'='global'
    AND status IN ('open','resolving')
  GROUP BY timeline_id
  HAVING count(*)=1
), candidates AS (
  SELECT minor.timeline_id, minor.id AS objective_id, major.parent_id,
         timeline.head_event_seq AS base_head,
         row_number() OVER (PARTITION BY minor.timeline_id ORDER BY minor.introduced_at_seq,minor.id) AS offset
  FROM story_threads minor
  JOIN single_active_major major ON major.timeline_id=minor.timeline_id
  JOIN timelines timeline ON timeline.id=minor.timeline_id
  WHERE minor.metadata->>'kind'='objective'
    AND minor.metadata->>'scope'='minor'
    AND COALESCE(minor.metadata->>'parentObjectiveId','')=''
)
SELECT * FROM candidates;

UPDATE story_threads objective
SET metadata = objective.metadata || jsonb_build_object('parentObjectiveId',link.parent_id),
    last_touched_at_seq = link.base_head + link.offset
FROM quest_legacy_links link
WHERE objective.id=link.objective_id AND objective.timeline_id=link.timeline_id;

INSERT INTO story_events(id,timeline_id,seq,event_type,schema_version,payload,created_at)
SELECT gen_random_uuid(), objective.timeline_id, link.base_head+link.offset,
       'objective_updated',1,
       jsonb_build_object(
         'objectiveId',objective.id,
         'parentObjectiveId',link.parent_id,
         'scope','minor',
         'kind',COALESCE(objective.metadata->>'objectiveKind','task'),
         'title',objective.title,
         'description',objective.summary,
         'successCriteria',COALESCE(objective.metadata->>'successCriteria',''),
         'status',CASE objective.status WHEN 'closed' THEN 'completed' WHEN 'abandoned' THEN 'failed' ELSE 'active' END,
         'progress',COALESCE((objective.metadata->>'progress')::int,0),
         'evidence',COALESCE(objective.metadata->>'evidence','')
       ),now()
FROM quest_legacy_links link
JOIN story_threads objective ON objective.id=link.objective_id AND objective.timeline_id=link.timeline_id;

UPDATE timelines timeline
SET head_event_seq=timeline.head_event_seq+counts.added,
    semantic_revision=timeline.semantic_revision+1,
    updated_at=now()
FROM (SELECT timeline_id,count(*) AS added FROM quest_legacy_links GROUP BY timeline_id) counts
WHERE timeline.id=counts.timeline_id;

-- +goose Down
-- Hierarchy migration appends Canon events and is intentionally irreversible.
SELECT 1;
