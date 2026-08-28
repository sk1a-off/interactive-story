-- name: CreateStory :one
INSERT INTO stories (id, owner_id, title, description, status, semantic_revision)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetStory :one
SELECT * FROM stories WHERE id = $1;
