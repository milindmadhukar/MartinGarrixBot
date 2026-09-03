-- name: CreateBackground :one
-- The ON CONFLICT arm is a no-op update rather than DO NOTHING so RETURNING
-- always yields a row -- this doubles as the idempotent "ensure the built-in
-- backgrounds are seeded" call made on every bot startup.
INSERT INTO backgrounds (filename, uploaded_by)
VALUES ($1, $2)
ON CONFLICT (filename) DO UPDATE SET filename = EXCLUDED.filename
RETURNING *;

-- name: ListBackgrounds :many
SELECT * FROM backgrounds ORDER BY id;

-- name: GetBackground :one
SELECT * FROM backgrounds WHERE id = $1;

-- name: DeleteBackground :exec
-- guild_backgrounds.background_id cascades (ON DELETE CASCADE) and
-- guilds.background_cycle_background_id is nulled out (ON DELETE SET NULL),
-- so a delete here can never leave either dangling.
DELETE FROM backgrounds WHERE id = $1;

-- name: ListGuildBackgrounds :many
SELECT b.* FROM backgrounds b
JOIN guild_backgrounds gb ON gb.background_id = b.id
WHERE gb.guild_id = $1
ORDER BY b.id;

-- name: ClearGuildBackgrounds :exec
DELETE FROM guild_backgrounds WHERE guild_id = $1;

-- name: AddGuildBackground :exec
INSERT INTO guild_backgrounds (guild_id, background_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: GetGuildBackgroundSettings :one
SELECT background_mode, background_cycle_background_id
FROM guilds
WHERE guild_id = $1;

-- name: SetGuildBackgroundMode :exec
UPDATE guilds SET background_mode = $2 WHERE guild_id = $1;

-- name: SetGuildBackgroundCyclePosition :exec
UPDATE guilds SET background_cycle_background_id = $2 WHERE guild_id = $1;
