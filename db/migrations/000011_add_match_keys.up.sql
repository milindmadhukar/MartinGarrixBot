-- Cross-source identity keys, computed in Go by utils/matchkey.go.
--
-- Deliberately not GENERATED columns: the normalization folds diacritics, splits
-- artist credits on seven different separators and strips rendition names, and that
-- logic has to exist in Go anyway for matching incoming API results before they are
-- stored. Two implementations of it would drift.
--
-- match_key identifies one recording (song + rendition + artist set).
-- base_key identifies the song irrespective of rendition, so every remix of
-- "Told You So" shares one key with the original.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS match_key TEXT;
ALTER TABLE songs ADD COLUMN IF NOT EXISTS base_key  TEXT;

CREATE INDEX IF NOT EXISTS idx_songs_match_key ON songs (match_key) WHERE match_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_songs_base_key  ON songs (base_key)  WHERE base_key  IS NOT NULL;
