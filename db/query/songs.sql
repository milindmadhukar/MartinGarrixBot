-- name: GetSong :one
-- IS NOT DISTINCT FROM, not "=": release_date is NULL for unreleased songs and for
-- ones whose date we could not establish, and "=" never matches NULL.
SELECT * FROM songs
WHERE name = $1 AND artists = $2 AND release_date IS NOT DISTINCT FROM sqlc.narg(release_date);

-- name: GetSongByID :one
SELECT * FROM songs WHERE id = $1;

-- name: GetSongByBeatportID :one
SELECT * FROM songs WHERE beatport_id = $1;

-- name: GetSongsLike :many
-- Autocomplete offers one entry per song. Remix rows are excluded: ten "Told You
-- So" choices is not a useful list, and the choice payload is capped at 100 bytes
-- by Discord, which long remix names were overflowing.
SELECT id, name, artists, release_date
FROM songs
WHERE parent_song_id IS NULL
  AND LOWER(artists || ' - ' || name) LIKE LOWER($1)
ORDER BY release_date DESC
LIMIT 20;

-- name: GetRandomSongNames :many
SELECT id, name, artists, release_date
FROM songs
WHERE parent_song_id IS NULL
ORDER BY RANDOM()
LIMIT 20;

-- name: GetSongsWithLyricsLike :many
SELECT id, name, artists, release_date
FROM songs
WHERE lyrics IS NOT NULL
  AND parent_song_id IS NULL
  AND LOWER(artists || ' - ' || name) LIKE LOWER($1)
ORDER BY release_date DESC
LIMIT 20;

-- name: GetRandomSongNamesWithLyrics :many
SELECT id, name, artists, release_date
FROM songs
WHERE lyrics IS NOT NULL AND parent_song_id IS NULL
ORDER BY RANDOM()
LIMIT 20;

-- name: GetRandomSongWithLyrics :one
-- '%ytram%' was '%ytrram%'. The typo meant no Ytram song has ever been served by
-- the quiz; config.toml's monitored-artist list confirms the correct spelling.
SELECT * FROM songs
WHERE lyrics IS NOT NULL
AND NOT is_instrumental
AND NOT is_collection
AND parent_song_id IS NULL
AND (LOWER(artists) LIKE '%martin garrix%'
   OR LOWER(artists) LIKE '%area21%'
   OR LOWER(artists) LIKE '%ytram%'
   OR LOWER(artists) LIKE '%grx%')
ORDER BY RANDOM()
LIMIT 1;

-- name: GetRandomSongWithLyricsEasy :one
SELECT * FROM songs
WHERE lyrics IS NOT NULL
AND NOT is_instrumental
AND NOT is_collection
AND parent_song_id IS NULL
AND LOWER(artists) LIKE '%martin garrix%'
ORDER BY RANDOM()
LIMIT 1;

-- name: InsertRelease :one
INSERT INTO songs (
    name, artists, release_date, thumbnail_url, stmpd_slug,
    spotify_url, apple_music_url, youtube_url, youtube_music_url,
    deezer_url, tidal_url, amazon_music_url, beatport_url, beatport_release_id,
    stmpd_synced_at, source
) VALUES (
    sqlc.arg(name), sqlc.arg(artists), sqlc.arg(release_date), sqlc.narg(thumbnail_url), sqlc.narg(stmpd_slug),
    sqlc.narg(spotify_url), sqlc.narg(apple_music_url), sqlc.narg(youtube_url), sqlc.narg(youtube_music_url),
    sqlc.narg(deezer_url), sqlc.narg(tidal_url), sqlc.narg(amazon_music_url),
    sqlc.narg(beatport_url), sqlc.narg(beatport_release_id),
    NOW(), 'stmpd'
)
RETURNING *;

-- name: GetSongByStmpdSlug :one
SELECT * FROM songs WHERE stmpd_slug = $1;

-- name: UpdateSongWithStmpdRelease :execrows
-- The full-fidelity counterpart to UpdateSongWithStmpdLinks: everything the STMPD
-- dataset knows about a release, applied to a row that already exists.
--
-- release_date is COALESCEd rather than left alone because the dataset carries the
-- exact date and many stored rows carry a "<year>-01-01" placeholder. Correcting it
-- is safe now only because announcement is gated on songs.announced_at, which every
-- pre-existing row already has stamped -- without that watermark this UPDATE would
-- make the back catalogue look new.
--
-- thumbnail_url is only ever filled in, never overwritten: roughly 800 of the 1015
-- releases have no artwork in the dataset, and an empty value there means "unknown",
-- not "this release has none".
UPDATE songs SET
    stmpd_slug          = COALESCE(sqlc.narg(stmpd_slug),          stmpd_slug),
    release_date        = COALESCE(sqlc.narg(release_date),        release_date),
    -- The catalogue's `version` field, which nothing used to carry across. A legacy
    -- row for "La La La (Drove Remix)" was stored as plain "La La La", so it keyed
    -- identically to the original: it showed up as a second, indistinguishable entry
    -- in autocomplete, and dedupe wanted to merge the two and delete one. Recording
    -- the rendition instead makes it a remix of the original, exactly like the
    -- Catharina remixes, which carry theirs in mix_name already.
    mix_name            = COALESCE(mix_name, sqlc.narg(mix_name)),
    spotify_url         = COALESCE(sqlc.narg(spotify_url),         spotify_url),
    apple_music_url     = COALESCE(sqlc.narg(apple_music_url),     apple_music_url),
    youtube_url         = COALESCE(sqlc.narg(youtube_url),         youtube_url),
    youtube_music_url   = COALESCE(sqlc.narg(youtube_music_url),   youtube_music_url),
    deezer_url          = COALESCE(sqlc.narg(deezer_url),          deezer_url),
    tidal_url           = COALESCE(sqlc.narg(tidal_url),           tidal_url),
    amazon_music_url    = COALESCE(sqlc.narg(amazon_music_url),    amazon_music_url),
    beatport_url        = COALESCE(sqlc.narg(beatport_url),        beatport_url),
    beatport_release_id = COALESCE(sqlc.narg(beatport_release_id), beatport_release_id),
    thumbnail_url       = CASE WHEN COALESCE(thumbnail_url, '') = ''
                               THEN sqlc.narg(thumbnail_url) ELSE thumbnail_url END,
    -- A song someone added because they heard it played, which has now actually
    -- come out. Clearing announced_at re-arms the announcement: the row is old, but
    -- the release is news, and this is the one case where re-announcing is right.
    --
    -- The ::text casts are load-bearing. IS NOT NULL accepts any type, so these are
    -- the only places the parameter appears without a type to infer from, and
    -- Postgres rejects the whole statement with 42P08 at execution time.
    is_unreleased = CASE WHEN sqlc.narg(release_date)::text IS NOT NULL THEN FALSE ELSE is_unreleased END,
    announced_at  = CASE WHEN is_unreleased AND sqlc.narg(release_date)::text IS NOT NULL
                         THEN NULL ELSE announced_at END,
    stmpd_synced_at     = NOW()
WHERE id = sqlc.arg(id)
  AND (
       (is_unreleased AND sqlc.narg(release_date)::text IS NOT NULL)
    OR stmpd_slug          IS DISTINCT FROM COALESCE(sqlc.narg(stmpd_slug),          stmpd_slug)
    OR release_date        IS DISTINCT FROM COALESCE(sqlc.narg(release_date),        release_date)
    OR mix_name            IS DISTINCT FROM COALESCE(mix_name, sqlc.narg(mix_name))
    OR spotify_url         IS DISTINCT FROM COALESCE(sqlc.narg(spotify_url),         spotify_url)
    OR apple_music_url     IS DISTINCT FROM COALESCE(sqlc.narg(apple_music_url),     apple_music_url)
    OR youtube_url         IS DISTINCT FROM COALESCE(sqlc.narg(youtube_url),         youtube_url)
    OR youtube_music_url   IS DISTINCT FROM COALESCE(sqlc.narg(youtube_music_url),   youtube_music_url)
    OR deezer_url          IS DISTINCT FROM COALESCE(sqlc.narg(deezer_url),          deezer_url)
    OR tidal_url           IS DISTINCT FROM COALESCE(sqlc.narg(tidal_url),           tidal_url)
    OR amazon_music_url    IS DISTINCT FROM COALESCE(sqlc.narg(amazon_music_url),    amazon_music_url)
    OR beatport_url        IS DISTINCT FROM COALESCE(sqlc.narg(beatport_url),        beatport_url)
    OR beatport_release_id IS DISTINCT FROM COALESCE(sqlc.narg(beatport_release_id), beatport_release_id)
    -- Cast for the same reason as in UpdateSongWithStmpdLinks: IS NOT NULL leaves
    -- the parameter's type undetermined and Postgres raises 42P08.
    OR (COALESCE(thumbnail_url, '') = '' AND sqlc.narg(thumbnail_url)::text IS NOT NULL)
    OR stmpd_synced_at IS NULL
  );

-- name: InsertBeatportSong :one
INSERT INTO songs (
    name, artists, release_date, thumbnail_url, beatport_id, mix_name,
    release_name, genre, sub_genre, bpm, musical_key, length_ms, source
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'beatport')
RETURNING *;

-- name: DoesSongExist :one
SELECT EXISTS(SELECT 1 FROM songs
  WHERE name = $1 AND artists = $2 AND release_date IS NOT DISTINCT FROM sqlc.narg(release_date));

-- name: DoesBeatportSongExist :one
SELECT EXISTS(SELECT 1 FROM songs WHERE beatport_id = $1);

-- name: UpdateSongWithBeatportData :execrows
-- :execrows plus the IS DISTINCT FROM guard below make this a no-op when the row
-- already holds this data. Without it every cycle rewrote the same ~73 rows and
-- reported updated=73 forever, which hid whether anything had actually changed.
-- thumbnail_url is COALESCEd rather than assigned: a beatport track with no square
-- artwork arrives as NULL and must not erase artwork another source already found.
UPDATE songs SET
    name          = sqlc.arg(name),
    artists       = sqlc.arg(artists),
    thumbnail_url = COALESCE(sqlc.narg(thumbnail_url), thumbnail_url),
    beatport_id   = sqlc.narg(beatport_id),
    mix_name      = sqlc.narg(mix_name),
    release_date  = sqlc.arg(release_date),
    release_name  = sqlc.narg(release_name),
    genre         = sqlc.narg(genre),
    sub_genre     = sqlc.narg(sub_genre),
    bpm           = sqlc.narg(bpm),
    musical_key   = sqlc.narg(musical_key),
    length_ms     = sqlc.narg(length_ms),
    beatport_updated = TRUE
WHERE id = sqlc.arg(id)
  AND (
       name          IS DISTINCT FROM sqlc.arg(name)
    OR artists       IS DISTINCT FROM sqlc.arg(artists)
    OR thumbnail_url IS DISTINCT FROM COALESCE(sqlc.narg(thumbnail_url), thumbnail_url)
    OR beatport_id   IS DISTINCT FROM sqlc.narg(beatport_id)
    OR mix_name      IS DISTINCT FROM sqlc.narg(mix_name)
    OR release_date  IS DISTINCT FROM sqlc.arg(release_date)
    OR release_name  IS DISTINCT FROM sqlc.narg(release_name)
    OR genre         IS DISTINCT FROM sqlc.narg(genre)
    OR sub_genre     IS DISTINCT FROM sqlc.narg(sub_genre)
    OR bpm           IS DISTINCT FROM sqlc.narg(bpm)
    OR musical_key   IS DISTINCT FROM sqlc.narg(musical_key)
    OR length_ms     IS DISTINCT FROM sqlc.narg(length_ms)
    OR beatport_updated IS DISTINCT FROM TRUE
  );

-- name: UpdateSongWithStmpdLinks :execrows
-- This no longer touches beatport_updated. That flag is the beatport fetcher's own
-- "already enriched, skip" sentinel; setting it here made the STMPD backfill skip
-- every row beatport had ever touched, which is why 497 beatport rows had no links.
-- stmpd_synced_at is this fetcher's separate record of having visited the row.
UPDATE songs SET
    spotify_url     = COALESCE(sqlc.narg(spotify_url),     spotify_url),
    apple_music_url = COALESCE(sqlc.narg(apple_music_url), apple_music_url),
    youtube_url     = COALESCE(sqlc.narg(youtube_url),     youtube_url),
    thumbnail_url   = CASE WHEN thumbnail_url IS NULL OR thumbnail_url = ''
                           THEN sqlc.narg(thumbnail_url) ELSE thumbnail_url END,
    stmpd_synced_at = NOW()
WHERE id = sqlc.arg(id)
  AND (
       spotify_url     IS DISTINCT FROM COALESCE(sqlc.narg(spotify_url),     spotify_url)
    OR apple_music_url IS DISTINCT FROM COALESCE(sqlc.narg(apple_music_url), apple_music_url)
    OR youtube_url     IS DISTINCT FROM COALESCE(sqlc.narg(youtube_url),     youtube_url)
    -- Only a reason to write when there is actually a thumbnail to write. Testing
    -- emptiness alone would match forever on a row neither side has artwork for,
    -- re-stamping it every cycle and reintroducing the churn this guard removes.
    --
    -- The cast is required, not cosmetic: IS NOT NULL accepts any type, so this is
    -- the one place the parameter appears without a type to infer from, and Postgres
    -- rejects the whole statement with 42P08 at execution time.
    OR (COALESCE(thumbnail_url, '') = '' AND sqlc.narg(thumbnail_url)::text IS NOT NULL)
    OR stmpd_synced_at IS NULL
  );

-- name: MarkBeatportUpdated :exec
UPDATE songs SET beatport_updated = TRUE WHERE id = $1;

-- name: GetAllSongsForMatching :many
SELECT id, name, artists, source, beatport_id, beatport_release_id,
       stmpd_slug, match_key, base_key, mix_name, spotify_url, stmpd_synced_at
FROM songs;

-- name: SetSongKeys :execrows
UPDATE songs SET match_key = $2, base_key = $3
WHERE id = $1 AND (match_key IS DISTINCT FROM $2 OR base_key IS DISTINCT FROM $3);

-- name: GetSongsForKeying :many
SELECT id, name, artists, mix_name, length_ms, is_collection FROM songs ORDER BY id;

-- name: GetRandomSongForRadio :one
-- Canonical rows only, so the rotation does not play six versions of one track, and
-- no collections: an EP, an album or a remix package is a release containing songs,
-- not a song, and queueing one asks the player to stream a whole record as a track.
SELECT id, name, artists, thumbnail_url, youtube_url
FROM songs
WHERE youtube_url IS NOT NULL
  AND parent_song_id IS NULL
  AND NOT is_collection
  AND (length_ms IS NULL OR length_ms <= 600000)
ORDER BY RANDOM()
LIMIT 1;
-- name: MarkSongAnnounced :exec
UPDATE songs SET announced_at = NOW() WHERE id = $1 AND announced_at IS NULL;

-- name: GetSongsNeverAnnounced :many
SELECT id, name, artists, release_date FROM songs WHERE announced_at IS NULL;

-- name: MergeSongRows :exec
-- Fold `loser` into `winner`, keeping the first non-null of each field. Used when a
-- release_date correction would collide with the unique_release constraint: the
-- collision means a twin row already holds that identity, so the two are the same
-- song arriving from two sources. Nothing has a foreign key to songs, so the loser
-- can be deleted afterwards.
UPDATE songs w SET
    spotify_url     = COALESCE(w.spotify_url,     l.spotify_url),
    apple_music_url = COALESCE(w.apple_music_url, l.apple_music_url),
    youtube_url     = COALESCE(w.youtube_url,     l.youtube_url),
    thumbnail_url   = COALESCE(NULLIF(w.thumbnail_url, ''), l.thumbnail_url),
    lyrics          = COALESCE(w.lyrics,          l.lyrics),
    bpm             = COALESCE(w.bpm,             l.bpm),
    musical_key     = COALESCE(w.musical_key,     l.musical_key),
    length_ms       = COALESCE(w.length_ms,       l.length_ms),
    genre           = COALESCE(w.genre,           l.genre),
    sub_genre       = COALESCE(w.sub_genre,       l.sub_genre),
    mix_name        = COALESCE(w.mix_name,        l.mix_name),
    release_name    = COALESCE(w.release_name,    l.release_name),
    -- The columns below were added after this query was first written. Leaving them
    -- out silently discarded the loser's slug and its deezer/tidal/amazon links.
    -- beatport_id and stmpd_slug are handled by Release/AdoptSongIdentifiers:
    -- both are uniquely indexed and cannot be copied while the loser still holds them.
    youtube_music_url   = COALESCE(w.youtube_music_url,   l.youtube_music_url),
    deezer_url          = COALESCE(w.deezer_url,          l.deezer_url),
    tidal_url           = COALESCE(w.tidal_url,           l.tidal_url),
    amazon_music_url    = COALESCE(w.amazon_music_url,    l.amazon_music_url),
    beatport_url        = COALESCE(w.beatport_url,        l.beatport_url),
    beatport_release_id = COALESCE(w.beatport_release_id, l.beatport_release_id),
    -- A real date always beats the 1970-01-01 placeholder, whichever row holds it.
    release_date    = CASE WHEN w.release_date = '1970-01-01' AND l.release_date <> '1970-01-01'
                           THEN l.release_date ELSE w.release_date END,
    announced_at    = LEAST(w.announced_at,       l.announced_at),
    first_seen_at   = LEAST(w.first_seen_at,      l.first_seen_at),
    stmpd_synced_at = COALESCE(w.stmpd_synced_at, l.stmpd_synced_at),
    beatport_updated = w.beatport_updated OR l.beatport_updated
FROM songs l
WHERE w.id = sqlc.arg(winner_id) AND l.id = sqlc.arg(loser_id);

-- name: DeleteSong :exec
DELETE FROM songs WHERE id = $1;

-- name: GetSongsMissingLinks :many
-- The backfill queue: rows the STMPD sync has never successfully applied to. After
-- migration 000009 this is exactly the set of rows carrying no streaming links.
SELECT id, name, artists, release_date, source, beatport_id
FROM songs WHERE stmpd_synced_at IS NULL ORDER BY id;

-- name: CountLinklessSongs :one
SELECT count(*) FROM songs
WHERE spotify_url IS NULL AND apple_music_url IS NULL AND youtube_url IS NULL;

-- name: SetSongParent :execrows
UPDATE songs SET parent_song_id = sqlc.narg(parent_song_id)
WHERE id = sqlc.arg(id) AND parent_song_id IS DISTINCT FROM sqlc.narg(parent_song_id);

-- name: SetSongInstrumental :execrows
UPDATE songs SET is_instrumental = $2 WHERE id = $1 AND is_instrumental IS DISTINCT FROM $2;

-- name: GetSongsForParentLinking :many
SELECT id, name, artists, mix_name, release_date, source, base_key,
       spotify_url, youtube_url, apple_music_url, lyrics, parent_song_id, is_collection
FROM songs ORDER BY id;

-- name: GetSongMixes :many
-- The renditions hanging off a canonical song, for the track card to list.
SELECT id, name, mix_name, artists FROM songs
WHERE parent_song_id = $1 ORDER BY release_date, id;

-- name: CopyLyricsToRemixes :execrows
-- Lyrics are entered by hand against the canonical row. A remix of a vocal track has
-- the same words, so fan them out rather than making someone paste them ten times.
UPDATE songs t SET lyrics = s.lyrics
FROM songs s
WHERE s.id = $1 AND t.parent_song_id = s.id AND t.lyrics IS NULL
  AND s.lyrics IS NOT NULL AND NOT t.is_instrumental;

-- name: GetSongsWithPlaceholderDate :many
-- Rows whose release date is not a real date: the 1970-01-01 sentinel the original
-- importer wrote when it had none, and rows dated the 1st of January, which is what
-- the year-only scrape produced before the dataset gave exact dates.
-- Ordered so the ones resolvable from a stored link come first.
SELECT id, name, artists, release_date, apple_music_url, spotify_url
FROM songs WHERE release_date = '1970-01-01' OR release_date LIKE '%-01-01'
ORDER BY (apple_music_url IS NULL), id;

-- name: SetSongReleaseDate :execrows
UPDATE songs SET release_date = $2 WHERE id = $1 AND release_date IS DISTINCT FROM $2;

-- name: GetDuplicateMatchKeyRows :many
-- Every row belonging to a match_key held by more than one row. A match_key is the
-- artist set, the base title and the rendition, so two rows sharing one are the same
-- recording stored twice -- usually once per source, with the variant in `name` on
-- one side and in `mix_name` on the other.
SELECT id, name, artists, mix_name, release_date, source, match_key,
       stmpd_slug, beatport_id, spotify_url, apple_music_url, youtube_url,
       lyrics, parent_song_id, thumbnail_url
FROM songs
WHERE match_key IN (
    SELECT match_key FROM songs
    WHERE match_key IS NOT NULL AND match_key <> '||'
    GROUP BY match_key HAVING count(*) > 1
)
ORDER BY match_key, id;

-- name: RepointChildren :execrows
-- Move a merged-away row's remixes onto the row that survives.
UPDATE songs SET parent_song_id = sqlc.arg(new_parent)
WHERE parent_song_id = sqlc.arg(old_parent);

-- name: ReleaseSongIdentifiers :exec
-- Clear the uniquely-indexed identifiers from a row that is about to be merged away.
--
-- beatport_id and stmpd_slug each carry a partial unique index, so copying them onto
-- the surviving row while this one still holds them violates the index. The caller
-- captured the values first and reassigns them with AdoptSongIdentifiers once this
-- row no longer claims them.
UPDATE songs SET beatport_id = NULL, stmpd_slug = NULL WHERE id = $1;

-- name: AdoptSongIdentifiers :exec
-- Give the surviving row the identifiers released above, without overwriting any it
-- already has of its own.
UPDATE songs SET
    beatport_id = COALESCE(beatport_id, sqlc.narg(beatport_id)),
    stmpd_slug  = COALESCE(stmpd_slug,  sqlc.narg(stmpd_slug))
WHERE id = sqlc.arg(id);

-- name: GetCanonicalSongsForReview :many
-- Songs as a user sees them, for reporting likely-duplicate groups that are not safe
-- to merge automatically.
SELECT id, name, artists, release_date, source, base_key, match_key,
       spotify_url, youtube_url, apple_music_url, beatport_id
FROM songs WHERE parent_song_id IS NULL AND base_key IS NOT NULL AND base_key <> '|'
ORDER BY base_key, id;

-- name: MarkSongUnreleased :exec
-- For adding a track that has been played but not put out. The date must go with it:
-- the unreleased_has_no_date constraint will not allow one without the other.
UPDATE songs SET is_unreleased = TRUE, release_date = NULL WHERE id = $1;

-- name: GetUnreleasedSongs :many
SELECT id, name, artists, lyrics IS NOT NULL AS has_lyrics, first_seen_at
FROM songs WHERE is_unreleased ORDER BY first_seen_at DESC, id;

-- name: SetSongCollection :execrows
UPDATE songs SET is_collection = $2 WHERE id = $1 AND is_collection IS DISTINCT FROM $2;

-- name: ClearStmpdSlug :execrows
-- Detach a release identity from a row it does not belong to.
UPDATE songs SET stmpd_slug = NULL WHERE id = $1;

-- name: GetSongsNeedingYoutube :many
-- Rows whose YouTube button is missing or points at a playlist rather than a video.
-- A playlist link is worse than none: it sends people to a playlist instead of the
-- song they asked for.
-- youtu.be short links are perfectly good videos -- the radio already follows them
-- -- so they are not "missing". Only a genuine playlist or an absent link is.
SELECT id, name, artists, mix_name, youtube_url, spotify_url
FROM songs
WHERE youtube_url IS NULL
   OR (youtube_url NOT LIKE '%watch?v=%' AND youtube_url NOT LIKE '%youtu.be/%')
ORDER BY id;

-- name: GetSongsWithUntidyYoutubeURL :many
-- Rows whose link is a video but not in canonical form: a short link, or one carrying
-- share tracking. Rewriting them makes every query and report agree on what a video
-- looks like.
SELECT id, youtube_url FROM songs
WHERE youtube_url IS NOT NULL
  AND (youtube_url LIKE '%youtu.be/%' OR youtube_url LIKE '%si=%' OR youtube_url LIKE '%&t=%')
ORDER BY id;

-- name: SetSongYoutubeURL :execrows
UPDATE songs SET youtube_url = sqlc.narg(youtube_url)
WHERE id = sqlc.arg(id) AND youtube_url IS DISTINCT FROM sqlc.narg(youtube_url);

-- name: ClearPlaylistSpotifyLinks :execrows
-- Spotify playlist links cannot be resolved without the Spotify API, and pointing a
-- song's Spotify button at a playlist is worse than showing no button.
UPDATE songs SET spotify_url = NULL WHERE spotify_url LIKE '%/playlist/%';

-- name: GetSongsMissingArtwork :many
-- Rows with no cover art. Beatport cannot help with these: every row it knows about
-- already has artwork, and none of these carry a beatport_id. Apple can -- most of
-- them have an Apple link, and the lookup returns a cover.
SELECT id, name, artists, apple_music_url
FROM songs WHERE coalesce(thumbnail_url, '') = ''
ORDER BY (apple_music_url IS NULL), id;

-- name: SetSongArtwork :execrows
UPDATE songs SET thumbnail_url = sqlc.narg(thumbnail_url)
WHERE id = sqlc.arg(id) AND coalesce(thumbnail_url, '') = '';

-- name: GetSongsToCheckForCollection :many
-- Rows not yet known to be releases, that carry an Apple link we can ask about.
SELECT id, name, artists, apple_music_url
FROM songs
WHERE NOT is_collection AND apple_music_url IS NOT NULL
  AND apple_music_url NOT LIKE '%/playlist/%'
ORDER BY id;

-- name: ClearUnresolvedYoutubePlaylists :execrows
-- A YouTube link that names a playlist rather than a video buys nothing: the radio
-- ignores it and falls back to a search either way, and the button sends a listener
-- to a playlist they did not ask for. Once everything resolvable has been resolved,
-- what is left is better as no button at all.
UPDATE songs SET youtube_url = NULL
WHERE youtube_url IS NOT NULL
  AND youtube_url NOT LIKE '%watch?v=%'
  AND youtube_url NOT LIKE '%youtu.be/%';
