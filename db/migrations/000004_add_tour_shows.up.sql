-- Create tour_shows table to track Martin Garrix tour dates
CREATE TABLE IF NOT EXISTS tour_shows (
    id BIGSERIAL PRIMARY KEY,
    show_name VARCHAR(300) NOT NULL,
    city VARCHAR(200) NOT NULL,
    country VARCHAR(200) NOT NULL,
    venue VARCHAR(300) NOT NULL,
    show_date DATE NOT NULL,
    ticket_url TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT unique_tour_show UNIQUE (show_name, show_date, venue)
);

-- Add tour notification configuration to guild_configurations
ALTER TABLE guild_configurations 
    ADD COLUMN IF NOT EXISTS tour_notifications_channel BIGINT,
    ADD COLUMN IF NOT EXISTS tour_notifications_role BIGINT;
