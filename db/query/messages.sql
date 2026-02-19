-- name: MessageSent :exec
WITH message_insert AS (
    INSERT INTO messages (message_id, guild_id, channel_id, author_id, author_guild_id, content)
    VALUES ($1, $2, $3, $4, $2, $5)
    ON CONFLICT DO NOTHING
    RETURNING author_id
),
user_updates AS (
    UPDATE users 
    SET 
        total_xp = $6,
        last_xp_added = $7,
        messages_sent = messages_sent + 1
    WHERE id = $4 AND guild_id = $2
    RETURNING id
)
SELECT 1;
-- $1 = message_id
-- $2 = guild_id
-- $3 = channel_id
-- $4 = author_id
-- $5 = content
-- $6 = total_xp
-- $7 = last_xp_added