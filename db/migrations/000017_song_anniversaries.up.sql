-- Per-guild anniversary configuration. Channel and role follow the existing
-- notification columns exactly. Hour and timezone are new: unlike every other feed,
-- this one is not "post when the source changes" but "post in this server's
-- morning", so the schedule itself has to live per guild.
ALTER TABLE guilds ADD COLUMN IF NOT EXISTS anniversary_notifications_channel BIGINT;
ALTER TABLE guilds ADD COLUMN IF NOT EXISTS anniversary_notifications_role BIGINT;

-- Local hour of day to post at. Defaults to 9: late enough not to be the middle of
-- the night, early enough to still read as "this morning". The CHECK rides on the
-- ADD COLUMN so the whole statement is skipped when the column already exists.
ALTER TABLE guilds ADD COLUMN IF NOT EXISTS anniversary_hour INTEGER NOT NULL DEFAULT 9
    CHECK (anniversary_hour BETWEEN 0 AND 23);

-- IANA name. Validated with time.LoadLocation before it is ever written here, so a
-- row that reaches the scheduler always names a zone Go can resolve.
ALTER TABLE guilds ADD COLUMN IF NOT EXISTS anniversary_timezone TEXT NOT NULL DEFAULT 'UTC';

-- The replay lock. One row per (guild, local date), not per song: the whole day is
-- announced in a single pass, so the day is the unit that either happened or did
-- not. The key is the guild's LOCAL date, because that is what "today" means for
-- that guild -- for a server at UTC+13 the local day and the UTC day disagree for
-- half of every day, and keying on UTC would post on the wrong one.
CREATE TABLE IF NOT EXISTS anniversary_posts (
    guild_id   BIGINT      NOT NULL REFERENCES guilds(guild_id) ON DELETE CASCADE,
    local_date DATE        NOT NULL,
    song_count INTEGER     NOT NULL,
    posted_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (guild_id, local_date)
);

-- release_date is TEXT holding 'YYYY-MM-DD' (migration 000008), so right(x, 5) is
-- the 'MM-DD' anniversary key. right() is immutable and therefore indexable;
-- to_date() is only stable and cannot be. The partial predicate matches the
-- eligibility filter the daily query uses, turning a 1000-row scan into a handful.
CREATE INDEX IF NOT EXISTS idx_songs_anniversary
    ON songs (right(release_date, 5))
    WHERE parent_song_id IS NULL AND NOT is_collection AND release_date IS NOT NULL;
