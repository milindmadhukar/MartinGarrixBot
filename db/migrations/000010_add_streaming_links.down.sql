DROP INDEX IF EXISTS idx_songs_beatport_release_id;
DROP INDEX IF EXISTS idx_songs_stmpd_slug;
ALTER TABLE songs DROP COLUMN IF EXISTS stmpd_slug;
ALTER TABLE songs DROP COLUMN IF EXISTS beatport_release_id;
ALTER TABLE songs DROP COLUMN IF EXISTS beatport_url;
ALTER TABLE songs DROP COLUMN IF EXISTS youtube_music_url;
ALTER TABLE songs DROP COLUMN IF EXISTS amazon_music_url;
ALTER TABLE songs DROP COLUMN IF EXISTS tidal_url;
ALTER TABLE songs DROP COLUMN IF EXISTS deezer_url;
