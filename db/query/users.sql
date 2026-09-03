-- name: GetUser :one
SELECT * FROM users WHERE id = $1 AND guild_id = $2;

-- name: CreateUser :one
INSERT INTO users(id, guild_id)
VALUES ($1, $2)
RETURNING *;

-- The four leaderboards below back /leaderboard. Two things they all need:
--
--   NULLS LAST -- migration 000025 made these columns NOT NULL, so nothing can
--   sort NULL-first any more, but saying it here means a future nullable column
--   cannot quietly reintroduce the bug that put ten zero-XP members at the top
--   of the levels board.
--
--   a trailing `id` -- a deterministic tiebreak. Without it members tied on the
--   same value swap places between queries, so paging past the first page can
--   repeat or skip rows.

-- name: GetCoinsLeaderboard :many
SELECT id, stmpd_coins, in_hand FROM users
WHERE guild_id = $1
ORDER BY stmpd_coins + in_hand DESC NULLS LAST, id
OFFSET $2 LIMIT $3;

-- name: GetLevelsLeaderboard :many
SELECT id, total_xp FROM users
WHERE guild_id = $1
ORDER BY total_xp DESC NULLS LAST, id
OFFSET $2 LIMIT $3;

-- name: GetMessagesSentLeaderboard :many
SELECT id, messages_sent FROM users
WHERE guild_id = $1
ORDER BY messages_sent DESC NULLS LAST, id
OFFSET $2 LIMIT $3;

-- name: GetInHandLeaderboard :many
SELECT id, in_hand FROM users
WHERE guild_id = $1
ORDER BY in_hand DESC NULLS LAST, id
OFFSET $2 LIMIT $3;

-- name: GetLeaderboardCount :one
-- Total rows behind any of the leaderboards above, for the page count.
SELECT COUNT(*)::bigint FROM users WHERE guild_id = $1;

-- name: GetUserLevelData :one
WITH user_ranks AS (
  SELECT *,
         RANK() OVER (PARTITION BY guild_id ORDER BY total_xp DESC NULLS LAST) as rank
  FROM users
  WHERE guild_id = $2
)
SELECT *
FROM user_ranks
WHERE id = $1 AND guild_id = $2;

-- name: ImportUserStats :exec
-- Overwrites one member's XP and message count from an external source of
-- truth, creating the row if this is somebody the bot has never seen.
--
-- Called once per member rather than as one bulk statement, deliberately: each
-- call is its own auto-committed transaction, so a row lock is held for barely a
-- millisecond and the live bot's MessageSent never queues behind the import.
-- Batching every row into one transaction is what made a first attempt block the
-- bot for 21 seconds.
--
-- Coins are deliberately absent. stmpd_coins and in_hand are this bot's own
-- economy with no counterpart to import, so an import must not touch them.
INSERT INTO users (id, guild_id, total_xp, messages_sent)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id, guild_id) DO UPDATE
SET total_xp      = EXCLUDED.total_xp,
    messages_sent = EXCLUDED.messages_sent;

-- name: GetUsersInGuild :many
-- Every tracked member of one guild. Used by maintenance passes that need to
-- diff the whole table before writing.
SELECT * FROM users WHERE guild_id = $1;
