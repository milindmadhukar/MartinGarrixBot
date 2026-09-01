DROP INDEX IF EXISTS idx_songs_anniversary;
DROP TABLE IF EXISTS anniversary_posts;
ALTER TABLE guilds DROP COLUMN IF EXISTS anniversary_timezone;
ALTER TABLE guilds DROP COLUMN IF EXISTS anniversary_hour;
ALTER TABLE guilds DROP COLUMN IF EXISTS anniversary_notifications_role;
ALTER TABLE guilds DROP COLUMN IF EXISTS anniversary_notifications_channel;
