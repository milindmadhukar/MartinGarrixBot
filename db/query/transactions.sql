-- name: AddCoins :exec
UPDATE users SET in_hand=in_hand + $2 WHERE id = $1;

-- name: UpdateCoins :exec
UPDATE users SET in_hand=$2 WHERE id = $1;

-- name: GetBalance :one
SELECT garrix_coins, in_hand FROM users WHERE id = $1;

-- name: WithdrawAmount :exec
UPDATE users SET in_hand = in_hand + $2, garrix_coins = garrix_coins - $2 WHERE id = $1;

-- name: DepositAmount :exec
UPDATE users SET in_hand = in_hand - $2, garrix_coins = garrix_coins + $2 WHERE id = $1;
