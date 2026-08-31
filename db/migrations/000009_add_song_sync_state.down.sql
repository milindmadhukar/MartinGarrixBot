DROP INDEX IF EXISTS idx_songs_stmpd_unsynced;
ALTER TABLE songs DROP COLUMN IF EXISTS stmpd_synced_at;
ALTER TABLE songs DROP COLUMN IF EXISTS announced_at;
ALTER TABLE songs DROP COLUMN IF EXISTS first_seen_at;
