-- Add moderator_role to guilds (renamed from guild_configurations in migration 000005)
ALTER TABLE guilds ADD COLUMN IF NOT EXISTS moderator_role BIGINT;
