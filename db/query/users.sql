-- name: GetUser :one
SELECT * FROM users WHERE id = $1 AND guild_id = $2;

-- name: CreateUser :one
INSERT INTO users(id, guild_id)
VALUES ($1, $2)
RETURNING *;

-- name: GetCoinsLeaderboard :many
SELECT id, garrix_coins, in_hand FROM users
WHERE guild_id = $1
ORDER BY garrix_coins + in_hand DESC OFFSET $2 LIMIT 10;

-- name: GetLevelsLeaderboard :many
SELECT id, total_xp FROM users
WHERE guild_id = $1
ORDER BY total_xp DESC OFFSET $2 LIMIT 10;

-- name: GetMessagesSentLeaderboard :many
SELECT id, messages_sent FROM users
WHERE guild_id = $1
ORDER BY messages_sent DESC OFFSET $2 LIMIT 10;

-- name: GetInHandLeaderboard :many
SELECT id, in_hand FROM users
WHERE guild_id = $1
ORDER BY in_hand DESC OFFSET $2 LIMIT 10;