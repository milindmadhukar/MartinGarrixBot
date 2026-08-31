-- Some rows are not songs. "Dawn EP", "Half Human [ALBUM]", "Catharina (Remixes)"
-- and "STMPD RCRDS Mixtape 2019 Side A" are releases that contain songs, and the
-- catalogue lists them alongside the tracks themselves.
--
-- They should not be offered as songs to pick, quizzed on, or -- most visibly -- put
-- into the radio rotation, where the player would try to stream a whole EP as if it
-- were one track.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS is_collection BOOLEAN NOT NULL DEFAULT FALSE;

-- Word-boundary matches only: "EP" must not fire on "Deep", and "LP" must not fire
-- on "Help". The bracketed and parenthesised forms are how the catalogue writes them.
UPDATE songs SET is_collection = TRUE
WHERE name ~* '(^|[^[:alnum:]])(ep|album|lp)([^[:alnum:]]|$)'
   OR name ~* '(^|[^[:alnum:]])remixes([^[:alnum:]]|$)'
   OR name ~* 'mixtape'
   OR name ~* 'festival edits'
   OR name ~* '\[album\]';

CREATE INDEX IF NOT EXISTS idx_songs_playable
    ON songs (id) WHERE parent_song_id IS NULL AND NOT is_collection;
