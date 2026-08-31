-- Announcement is currently implicit: "a row was inserted, therefore announce it".
-- That coupling is why the STMPD fetcher throws away exact release dates -- changing
-- a date makes a stored song look new and would re-announce the back catalogue.
-- Make the watermark explicit and stamp every existing row as already announced,
-- so no later change to release_date can reach the announcement channel.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE songs ADD COLUMN IF NOT EXISTS announced_at  TIMESTAMPTZ;

UPDATE songs SET announced_at = NOW() WHERE announced_at IS NULL;

-- beatport_updated was carrying two contradictory meanings: "beatport metadata has
-- been written" (set by UpdateSongWithBeatportData) and "the STMPD link backfill has
-- visited this row" (set by UpdateSongWithStmpdLinks, and read as a skip guard by the
-- STMPD fetcher). The collision meant that the instant Beatport enriched a row, the
-- STMPD backfill was locked out of it permanently. Give the backfill its own column.
ALTER TABLE songs ADD COLUMN IF NOT EXISTS stmpd_synced_at TIMESTAMPTZ;

UPDATE songs SET stmpd_synced_at = NOW()
 WHERE source = 'stmpd'
   AND (spotify_url IS NOT NULL OR apple_music_url IS NOT NULL OR youtube_url IS NOT NULL);

-- Rows left NULL above are the backfill queue: the beatport-sourced rows that have
-- never received streaming links, plus the handful of stmpd rows that have none.
CREATE INDEX IF NOT EXISTS idx_songs_stmpd_unsynced ON songs (id) WHERE stmpd_synced_at IS NULL;
