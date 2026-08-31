-- The archive scrape read three streaming links because that is all the page
-- exposed. The dataset behind it carries eight, plus a beatport release URL.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS deezer_url        TEXT;
ALTER TABLE songs ADD COLUMN IF NOT EXISTS tidal_url         TEXT;
ALTER TABLE songs ADD COLUMN IF NOT EXISTS amazon_music_url  TEXT;
ALTER TABLE songs ADD COLUMN IF NOT EXISTS youtube_music_url TEXT;
ALTER TABLE songs ADD COLUMN IF NOT EXISTS beatport_url      TEXT;

-- beatport_url points at a RELEASE (/release/<slug>/<id>), while songs.beatport_id
-- holds a beatport TRACK id. They are different namespaces and must not be conflated:
-- this id joins to a track's release, not to the track. Only about 30 of the 1015
-- releases carry one, so it is a useful exact match where present and never a
-- primary key.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS beatport_release_id INTEGER;

-- stmpd_slug is the STMPD-side identity. All 1015 slugs in the dataset are distinct
-- and stable, which lets the fetcher stop using (name, artists, release_date) as its
-- existence test -- the composite key that forced release_date to be stored as a
-- year placeholder, because correcting a date made a stored song look brand new.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS stmpd_slug TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_songs_stmpd_slug
    ON songs (stmpd_slug) WHERE stmpd_slug IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_songs_beatport_release_id
    ON songs (beatport_release_id) WHERE beatport_release_id IS NOT NULL;
