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
