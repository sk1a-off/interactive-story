-- name: CreateTimeline :one
INSERT INTO timelines (id, story_id, parent_timeline_id, name, status, head_event_seq)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetTimeline :one
SELECT * FROM timelines WHERE id = $1;

-- name: LockTimeline :one
SELECT * FROM timelines WHERE id = $1 FOR UPDATE;

-- name: AdvanceTimelineHead :execrows
UPDATE timelines
SET head_event_seq = $2,
    updated_at = now()
WHERE id = $1;

-- name: SetTimelineHeadSnapshot :execrows
UPDATE timelines
SET head_snapshot_id = $2,
    updated_at = now()
WHERE id = $1;

-- name: ListStoryTimelines :many
SELECT id, story_id, parent_timeline_id, forked_from_save_id, name, status,
       head_event_seq, head_snapshot_id, created_at, updated_at
FROM timelines
WHERE story_id = $1 AND status <> 'deleted'
ORDER BY created_at, id;
