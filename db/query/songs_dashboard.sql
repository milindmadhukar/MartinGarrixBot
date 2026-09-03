-- Queries backing the dashboard's catalogue pages.
--
-- They are separate from songs.sql because none of the bot's song queries fit: those
-- exist to serve one song to one Discord user, so they filter renditions and
-- collections out of sight and cap at twenty rows. The dashboard's job is the opposite
-- -- show the table as it actually is, including the rows the bot deliberately hides,
-- because a row nobody can see is a row nobody can fix.
--
-- Search matches per term against the folded haystack rather than as one contiguous
-- LIKE; see the note on GetSongsLike in songs.sql. COALESCE onto an unfolded expression
-- keeps a row inserted before the next rekey-songs findable.

-- name: DashSongs :many
SELECT s.id, s.name, s.artists, s.mix_name, s.release_date, s.release_name, s.source,
       s.thumbnail_url, s.parent_song_id, s.is_collection, s.is_unreleased,
       s.is_instrumental, s.locked_fields, s.beatport_id, s.stmpd_slug,
       (s.lyrics IS NOT NULL)::boolean AS has_lyrics,
       (s.spotify_url IS NOT NULL OR s.youtube_url IS NOT NULL
        OR s.apple_music_url IS NOT NULL OR s.beatport_url IS NOT NULL
        OR s.deezer_url IS NOT NULL OR s.tidal_url IS NOT NULL
        OR s.amazon_music_url IS NOT NULL OR s.youtube_music_url IS NOT NULL)::boolean AS has_links,
       (SELECT count(*) FROM songs c WHERE c.parent_song_id = s.id)::bigint AS rendition_count
FROM songs s
WHERE COALESCE(s.search_text,
               LOWER(s.artists || ' ' || s.name || ' ' || COALESCE(s.mix_name, '')))
        LIKE ALL (sqlc.arg(terms)::text[])
  AND (sqlc.narg('source')::varchar IS NULL OR s.source = sqlc.narg('source'))
  AND (sqlc.narg('is_collection')::boolean IS NULL OR s.is_collection = sqlc.narg('is_collection'))
  AND (sqlc.narg('is_canonical')::boolean IS NULL
       OR (s.parent_song_id IS NULL) = sqlc.narg('is_canonical'))
  AND (sqlc.narg('has_lyrics')::boolean IS NULL
       OR (s.lyrics IS NOT NULL) = sqlc.narg('has_lyrics'))
  AND (sqlc.narg('has_artwork')::boolean IS NULL
       OR (s.thumbnail_url IS NOT NULL) = sqlc.narg('has_artwork'))
  AND (sqlc.narg('has_links')::boolean IS NULL
       OR (s.spotify_url IS NOT NULL OR s.youtube_url IS NOT NULL
           OR s.apple_music_url IS NOT NULL OR s.beatport_url IS NOT NULL
           OR s.deezer_url IS NOT NULL OR s.tidal_url IS NOT NULL
           OR s.amazon_music_url IS NOT NULL OR s.youtube_music_url IS NOT NULL)
          = sqlc.narg('has_links'))
  -- The problem filter passes the ids the audit flagged. Keeping it as a plain id set
  -- means every invariant is filterable without a query per check, and the filters
  -- above still compose with it.
  AND (sqlc.narg('ids')::bigint[] IS NULL OR s.id = ANY(sqlc.narg('ids')::bigint[]))
-- id is the tiebreak for the same reason DashModlogs orders by id DESC: a whole release
-- shares one date, so without it those rows shuffle between pages and paging forwards
-- shows some twice while skipping others.
ORDER BY s.release_date DESC NULLS LAST, s.id DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: DashSongsCount :one
SELECT COUNT(*) FROM songs s
WHERE COALESCE(s.search_text,
               LOWER(s.artists || ' ' || s.name || ' ' || COALESCE(s.mix_name, '')))
        LIKE ALL (sqlc.arg(terms)::text[])
  AND (sqlc.narg('source')::varchar IS NULL OR s.source = sqlc.narg('source'))
  AND (sqlc.narg('is_collection')::boolean IS NULL OR s.is_collection = sqlc.narg('is_collection'))
  AND (sqlc.narg('is_canonical')::boolean IS NULL
       OR (s.parent_song_id IS NULL) = sqlc.narg('is_canonical'))
  AND (sqlc.narg('has_lyrics')::boolean IS NULL
       OR (s.lyrics IS NOT NULL) = sqlc.narg('has_lyrics'))
  AND (sqlc.narg('has_artwork')::boolean IS NULL
       OR (s.thumbnail_url IS NOT NULL) = sqlc.narg('has_artwork'))
  AND (sqlc.narg('has_links')::boolean IS NULL
       OR (s.spotify_url IS NOT NULL OR s.youtube_url IS NOT NULL
           OR s.apple_music_url IS NOT NULL OR s.beatport_url IS NOT NULL
           OR s.deezer_url IS NOT NULL OR s.tidal_url IS NOT NULL
           OR s.amazon_music_url IS NOT NULL OR s.youtube_music_url IS NOT NULL)
          = sqlc.narg('has_links'))
  AND (sqlc.narg('ids')::bigint[] IS NULL OR s.id = ANY(sqlc.narg('ids')::bigint[]));

-- name: DashSongAnnouncements :many
-- Where a song has been posted, for the song page. idx_song_announcements_song covers
-- this; there was no per-song listing before because the bot only ever reads
-- announcements by age, to decide what to refresh.
SELECT guild_id, channel_id, message_id, buttons_key, posted_at, edited_at,
       edit_count, failed_at
FROM song_announcements
WHERE song_id = $1
ORDER BY posted_at DESC;

-- name: DashUpdateSong :one
-- Every editable column is submitted on every save, including the empty ones, so that
-- clearing a field works -- the same contract UpdateGuildConfig has.
--
-- locked_fields arrives already computed rather than being derived here. The handler
-- has diffed the submitted values against the stored row to build the audit line
-- anyway, so it already knows which columns changed; expressing "which of these twenty
-- assignments changed something" in SQL would be a second, harder implementation of a
-- question already answered.
--
-- No lock guard on these assignments, unlike every automated writer: this IS the
-- authority the locks exist to protect. A person looking at the row is the one writing.
UPDATE songs SET
    -- The ::varchar casts on name are load-bearing, the same way the ::text casts in
    -- UpdateSongWithStmpdRelease are. This parameter appears both as an assignment and
    -- inside the normalized_name CASE below, and without a cast Postgres tries to
    -- deduce a type for it twice and raises 42P08, "inconsistent types deduced for
    -- parameter $1".
    name = sqlc.arg(name)::varchar,
    artists = sqlc.arg(artists),
    mix_name = sqlc.narg(mix_name),
    thumbnail_url = sqlc.narg(thumbnail_url),
    spotify_url = sqlc.narg(spotify_url),
    apple_music_url = sqlc.narg(apple_music_url),
    youtube_url = sqlc.narg(youtube_url),
    youtube_music_url = sqlc.narg(youtube_music_url),
    deezer_url = sqlc.narg(deezer_url),
    tidal_url = sqlc.narg(tidal_url),
    amazon_music_url = sqlc.narg(amazon_music_url),
    beatport_url = sqlc.narg(beatport_url),
    release_date = sqlc.narg(release_date)::text,
    release_name = sqlc.narg(release_name),
    lyrics = sqlc.narg(lyrics),
    genre = sqlc.narg(genre),
    sub_genre = sqlc.narg(sub_genre),
    bpm = sqlc.narg(bpm)::int,
    musical_key = sqlc.narg(musical_key),
    is_collection = sqlc.arg(is_collection),
    is_instrumental = sqlc.arg(is_instrumental),
    is_unreleased = sqlc.arg(is_unreleased),
    -- Derived from name, so a hand-edited name invalidates it. Cleared rather than
    -- recomputed for the reason 000021 gives: SQL cannot derive it, and NULL means
    -- "derive on read", which is survivable where a stale stored value is not.
    normalized_name = CASE WHEN name IS DISTINCT FROM sqlc.arg(name)::varchar
                           THEN NULL ELSE normalized_name END,
    locked_fields = sqlc.arg(locked_fields)::text[]
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DashUnlockSongField :one
-- Hands one field back to automation. array_remove is a no-op on a field that is not
-- locked, so a double submit is harmless.
UPDATE songs SET locked_fields = array_remove(locked_fields, sqlc.arg(field)::text)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DashSetSongParent :one
-- Re-parenting from the dashboard, which unlike the automated writer is never refused
-- by a lock: a person moving a row is the authority the lock protects. Locking the
-- column is what stops the next link-remix-parents run from moving it back.
UPDATE songs
SET parent_song_id = sqlc.narg(parent_song_id)::bigint,
    -- ARRAY[...] rather than a bare string: `text[] || 'parent_song_id'` makes
    -- Postgres read the literal as an array literal and fail with 22P02, since an
    -- unadorned word is not a valid array literal.
    locked_fields = CASE WHEN 'parent_song_id' = ANY(locked_fields) THEN locked_fields
                         ELSE locked_fields || ARRAY['parent_song_id'::text] END
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DashRepointChildren :execrows
-- Moves every rendition of one song onto another. Used when promoting a rendition to be
-- the canonical row: its siblings have to follow it, or they are left pointing at a row
-- that is now a child and the tree is two levels deep.
UPDATE songs SET parent_song_id = sqlc.arg(new_parent)
WHERE parent_song_id = sqlc.arg(old_parent) AND id <> sqlc.arg(new_parent);

-- name: DashRepointAnnouncements :execrows
-- Moves a song's posted announcements onto the row that survives a merge.
--
-- Without this the merge loses them: song_announcements.song_id is ON DELETE CASCADE,
-- so deleting the merged-away row deletes its announcement history with it, and the
-- refresh loop then has no record that those messages exist.
UPDATE song_announcements SET song_id = sqlc.arg(new_song)
WHERE song_id = sqlc.arg(old_song);

-- name: DashSongCandidates :many
-- Rows a given song could be filed under or merged with: same folded terms, never
-- itself. Deliberately not restricted to canonical rows -- promoting a rendition is a
-- thing the page has to allow, and the handler enforces the tree's shape.
SELECT id, name, artists, mix_name, release_date, source, parent_song_id, is_collection,
       thumbnail_url
FROM songs
WHERE id <> sqlc.arg(exclude_id)
  AND COALESCE(search_text, LOWER(artists || ' ' || name || ' ' || COALESCE(mix_name, '')))
        LIKE ALL (sqlc.arg(terms)::text[])
ORDER BY (parent_song_id IS NULL) DESC, release_date DESC NULLS LAST, id
LIMIT 25;
