DROP INDEX IF EXISTS idx_songs_unreleased;
ALTER TABLE songs DROP CONSTRAINT IF EXISTS unique_release;
ALTER TABLE songs ADD CONSTRAINT unique_release UNIQUE (name, artists, release_date);
ALTER TABLE songs DROP CONSTRAINT IF EXISTS unreleased_has_no_date;
UPDATE songs SET release_date = '1970-01-01' WHERE release_date IS NULL;
ALTER TABLE songs ALTER COLUMN release_date SET DEFAULT '1970-01-01';
ALTER TABLE songs ALTER COLUMN release_date SET NOT NULL;
