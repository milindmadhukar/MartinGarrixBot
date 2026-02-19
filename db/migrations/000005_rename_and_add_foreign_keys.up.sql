-- Rename guild_configurations to guilds
ALTER TABLE guild_configurations RENAME TO guilds;

-- Add foreign key constraint from messages.author_id to users.id
-- Note: We need to handle cases where a message author may not exist in users table
-- The foreign key is set up with ON DELETE SET NULL to preserve messages even when users are deleted
-- This means messages will have NULL author_id if the user is removed from the database

-- Add author_guild_id column to support composite foreign key
ALTER TABLE messages ADD COLUMN IF NOT EXISTS author_guild_id BIGINT;

-- Update author_guild_id to match guild_id for existing rows
UPDATE messages SET author_guild_id = guild_id WHERE author_guild_id IS NULL;

-- Make author_guild_id NOT NULL after populating
ALTER TABLE messages ALTER COLUMN author_guild_id SET NOT NULL;

-- Make author_id nullable first (required for ON DELETE SET NULL)
ALTER TABLE messages ALTER COLUMN author_id DROP NOT NULL;

-- Clean up orphaned messages: set author_id to NULL for messages where the author doesn't exist in users table
-- This ensures the foreign key constraint can be added without violating referential integrity
UPDATE messages 
SET author_id = NULL 
WHERE NOT EXISTS (
    SELECT 1 
    FROM users 
    WHERE users.id = messages.author_id 
    AND users.guild_id = messages.author_guild_id
);

-- Add foreign key constraint
-- ON DELETE SET NULL: If a user is deleted from users table, set author_id to NULL instead of deleting the message
-- This preserves message history even when user data is removed
ALTER TABLE messages 
    ADD CONSTRAINT fk_messages_author 
    FOREIGN KEY (author_id, author_guild_id) 
    REFERENCES users(id, guild_id) 
    ON DELETE SET NULL;
