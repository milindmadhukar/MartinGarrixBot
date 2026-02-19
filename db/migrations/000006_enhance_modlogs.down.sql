-- Drop indexes
DROP INDEX IF EXISTS idx_modlogs_expires;
DROP INDEX IF EXISTS idx_modlogs_guild;
DROP INDEX IF EXISTS idx_modlogs_user_guild;

-- Drop columns
ALTER TABLE modlogs DROP COLUMN IF EXISTS active;
ALTER TABLE modlogs DROP COLUMN IF EXISTS expires_at;
ALTER TABLE modlogs DROP COLUMN IF EXISTS guild_id;
