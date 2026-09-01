-- name: GetAnniversaryGuilds :many
-- Every guild with the feature switched on. The scheduler needs the hour and the
-- timezone alongside the channel, so this cannot share the shape of the other
-- Get*NotificationChannels queries, which only ever return a channel and a role.
SELECT guild_id,
       anniversary_notifications_channel,
       anniversary_notifications_role,
       anniversary_hour,
       anniversary_timezone
FROM guilds
WHERE anniversary_notifications_channel IS NOT NULL;

-- name: GetSongAnniversaries :many
-- Canonical, non-collection songs released on this month/day in an earlier year.
--
-- month_day is 'MM-DD' and is matched with right(), which is what idx_songs_anniversary
-- indexes -- to_date() is only stable, so it cannot carry an index. The date
-- comparison is what makes "earlier year" true: without it a song released this
-- morning would be announced as its own zeroth anniversary.
SELECT sqlc.embed(songs),
       (EXTRACT(YEAR FROM sqlc.arg(today)::date)
        - EXTRACT(YEAR FROM to_date(release_date, 'YYYY-MM-DD')))::int AS years_old
FROM songs
WHERE parent_song_id IS NULL
  AND NOT is_collection
  AND release_date IS NOT NULL
  AND right(release_date, 5) = ANY(sqlc.arg(month_days)::text[])
  AND to_date(release_date, 'YYYY-MM-DD') < sqlc.arg(today)::date
ORDER BY release_date;

-- name: HasPostedAnniversaries :one
SELECT EXISTS (
    SELECT 1 FROM anniversary_posts WHERE guild_id = $1 AND local_date = $2
);

-- name: ClaimAnniversaryDay :execrows
-- Claims a guild's post for one local day, atomically. Returns 1 when this caller
-- won the claim and should send, 0 when the day was already taken.
--
-- The insert is the claim, so two ticks racing each other -- or two bot instances --
-- cannot both post. HasPostedAnniversaries above is only a cheap pre-filter to keep
-- the songs query from running every five minutes until local midnight; this is what
-- actually decides.
--
-- Claimed before the messages go out, not after. A crash mid-send costs one day's
-- post; a crash before the claim would repost the whole day on the next tick, and of
-- the two, quiet is the safer failure -- the same reasoning as MarkSongAnnounced.
INSERT INTO anniversary_posts (guild_id, local_date, song_count)
VALUES ($1, $2, $3)
ON CONFLICT (guild_id, local_date) DO NOTHING;

-- name: SetAnniversaryChannel :exec
UPDATE guilds SET anniversary_notifications_channel = $2 WHERE guild_id = $1;

-- name: SetAnniversaryRole :exec
UPDATE guilds SET anniversary_notifications_role = $2 WHERE guild_id = $1;

-- name: SetAnniversarySchedule :exec
UPDATE guilds SET anniversary_hour = $2, anniversary_timezone = $3 WHERE guild_id = $1;
