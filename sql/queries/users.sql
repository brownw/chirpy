-- name: CreateUser :one
INSERT INTO users (email)
VALUES ($1)
RETURNING *;

-- name: ResetUsers :execrows
DELETE FROM users;