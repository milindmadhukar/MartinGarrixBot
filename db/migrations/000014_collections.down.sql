DROP INDEX IF EXISTS idx_songs_playable;
ALTER TABLE songs DROP COLUMN IF EXISTS is_collection;
