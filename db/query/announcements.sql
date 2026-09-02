-- name: InsertSongAnnouncement :exec
-- ON CONFLICT DO NOTHING because the send path is best-effort bookkeeping: a message
-- id we already hold is not a problem worth failing an announcement over.
INSERT INTO song_announcements (song_id, guild_id, channel_id, message_id, buttons_key)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (guild_id, message_id) DO NOTHING;

-- name: GetAnnouncementsToRefresh :many
-- Recent announcements whose buttons may have moved on.
--
-- Four bounds, each doing a different job. failed_at parks a message the bot cannot
-- currently edit. edit_count stops correcting a message that has been corrected five
-- times, which means something upstream is flapping rather than settling. edited_at
-- caps any one message at one edit an hour. posted_at is what keeps the candidate set
-- from growing by every song the bot ever announces: a release's links stop arriving
-- within weeks.
--
-- The buttons themselves are compared in Go, not here -- the fingerprint is a hash of
-- what utils.GetSongButtons would render, and SQL cannot compute it.
SELECT a.song_id, a.guild_id, a.channel_id, a.message_id, a.buttons_key, a.edit_count,
       sqlc.embed(s)
FROM song_announcements a
JOIN songs s ON s.id = a.song_id
WHERE a.failed_at IS NULL
  AND a.edit_count < 5
  AND a.posted_at > NOW() - INTERVAL '60 days'
  AND (a.edited_at IS NULL OR a.edited_at < NOW() - INTERVAL '1 hour')
ORDER BY a.posted_at DESC
LIMIT sqlc.arg(lim);

-- name: MarkAnnouncementEdited :exec
-- Writes the new fingerprint, so the same change can never be detected twice.
UPDATE song_announcements
SET buttons_key = sqlc.arg(buttons_key), edited_at = NOW(), edit_count = edit_count + 1
WHERE guild_id = sqlc.arg(guild_id) AND message_id = sqlc.arg(message_id);

-- name: MarkAnnouncementFailed :exec
-- Discord refused the edit for a reason that may resolve itself -- usually the bot
-- losing access to the channel. Parked, not deleted: permissions come back, and this
-- row is the only record of where the message is.
UPDATE song_announcements SET failed_at = NOW()
WHERE guild_id = $1 AND message_id = $2;

-- name: DeleteAnnouncement :exec
-- The message or its channel is gone. There is nothing left to correct, and keeping
-- the row would retry forever.
DELETE FROM song_announcements WHERE guild_id = $1 AND message_id = $2;

-- name: ClearAnnouncementFailures :exec
-- Called after a successful post to a channel: whatever the permission problem was,
-- it is over, so the parked messages in that channel become editable again.
UPDATE song_announcements SET failed_at = NULL
WHERE channel_id = $1 AND failed_at IS NOT NULL;
