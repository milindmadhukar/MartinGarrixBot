-- An unreleased song has no release date, and 1970-01-01 was standing in for that.
-- The placeholder is not harmless: it sorts the affected songs to the bottom of the
-- catalogue forever, and it reads as a fact when it is really an absence.
ALTER TABLE songs ALTER COLUMN release_date DROP NOT NULL;
ALTER TABLE songs ALTER COLUMN release_date DROP DEFAULT;

-- is_unreleased has existed since the first schema and nothing has ever read or
-- written it, so some of its values are stale. A real release date is the stronger
-- evidence: "Inside Our Hearts" was flagged unreleased and came out on 2025-07-25.
UPDATE songs SET is_unreleased = FALSE
 WHERE is_unreleased AND release_date IS NOT NULL AND release_date <> '1970-01-01';

UPDATE songs SET release_date = NULL WHERE is_unreleased;

-- A remaining placeholder means "we could not find out", which is also absence.
-- NULL says that honestly; 1970-01-01 does not.
UPDATE songs SET release_date = NULL WHERE release_date = '1970-01-01';

-- The flag and the date must agree. A song cannot be unreleased and have come out.
ALTER TABLE songs ADD CONSTRAINT unreleased_has_no_date
    CHECK (NOT (is_unreleased AND release_date IS NOT NULL));

-- Postgres treats NULLs as distinct in a unique constraint by default, which would
-- let the same undated song be inserted repeatedly. NULLS NOT DISTINCT keeps
-- unique_release meaning what it meant before the column became nullable.
ALTER TABLE songs DROP CONSTRAINT unique_release;
ALTER TABLE songs ADD CONSTRAINT unique_release
    UNIQUE NULLS NOT DISTINCT (name, artists, release_date);

CREATE INDEX IF NOT EXISTS idx_songs_unreleased ON songs (id) WHERE is_unreleased;
