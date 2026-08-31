DROP INDEX IF EXISTS idx_songs_canonical;
DROP INDEX IF EXISTS idx_songs_parent;
ALTER TABLE songs DROP COLUMN IF EXISTS is_instrumental;
ALTER TABLE songs DROP COLUMN IF EXISTS parent_song_id;
