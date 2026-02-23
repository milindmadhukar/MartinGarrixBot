-- Remove beatport index
DROP INDEX IF EXISTS idx_songs_beatport_id;

-- Drop new constraint
ALTER TABLE songs DROP CONSTRAINT IF EXISTS unique_release;

-- Re-add old columns
ALTER TABLE songs ADD COLUMN IF NOT EXISTS release_year INTEGER;
ALTER TABLE songs ADD COLUMN IF NOT EXISTS pure_title VARCHAR(300);

-- Migrate release_date back to release_year (extract year part)
UPDATE songs SET release_year = CAST(SUBSTRING(release_date FROM 1 FOR 4) AS INTEGER) WHERE release_year IS NULL;
ALTER TABLE songs ALTER COLUMN release_year SET NOT NULL;

-- Drop new columns
ALTER TABLE songs DROP COLUMN IF EXISTS beatport_id;
ALTER TABLE songs DROP COLUMN IF EXISTS mix_name;
ALTER TABLE songs DROP COLUMN IF EXISTS release_date;
ALTER TABLE songs DROP COLUMN IF EXISTS release_name;
ALTER TABLE songs DROP COLUMN IF EXISTS genre;
ALTER TABLE songs DROP COLUMN IF EXISTS sub_genre;
ALTER TABLE songs DROP COLUMN IF EXISTS bpm;
ALTER TABLE songs DROP COLUMN IF EXISTS musical_key;
ALTER TABLE songs DROP COLUMN IF EXISTS length_ms;
ALTER TABLE songs DROP COLUMN IF EXISTS beatport_updated;
ALTER TABLE songs DROP COLUMN IF EXISTS source;

-- Restore old unique constraint
ALTER TABLE songs ADD CONSTRAINT unique_release UNIQUE (name, artists, release_year);
