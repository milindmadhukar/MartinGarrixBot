-- join_leave_logs has been guild-blind since it was created: no guild_id, no
-- primary key, no index. The dashboard cannot attribute a join or a leave to a
-- guild without this, and every existing row predates multi-guild support, so
-- the default backfills them onto the main guild the same way 000006 did for
-- modlogs and 000005 did for users and messages.
ALTER TABLE join_leave_logs ADD COLUMN IF NOT EXISTS id BIGSERIAL PRIMARY KEY;
ALTER TABLE join_leave_logs
    ADD COLUMN IF NOT EXISTS guild_id BIGINT NOT NULL DEFAULT 690950056202731521;

CREATE INDEX IF NOT EXISTS idx_jll_guild_time ON join_leave_logs (guild_id, "time" DESC);

-- The messages table has never carried a single index. Every dashboard activity
-- query filters on guild_id and ranges over timestamp, which without these is a
-- sequential scan over every message the bot has ever seen.
CREATE INDEX IF NOT EXISTS idx_messages_guild_time    ON messages (guild_id, "timestamp" DESC);
CREATE INDEX IF NOT EXISTS idx_messages_guild_channel ON messages (guild_id, channel_id);
CREATE INDEX IF NOT EXISTS idx_messages_guild_author  ON messages (guild_id, author_id)
    WHERE author_id IS NOT NULL;

-- idx_modlogs_guild covers the equality but not the ORDER BY time DESC that
-- every modlog listing does.
CREATE INDEX IF NOT EXISTS idx_modlogs_guild_time ON modlogs (guild_id, "time" DESC);
