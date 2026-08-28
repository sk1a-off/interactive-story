-- +goose Up
ALTER TABLE story_setup_components
  DROP CONSTRAINT story_setup_components_component_key_check;
ALTER TABLE story_setup_components
  ADD CONSTRAINT story_setup_components_component_key_check CHECK(component_key IN (
    'story_bible','player','world','initial_cast','visual_bible','initial_quests','opening_situation'
  ));

-- Existing drafts receive a conservative quest hierarchy derived from their
-- opening goals. This keeps old, not-yet-started stories immediately playable.
INSERT INTO story_setup_components(story_id,component_key,revision,source,payload,locked,status,generation_id,updated_at)
SELECT s.id,
       'initial_quests',
       1,
       'mixed',
       jsonb_build_object('quests', jsonb_build_array(jsonb_build_object(
         'title', COALESCE(NULLIF(trim(o.payload->>'chapterGoal'),''), s.title),
         'description', '',
         'successCriteria', COALESCE(NULLIF(trim(o.payload->>'chapterGoal'),''), s.title),
         'stages', CASE
           WHEN NULLIF(trim(o.payload->>'sceneGoal'),'') IS NULL THEN '[]'::jsonb
           ELSE jsonb_build_array(jsonb_build_object(
             'kind','task',
             'title',trim(o.payload->>'sceneGoal'),
             'description','',
             'successCriteria',trim(o.payload->>'sceneGoal')
           ))
         END
       ))),
       false,
       'ready',
       NULL,
       now()
FROM stories s
JOIN story_setup_components o ON o.story_id=s.id AND o.component_key='opening_situation'
ON CONFLICT(story_id,component_key) DO NOTHING;

-- +goose Down
DELETE FROM story_setup_components WHERE component_key='initial_quests';
ALTER TABLE story_setup_components
  DROP CONSTRAINT story_setup_components_component_key_check;
ALTER TABLE story_setup_components
  ADD CONSTRAINT story_setup_components_component_key_check CHECK(component_key IN (
    'story_bible','player','world','initial_cast','visual_bible','opening_situation'
  ));
