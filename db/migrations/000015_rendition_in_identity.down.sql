ALTER TABLE songs DROP CONSTRAINT IF EXISTS unique_release;
ALTER TABLE songs ADD CONSTRAINT unique_release
    UNIQUE NULLS NOT DISTINCT (name, artists, release_date);
