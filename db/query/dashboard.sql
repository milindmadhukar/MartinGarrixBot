-- Queries backing the web dashboard. They are kept in their own file because
-- none of the existing ones fit: the leaderboards in users.sql hardcode
-- LIMIT 10, and GetModlogsByGuild takes no filters.
--
-- Two conventions here that every query in this file follows, both forced by
-- the fact that modlogs.time, join_leave_logs.time and messages.timestamp are
-- `timestamp without time zone` columns holding UTC instants:
--
--   1. Day and hour buckets are computed as
--        ("time" AT TIME ZONE 'UTC') AT TIME ZONE sqlc.arg('tz')
--      The first conversion reinterprets the naive value as the UTC instant it
--      actually is; the second renders it as wall-clock in the viewer's zone.
--      A plain date_trunc('day', "time") cuts days at UTC midnight, which for
--      the bot's own Asia/Kolkata is 05:30 -- so "yesterday" on the chart would
--      not be yesterday.
--
--   2. Comparisons against the current time are written
--        (now() AT TIME ZONE 'UTC')
--      rather than bare now(). now() is timestamptz, and comparing it to a naive
--      column makes Postgres convert using the session TimeZone GUC, so the
--      result would silently depend on the database container's timezone.

-- name: DashModlogs :many
SELECT * FROM modlogs
WHERE guild_id = $1
  AND (sqlc.narg('log_type')::varchar IS NULL OR log_type = sqlc.narg('log_type'))
  AND (sqlc.narg('user_id')::bigint IS NULL OR user_id = sqlc.narg('user_id'))
  AND (sqlc.narg('moderator_id')::bigint IS NULL OR moderator_id = sqlc.narg('moderator_id'))
  AND (sqlc.narg('active')::boolean IS NULL OR active = sqlc.narg('active'))
  AND (sqlc.narg('after')::timestamp IS NULL OR "time" >= sqlc.narg('after'))
  AND (sqlc.narg('before')::timestamp IS NULL OR "time" < sqlc.narg('before'))
-- id DESC is not decoration: bulk moderation lands many rows on the same
-- timestamp, and without a deterministic tiebreak those rows shuffle between
-- pages, so paging forwards shows some of them twice and skips others.
ORDER BY "time" DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: DashModlogsCount :one
SELECT COUNT(*) FROM modlogs
WHERE guild_id = $1
  AND (sqlc.narg('log_type')::varchar IS NULL OR log_type = sqlc.narg('log_type'))
  AND (sqlc.narg('user_id')::bigint IS NULL OR user_id = sqlc.narg('user_id'))
  AND (sqlc.narg('moderator_id')::bigint IS NULL OR moderator_id = sqlc.narg('moderator_id'))
  AND (sqlc.narg('active')::boolean IS NULL OR active = sqlc.narg('active'))
  AND (sqlc.narg('after')::timestamp IS NULL OR "time" >= sqlc.narg('after'))
  AND (sqlc.narg('before')::timestamp IS NULL OR "time" < sqlc.narg('before'));

-- name: DashModlogTypes :many
-- Populates the filter dropdown from what this guild has actually recorded, so
-- a log_type added in the bot needs no dashboard change to become filterable.
SELECT log_type, COUNT(*)::bigint AS count
FROM modlogs
WHERE guild_id = $1
GROUP BY log_type
ORDER BY count DESC;

-- name: DashMemberLogs :many
SELECT * FROM join_leave_logs
WHERE guild_id = $1
  AND (sqlc.narg('action')::varchar IS NULL OR action = sqlc.narg('action'))
  AND (sqlc.narg('member_id')::bigint IS NULL OR member_id = sqlc.narg('member_id'))
  AND (sqlc.narg('after')::timestamp IS NULL OR "time" >= sqlc.narg('after'))
  AND (sqlc.narg('before')::timestamp IS NULL OR "time" < sqlc.narg('before'))
ORDER BY "time" DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: DashMemberLogsCount :one
SELECT COUNT(*) FROM join_leave_logs
WHERE guild_id = $1
  AND (sqlc.narg('action')::varchar IS NULL OR action = sqlc.narg('action'))
  AND (sqlc.narg('member_id')::bigint IS NULL OR member_id = sqlc.narg('member_id'))
  AND (sqlc.narg('after')::timestamp IS NULL OR "time" >= sqlc.narg('after'))
  AND (sqlc.narg('before')::timestamp IS NULL OR "time" < sqlc.narg('before'));

-- Overview -----------------------------------------------------------------

-- name: DashGuildOverview :one
SELECT
    (SELECT COUNT(*) FROM users u WHERE u.guild_id = $1)::bigint AS tracked_users,
    (SELECT COALESCE(SUM(u.messages_sent), 0) FROM users u WHERE u.guild_id = $1)::bigint AS messages_counted,
    (SELECT COALESCE(SUM(u.total_xp), 0) FROM users u WHERE u.guild_id = $1)::bigint AS total_xp,
    (SELECT COALESCE(SUM(COALESCE(u.stmpd_coins, 0) + COALESCE(u.in_hand, 0)), 0)
       FROM users u WHERE u.guild_id = $1)::bigint AS coins_in_circulation,
    (SELECT COUNT(*) FROM messages m
       WHERE m.guild_id = $1
         AND m."timestamp" >= (now() AT TIME ZONE 'UTC') - make_interval(days => sqlc.arg('window_days')::int)
    )::bigint AS messages_window,
    (SELECT COUNT(*) FROM modlogs ml
       WHERE ml.guild_id = $1
         AND ml."time" >= (now() AT TIME ZONE 'UTC') - make_interval(days => sqlc.arg('window_days')::int)
    )::bigint AS modlogs_window,
    (SELECT COUNT(*) FROM modlogs ml
       WHERE ml.guild_id = $1 AND ml.active = true
         AND ml.expires_at IS NOT NULL
         AND ml.expires_at > (now() AT TIME ZONE 'UTC')
    )::bigint AS active_punishments;

-- Member growth ------------------------------------------------------------

-- name: DashJoinLeaveDaily :many
-- The generate_series join is the point of this query: without it a day with no
-- joins and no leaves is a MISSING ROW, and a line chart happily draws a
-- straight line across the gap -- turning "nothing happened" into what looks
-- like real interpolated data.
WITH days AS (
    SELECT generate_series(
        date_trunc('day', ((now() AT TIME ZONE 'UTC') AT TIME ZONE sqlc.arg('tz')::text)
            - make_interval(days => sqlc.arg('window_days')::int - 1)),
        date_trunc('day', ((now() AT TIME ZONE 'UTC') AT TIME ZONE sqlc.arg('tz')::text)),
        INTERVAL '1 day'
    ) AS day
),
events AS (
    SELECT
        date_trunc('day', ("time" AT TIME ZONE 'UTC') AT TIME ZONE sqlc.arg('tz')::text) AS day,
        COUNT(*) FILTER (WHERE action = 'join')  AS joins,
        COUNT(*) FILTER (WHERE action = 'leave') AS leaves
    FROM join_leave_logs
    WHERE guild_id = $1
      AND "time" >= (now() AT TIME ZONE 'UTC') - make_interval(days => sqlc.arg('window_days')::int)
    GROUP BY 1
)
SELECT
    d.day::timestamp AS day,
    COALESCE(e.joins, 0)::bigint  AS joins,
    COALESCE(e.leaves, 0)::bigint AS leaves,
    (COALESCE(e.joins, 0) - COALESCE(e.leaves, 0))::bigint AS net
FROM days d
LEFT JOIN events e ON e.day = d.day
ORDER BY d.day;

-- name: DashJoinLeaveTotals :one
SELECT
    COUNT(*) FILTER (WHERE action = 'join')::bigint  AS joins,
    COUNT(*) FILTER (WHERE action = 'leave')::bigint AS leaves
FROM join_leave_logs
WHERE guild_id = $1
  AND "time" >= (now() AT TIME ZONE 'UTC') - make_interval(days => sqlc.arg('window_days')::int);

-- Message activity ---------------------------------------------------------

-- name: DashMessagesDaily :many
WITH days AS (
    SELECT generate_series(
        date_trunc('day', ((now() AT TIME ZONE 'UTC') AT TIME ZONE sqlc.arg('tz')::text)
            - make_interval(days => sqlc.arg('window_days')::int - 1)),
        date_trunc('day', ((now() AT TIME ZONE 'UTC') AT TIME ZONE sqlc.arg('tz')::text)),
        INTERVAL '1 day'
    ) AS day
),
counts AS (
    SELECT
        date_trunc('day', ("timestamp" AT TIME ZONE 'UTC') AT TIME ZONE sqlc.arg('tz')::text) AS day,
        COUNT(*) AS messages,
        COUNT(DISTINCT author_id) AS active_authors
    FROM messages
    WHERE guild_id = $1
      AND "timestamp" >= (now() AT TIME ZONE 'UTC') - make_interval(days => sqlc.arg('window_days')::int)
    GROUP BY 1
)
SELECT
    d.day::timestamp AS day,
    COALESCE(c.messages, 0)::bigint       AS messages,
    COALESCE(c.active_authors, 0)::bigint AS active_authors
FROM days d
LEFT JOIN counts c ON c.day = d.day
ORDER BY d.day;

-- name: DashActivityHeatmap :many
-- Hour-of-day x day-of-week, rendered as a CSS grid rather than a chart
-- library. dow is 0=Sunday, matching Postgres' EXTRACT(DOW).
SELECT
    EXTRACT(DOW  FROM ("timestamp" AT TIME ZONE 'UTC') AT TIME ZONE sqlc.arg('tz')::text)::int AS dow,
    EXTRACT(HOUR FROM ("timestamp" AT TIME ZONE 'UTC') AT TIME ZONE sqlc.arg('tz')::text)::int AS hour,
    COUNT(*)::bigint AS messages
FROM messages
WHERE guild_id = $1
  AND "timestamp" >= (now() AT TIME ZONE 'UTC') - make_interval(days => sqlc.arg('window_days')::int)
GROUP BY 1, 2
ORDER BY 1, 2;

-- name: DashTopChannels :many
SELECT channel_id, COUNT(*)::bigint AS messages
FROM messages
WHERE guild_id = $1
  AND "timestamp" >= (now() AT TIME ZONE 'UTC') - make_interval(days => sqlc.arg('window_days')::int)
GROUP BY channel_id
ORDER BY messages DESC, channel_id
LIMIT $2;

-- name: DashTopPosters :many
SELECT author_id, COUNT(*)::bigint AS messages
FROM messages
WHERE guild_id = $1
  AND "timestamp" >= (now() AT TIME ZONE 'UTC') - make_interval(days => sqlc.arg('window_days')::int)
  AND author_id IS NOT NULL
GROUP BY author_id
ORDER BY messages DESC, author_id
LIMIT $2;

-- Economy and levels -------------------------------------------------------

-- name: DashTopMembers :many
-- One query, three orderings. The existing leaderboards in users.sql back live
-- slash commands and hardcode LIMIT 10, so they are left alone.
-- The trailing `id` is a deterministic tiebreak; without it members tied on 0
-- shuffle position between renders.
SELECT
    id,
    messages_sent,
    total_xp,
    stmpd_coins,
    in_hand,
    (COALESCE(stmpd_coins, 0) + COALESCE(in_hand, 0))::bigint AS net_worth
FROM users
WHERE guild_id = $1
ORDER BY
    CASE WHEN sqlc.arg('sort')::text = 'messages' THEN messages_sent END DESC NULLS LAST,
    CASE WHEN sqlc.arg('sort')::text = 'xp'       THEN total_xp      END DESC NULLS LAST,
    CASE WHEN sqlc.arg('sort')::text = 'coins'
         THEN COALESCE(stmpd_coins, 0) + COALESCE(in_hand, 0)       END DESC NULLS LAST,
    id
LIMIT $2;

-- name: DashXPDistribution :many
SELECT
    width_bucket(
        u.total_xp, 0,
        GREATEST((SELECT MAX(mx.total_xp) FROM users mx WHERE mx.guild_id = $1), 1),
        20
    ) AS bucket,
    MIN(u.total_xp)::int AS min_xp,
    MAX(u.total_xp)::int AS max_xp,
    COUNT(*)::bigint     AS members
FROM users u
WHERE u.guild_id = $1 AND u.total_xp > 0
GROUP BY bucket
ORDER BY bucket;

-- Moderation ---------------------------------------------------------------

-- name: DashModlogsByType :many
SELECT log_type, COUNT(*)::bigint AS count
FROM modlogs
WHERE guild_id = $1
  AND "time" >= (now() AT TIME ZONE 'UTC') - make_interval(days => sqlc.arg('window_days')::int)
GROUP BY log_type
ORDER BY count DESC;

-- name: DashModlogsDaily :many
SELECT
    date_trunc('day', ("time" AT TIME ZONE 'UTC') AT TIME ZONE sqlc.arg('tz')::text)::timestamp AS day,
    log_type,
    COUNT(*)::bigint AS count
FROM modlogs
WHERE guild_id = $1
  AND "time" >= (now() AT TIME ZONE 'UTC') - make_interval(days => sqlc.arg('window_days')::int)
GROUP BY 1, 2
ORDER BY 1, 2;

-- name: DashTopModerators :many
SELECT
    moderator_id,
    COUNT(*)::bigint AS actions,
    COUNT(*) FILTER (WHERE log_type IN ('ban', 'tempban', 'softban'))::bigint AS bans,
    COUNT(*) FILTER (WHERE log_type IN ('mute', 'tempmute'))::bigint          AS mutes,
    COUNT(*) FILTER (WHERE log_type = 'kick')::bigint                         AS kicks,
    MAX("time")::timestamp AS last_action
FROM modlogs
WHERE guild_id = $1
  AND "time" >= (now() AT TIME ZONE 'UTC') - make_interval(days => sqlc.arg('window_days')::int)
GROUP BY moderator_id
ORDER BY actions DESC, moderator_id
LIMIT $2;

-- name: DashActivePunishments :many
SELECT * FROM modlogs
WHERE guild_id = $1
  AND active = true
  AND expires_at IS NOT NULL
  AND expires_at > (now() AT TIME ZONE 'UTC')
ORDER BY expires_at ASC, id
LIMIT $2;
