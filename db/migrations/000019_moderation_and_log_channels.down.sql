ALTER TABLE guilds DROP COLUMN IF EXISTS member_logs_channel;
ALTER TABLE guilds DROP COLUMN IF EXISTS voice_logs_channel;

DROP INDEX IF EXISTS idx_modlogs_audit_log_id;
ALTER TABLE modlogs DROP COLUMN IF EXISTS audit_log_id;
