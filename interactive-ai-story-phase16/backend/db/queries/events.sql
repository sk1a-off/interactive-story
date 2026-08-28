-- name: AppendStoryEvent :exec
INSERT INTO story_events (id, timeline_id, seq, event_type, schema_version, payload, generation_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListStoryEvents :many
SELECT *
FROM story_events
WHERE timeline_id = $1 AND seq > $2
ORDER BY seq ASC;
