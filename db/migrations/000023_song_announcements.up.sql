-- Where each song's release announcement landed, so its link buttons can be
-- corrected in place.
--
-- A release is announced within days of coming out, but its links arrive over the
-- following weeks: the Spotify and Apple links from the STMPD sync, the Beatport slug
-- from its own backfill, the YouTube video from resolve-youtube-links. The
-- announcement is the most looked-at message the bot posts, and it has always frozen
-- with whatever links existed in the first fifteen minutes -- because the notifier
-- logged the message id at debug level and threw it away.
--
-- Keyed on (guild_id, message_id) rather than (song_id, guild_id): the message id is
-- what has to be unique, and a song genuinely can be announced twice -- a row
-- promoted from unreleased clears announced_at by design, precisely so it announces
-- again.
CREATE TABLE IF NOT EXISTS song_announcements (
    song_id    BIGINT NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
    guild_id   BIGINT NOT NULL,
    channel_id BIGINT NOT NULL,
    message_id BIGINT NOT NULL,

    -- Fingerprint of the button set as posted. The refresh compares it against the
    -- row's buttons now and edits only on a mismatch, so a cycle that finds nothing
    -- changed makes zero REST calls -- which is the steady state.
    --
    -- Hashing the BUTTONS and not the row is the load-bearing choice. The artwork and
    -- release-date enrichment runs hourly and touches many of the same rows without
    -- changing a single button; a row-level fingerprint would have it triggering an
    -- edit on every announcement it touched, every hour.
    buttons_key TEXT NOT NULL,

    posted_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    edited_at  TIMESTAMPTZ,
    edit_count SMALLINT NOT NULL DEFAULT 0,

    -- Set when Discord refused the edit for a reason that may resolve itself, such as
    -- the bot losing access to the channel. Deliberately not a delete: access comes
    -- back, and this row is the only record of where the message is.
    failed_at  TIMESTAMPTZ,

    PRIMARY KEY (guild_id, message_id)
);

CREATE INDEX IF NOT EXISTS idx_song_announcements_song ON song_announcements (song_id);
