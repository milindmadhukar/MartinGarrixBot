-- name: MessageSent :one
-- Logs the message and updates the author's counters in one statement.
--
-- Three things here are deliberate:
--
--   The XP write is an INCREMENT, not `total_xp = $6`. The old absolute write
--   took a value read by an earlier GetUser, so two messages processed close
--   together could both read X and the second could write back a stale X over
--   the first one's award. Adding to the stored value cannot lose a write; under
--   a concurrent update Postgres re-evaluates the row against the new value. A
--   caller with nothing to award passes roll = 0, which makes it a no-op.
--
--   The multiplier is applied here rather than in Go. guilds.xp_multiplier has
--   been settable from the dashboard since it was added and has never once been
--   read; reading it in the listener would have meant a third database round
--   trip per message on the gateway's single event goroutine. As a CTE it costs
--   nothing. GREATEST(..., 1) keeps the smallest allowed multiplier (0.1x) from
--   rounding an award down to zero, which would stall a member forever.
--
--   messages_sent only moves when the message row actually inserted. The two
--   CTEs are independent, so the old unconditional `+ 1` also counted redelivered
--   gateway events that ON CONFLICT threw away.
--
-- prev snapshots total_xp from the statement snapshot, before the update lands,
-- so the caller gets both sides of the write and can tell whether this message
-- crossed a level boundary without a second query. The COALESCEs cover the row
-- vanishing between the caller's GetUser and this statement: both sides then
-- read 0, which reports "no level change" rather than erroring on a NULL.
WITH cfg AS (
    SELECT COALESCE(g.xp_multiplier, 1.0) AS mult
    FROM guilds g
    WHERE g.guild_id = sqlc.arg(guild_id)
),
prev AS (
    SELECT p.total_xp AS old_xp
    FROM users p
    WHERE p.id = sqlc.arg(author_id) AND p.guild_id = sqlc.arg(guild_id)
),
message_insert AS (
    INSERT INTO messages (message_id, guild_id, channel_id, author_id, author_guild_id, content)
    VALUES (sqlc.arg(message_id), sqlc.arg(guild_id), sqlc.arg(channel_id), sqlc.arg(author_id), sqlc.arg(guild_id), sqlc.arg(content))
    ON CONFLICT DO NOTHING
    RETURNING message_id
),
user_updates AS (
    UPDATE users u
    SET
        total_xp = u.total_xp + CASE
            WHEN sqlc.arg(roll)::int > 0 THEN GREATEST(
                ROUND(sqlc.arg(roll)::numeric
                      * COALESCE((SELECT mult FROM cfg), 1.0))::int, 1)
            ELSE 0
        END,
        last_xp_added = CASE
            WHEN sqlc.arg(roll)::int > 0 THEN sqlc.arg(awarded_at)::timestamp
            ELSE u.last_xp_added
        END,
        messages_sent = u.messages_sent + CASE
            WHEN EXISTS (SELECT 1 FROM message_insert) THEN 1
            ELSE 0
        END
    WHERE u.id = sqlc.arg(author_id) AND u.guild_id = sqlc.arg(guild_id)
    RETURNING u.total_xp AS new_xp
)
SELECT
    COALESCE((SELECT old_xp FROM prev), 0)::int         AS old_xp,
    COALESCE((SELECT new_xp FROM user_updates), 0)::int AS new_xp;
