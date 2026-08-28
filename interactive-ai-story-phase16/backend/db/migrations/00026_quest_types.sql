-- +goose Up
-- Existing quest lines predate the main/side distinction. Preserve their
-- semantics as main; newly generated or edited lines store an explicit type.
UPDATE story_threads
SET metadata = metadata || jsonb_build_object('questType', 'main')
WHERE metadata->>'kind' = 'objective'
  AND metadata->>'scope' = 'global'
  AND COALESCE(metadata->>'questType', '') = '';

UPDATE story_threads AS stage
SET metadata = stage.metadata || jsonb_build_object(
    'questType', COALESCE(parent.metadata->>'questType', 'main')
)
FROM story_threads AS parent
WHERE stage.metadata->>'kind' = 'objective'
  AND stage.metadata->>'scope' = 'minor'
  AND COALESCE(stage.metadata->>'questType', '') = ''
  AND parent.id::text = stage.metadata->>'parentObjectiveId';

UPDATE story_threads
SET metadata = metadata || jsonb_build_object('questType', 'main')
WHERE metadata->>'kind' = 'objective'
  AND metadata->>'scope' = 'minor'
  AND COALESCE(metadata->>'questType', '') = '';

-- +goose Down
UPDATE story_threads
SET metadata = metadata - 'questType'
WHERE metadata->>'kind' = 'objective';
