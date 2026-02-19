-- name: CreateModlog :one
INSERT INTO modlogs (
    user_id,
    moderator_id,
    guild_id,
    log_type,
    reason,
    expires_at,
    active
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: GetModlogsByUser :many
SELECT * FROM modlogs
WHERE user_id = $1 AND guild_id = $2
ORDER BY time DESC
LIMIT $3 OFFSET $4;

-- name: GetModlogsByUserCount :one
SELECT COUNT(*) FROM modlogs
WHERE user_id = $1 AND guild_id = $2;

-- name: GetModlogsByGuild :many
SELECT * FROM modlogs
WHERE guild_id = $1
ORDER BY time DESC
LIMIT $2 OFFSET $3;

-- name: GetActiveTemporaryActions :many
SELECT * FROM modlogs
WHERE guild_id = $1 
  AND active = true 
  AND expires_at IS NOT NULL 
  AND expires_at > NOW()
ORDER BY expires_at ASC;

-- name: GetExpiredTemporaryActions :many
SELECT * FROM modlogs
WHERE guild_id = $1 
  AND active = true 
  AND expires_at IS NOT NULL 
  AND expires_at <= NOW();

-- name: DeactivateModlog :exec
UPDATE modlogs
SET active = false
WHERE id = $1;

-- name: GetModlogByID :one
SELECT * FROM modlogs
WHERE id = $1;

-- name: GetActiveTempBanForUser :one
SELECT * FROM modlogs
WHERE user_id = $1 
  AND guild_id = $2 
  AND log_type = 'tempban' 
  AND active = true 
  AND expires_at > NOW()
LIMIT 1;

-- name: GetActiveTempMuteForUser :one
SELECT * FROM modlogs
WHERE user_id = $1 
  AND guild_id = $2 
  AND log_type = 'tempmute' 
  AND active = true 
  AND expires_at > NOW()
LIMIT 1;
