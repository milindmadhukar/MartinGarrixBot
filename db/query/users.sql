-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users(id)
VALUES ($1)
RETURNING *;

-- Either of them seems redundant

-- name: AddCoins :exec
UPDATE users SET in_hand=in_hand + $2 WHERE id = $1;

-- name: UpdateCoins :exec
UPDATE users SET in_hand=$2 WHERE id = $1;

-- name: GetBalance :one
SELECT garrix_coins, in_hand FROM users WHERE id = $1;