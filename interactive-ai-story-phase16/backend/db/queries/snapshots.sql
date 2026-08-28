-- name: CreateSnapshot :one
INSERT INTO snapshots (id, timeline_id, event_seq, state, state_hash, schema_version, serializer_version, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetSnapshot :one
SELECT * FROM snapshots WHERE id = $1;

-- name: GetLatestSnapshot :one
SELECT * FROM snapshots
WHERE timeline_id = $1
ORDER BY event_seq DESC
LIMIT 1;
