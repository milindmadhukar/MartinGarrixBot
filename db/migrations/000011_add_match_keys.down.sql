DROP INDEX IF EXISTS idx_songs_base_key;
DROP INDEX IF EXISTS idx_songs_match_key;
ALTER TABLE songs DROP COLUMN IF EXISTS base_key;
ALTER TABLE songs DROP COLUMN IF EXISTS match_key;
