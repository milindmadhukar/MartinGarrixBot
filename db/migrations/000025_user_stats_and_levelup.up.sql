-- Two related repairs: make the per-member counters honest, and give level-ups
-- somewhere to be configured.
--
-- 1. The counters.
--
-- Migration 000001 shipped users.total_xp and friends as nullable with a
-- DEFAULT 0 and left a TODO to tighten them. Nothing ever did, and 184 of 2157
-- rows drifted to NULL. That is not a cosmetic gap: `ORDER BY total_xp DESC`
-- sorts NULLs FIRST in Postgres, so the levels leaderboard returned ten members
-- with no XP at all and RANK() pushed the real top member down to rank 185.
-- Backfilling and constraining the columns fixes both at the source, and stops
-- the message listener from silently restarting a member's XP from zero when it
-- reads a NULL as 0.
--
-- last_xp_added stays nullable on purpose: NULL there means "has never earned
-- XP", which the cooldown check reads as "award on the next message".
UPDATE users SET total_xp      = 0 WHERE total_xp      IS NULL;
UPDATE users SET messages_sent = 0 WHERE messages_sent IS NULL;
UPDATE users SET stmpd_coins   = 0 WHERE stmpd_coins   IS NULL;
UPDATE users SET in_hand       = 0 WHERE in_hand       IS NULL;

ALTER TABLE users
    ALTER COLUMN total_xp      SET DEFAULT 0,
    ALTER COLUMN total_xp      SET NOT NULL,
    ALTER COLUMN messages_sent SET DEFAULT 0,
    ALTER COLUMN messages_sent SET NOT NULL,
    ALTER COLUMN stmpd_coins   SET DEFAULT 0,
    ALTER COLUMN stmpd_coins   SET NOT NULL,
    ALTER COLUMN in_hand       SET DEFAULT 0,
    ALTER COLUMN in_hand       SET NOT NULL;

-- 2. Level-up configuration.
--
-- The bot has never announced a level-up or granted a role for one, though both
-- existed in the Python bot and FEATURES.md still claims them. Python hardcoded
-- the role ID and threshold in an enum; here they are per-guild config so the
-- feature is off until someone sets a role, rather than off until someone edits
-- the source.
--
-- The threshold follows anniversary_hour's shape: NOT NULL with a default and a
-- CHECK, so a nonsense value cannot be stored. 13 matches the level named in the
-- listener's long-standing TODO.
ALTER TABLE guilds
    ADD COLUMN level_up_role       BIGINT,
    ADD COLUMN level_up_role_level INTEGER NOT NULL DEFAULT 13
        CONSTRAINT guilds_level_up_role_level_check CHECK (level_up_role_level >= 0);
