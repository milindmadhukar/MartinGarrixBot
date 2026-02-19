-- Remove foreign key constraint
ALTER TABLE messages DROP CONSTRAINT IF EXISTS fk_messages_author;

-- Make author_id NOT NULL again
ALTER TABLE messages ALTER COLUMN author_id SET NOT NULL;

-- Drop author_guild_id column
ALTER TABLE messages DROP COLUMN IF EXISTS author_guild_id;

-- Rename guilds back to guild_configurations
ALTER TABLE guilds RENAME TO guild_configurations;
