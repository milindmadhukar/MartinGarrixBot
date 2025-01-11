-- name: AddCoins :exec
UPDATE users SET in_hand=in_hand + $2 WHERE id = $1;

-- name: GetBalance :one
SELECT garrix_coins, in_hand FROM users WHERE id = $1;

-- name: WithdrawAmount :exec
UPDATE users SET in_hand = in_hand + $2, garrix_coins = garrix_coins - $2 WHERE id = $1;

-- name: DepositAmount :exec
UPDATE users SET in_hand = in_hand - $2, garrix_coins = garrix_coins + $2 WHERE id = $1;

-- name: GiveCoins :exec
WITH sender_update AS (
    UPDATE users AS sender
    SET in_hand = sender.in_hand - $3
    WHERE sender.id = $1 AND sender.in_hand >= $3
    RETURNING sender.id
)
UPDATE users AS receiver
SET in_hand = receiver.in_hand + $3
WHERE receiver.id = $2 
AND EXISTS (SELECT 1 FROM sender_update);-- name: GiveCoins :exec