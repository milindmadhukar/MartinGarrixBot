-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users(id)
VALUES ($1)
RETURNING *;

-- name: GetCoinsLeaderboard :many
SELECT id, garrix_coins, in_hand FROM users
ORDER BY garrix_coins + in_hand DESC OFFSET $1 LIMIT 10;

-- name: GetLevelsLeaderboard :many
SELECT id, total_xp FROM users
ORDER BY total_xp DESC OFFSET $1 LIMIT 10;

-- name: GetMessagesSentLeaderboard :many
SELECT id, messages_sent FROM users
ORDER BY messages_sent DESC OFFSET $1 LIMIT 10;

-- name: GetInHandLeaderboard :many
SELECT id, in_hand FROM users
ORDER BY in_hand DESC OFFSET $1 LIMIT 10;