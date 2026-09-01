-- Moderation performed through Discord's own UI (kick, ban, timeout) has never
-- been visible to the bot: modlogs is only ever written by the bot's own
-- /moderation commands, so a server whose staff use the native buttons shows
-- zero moderation actions. The audit-log listener fixes that, and this column
-- is what keeps it idempotent.
--
-- Discord can redeliver a gateway event, so the listener inserts with
-- ON CONFLICT DO NOTHING against this unique key. It doubles as the marker for
-- "this row came from Discord rather than from a bot command", which is what the
-- dashboard's Via column reads.
ALTER TABLE modlogs ADD COLUMN IF NOT EXISTS audit_log_id BIGINT;

-- Partial, so the many rows written by bot commands (all NULL here) do not
-- occupy the index and NULLs never collide with each other.
CREATE UNIQUE INDEX IF NOT EXISTS idx_modlogs_audit_log_id
    ON modlogs (audit_log_id) WHERE audit_log_id IS NOT NULL;

-- Two more log destinations, following the convention every other log channel
-- already uses: NULL means the feature is off, so no separate toggle column is
-- needed. Neither is persisted to a table -- both listeners relay to Discord and
-- store nothing, so there is no volume or retention concern.
ALTER TABLE guilds ADD COLUMN IF NOT EXISTS voice_logs_channel  BIGINT;
ALTER TABLE guilds ADD COLUMN IF NOT EXISTS member_logs_channel BIGINT;
