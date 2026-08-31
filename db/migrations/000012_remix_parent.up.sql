-- Beatport lists every remix as its own track, so one song becomes many rows:
-- "Catharina" is six, "Told You So" is ten. Each row is a real distinct recording
-- with its own beatport id, BPM and length, so they are kept -- but they must stop
-- appearing as separate songs in autocomplete, the quiz and the radio rotation.
--
-- parent_song_id points a remix at the canonical row for the same song. Canonical
-- rows keep it NULL, so "the songs a user should see" is parent_song_id IS NULL.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS parent_song_id BIGINT
    REFERENCES songs(id) ON DELETE SET NULL;

-- Instrumentals can never have lyrics. Without a way to say so they sit in the
-- "missing lyrics" backlog forever, and the quiz can pick one and ask a player to
-- recall words that do not exist.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS is_instrumental BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_songs_parent ON songs (parent_song_id) WHERE parent_song_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_songs_canonical ON songs (id) WHERE parent_song_id IS NULL;
