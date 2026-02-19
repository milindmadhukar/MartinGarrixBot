-- Add guild_id to modlogs for multi-guild support
ALTER TABLE modlogs ADD COLUMN guild_id BIGINT NOT NULL DEFAULT 690950056202731521;

-- Add expires_at for tempban/tempmute tracking
ALTER TABLE modlogs ADD COLUMN expires_at TIMESTAMP;

-- Add active field to track if temporary actions are still in effect
ALTER TABLE modlogs ADD COLUMN active BOOLEAN DEFAULT true;

-- Add index for faster lookups
CREATE INDEX idx_modlogs_user_guild ON modlogs(user_id, guild_id);
CREATE INDEX idx_modlogs_guild ON modlogs(guild_id);
CREATE INDEX idx_modlogs_expires ON modlogs(expires_at) WHERE expires_at IS NOT NULL;
