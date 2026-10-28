-- name: CreateUser :one
INSERT INTO users (
    username, email, hashed_password, display_name, avatar_url
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING id, username, email, display_name, avatar_url, created_at, updated_at;

-- name: GetUserByEmail :one
SELECT id, username, email, hashed_password, display_name, avatar_url, created_at, updated_at
FROM users
WHERE email = $1;

-- name: GetUserByUsername :one
SELECT id, username, email, hashed_password, display_name, avatar_url, created_at, updated_at
FROM users
WHERE username = $1;

-- name: GetUserByID :one
SELECT id, username, email, hashed_password, display_name, avatar_url, created_at, updated_at
FROM users
WHERE id = $1;

-- name: ExistsUserByEmailOrUsername :one
SELECT EXISTS (
    SELECT 1 FROM users
    WHERE email = $1 OR username = $2
);
