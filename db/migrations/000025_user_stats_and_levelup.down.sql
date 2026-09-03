ALTER TABLE guilds
    DROP COLUMN IF EXISTS level_up_role_level,
    DROP COLUMN IF EXISTS level_up_role;

-- Only the constraints come off. The backfilled zeroes stay: there is no way to
-- tell a row that was always 0 from one this migration filled in, and restoring
-- NULLs would put the leaderboard ordering bug straight back.
ALTER TABLE users
    ALTER COLUMN total_xp      DROP NOT NULL,
    ALTER COLUMN messages_sent DROP NOT NULL,
    ALTER COLUMN stmpd_coins   DROP NOT NULL,
    ALTER COLUMN in_hand       DROP NOT NULL;
