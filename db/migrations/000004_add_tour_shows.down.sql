-- Remove tour notification configuration from guild_configurations
ALTER TABLE guild_configurations 
    DROP COLUMN IF EXISTS tour_notifications_channel,
    DROP COLUMN IF EXISTS tour_notifications_role;

-- Drop tour_shows table
DROP TABLE IF EXISTS tour_shows;
