DROP INDEX IF EXISTS idx_modlogs_guild_time;
DROP INDEX IF EXISTS idx_messages_guild_author;
DROP INDEX IF EXISTS idx_messages_guild_channel;
DROP INDEX IF EXISTS idx_messages_guild_time;
DROP INDEX IF EXISTS idx_jll_guild_time;

ALTER TABLE join_leave_logs DROP COLUMN IF EXISTS guild_id;
ALTER TABLE join_leave_logs DROP COLUMN IF EXISTS id;
