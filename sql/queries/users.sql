-- name: CreateUser :one
INSERT INTO users (email, hashed_password)
VALUES ($1, $2)
RETURNING *;

-- name: ResetUsers :execrows
DELETE FROM users;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;