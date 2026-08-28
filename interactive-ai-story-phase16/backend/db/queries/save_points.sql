-- name: GetSavePoint :one
SELECT id, story_id, timeline_id, snapshot_id, event_seq, name, note, kind, pinned,
       chapter_id, scene_id, beat_id, created_at
FROM save_points
WHERE id = $1;

-- name: ListTimelineSavePoints :many
SELECT id, story_id, timeline_id, snapshot_id, event_seq, name, note, kind, pinned,
       chapter_id, scene_id, beat_id, created_at
FROM save_points
WHERE timeline_id = $1
ORDER BY created_at DESC, id;

-- name: CreateSavePoint :one
INSERT INTO save_points (
  id, story_id, timeline_id, snapshot_id, event_seq, name, note, kind, pinned,
  chapter_id, scene_id, beat_id, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
RETURNING *;
