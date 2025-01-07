-- name: MessageSent :exec
WITH message_insert AS (
    INSERT INTO messages (message_id, channel_id, author_id, content)
    VALUES ($1, $2, $3, $4)
    ON CONFLICT DO NOTHING
    RETURNING author_id
),
user_updates AS (
    UPDATE users 
    SET 
        total_xp = $5,
        last_xp_added = $6,
        messages_sent = messages_sent + 1
    WHERE id = $3
    RETURNING id
)
SELECT 1;
-- $1 = message_id
-- $2 = channel_id
-- $3 = author_id
-- $4 = content
-- $5 = total_xp
-- $6 = last_xp_added-- name: MessageSent :exec