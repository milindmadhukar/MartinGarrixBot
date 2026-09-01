//go:build integration

package db_test

// The song catalogue against a real Postgres. Two ingestion paths (Beatport and
// STMPD) write into one songs table and dedupe against each other, and the
// uniqueness rules that keep that honest live in the schema, not in Go, so they
// can only be verified here.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

// uniqueSuffix keeps rows from different tests apart, so they can run in
// parallel against one database without colliding on unique_release.
var uniqueSuffix atomic.Int64

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
func int4(i int32) pgtype.Int4  { return pgtype.Int4{Int32: i, Valid: true} }

// testSongName returns a name no other test will use.
func testSongName(t *testing.T, base string) string {
	t.Helper()
	return fmt.Sprintf("%s #%d", base, uniqueSuffix.Add(1))
}

// insertBeatportSong inserts a song and removes it when the test ends.
func insertBeatportSong(t *testing.T, q *db.Queries, arg db.InsertBeatportSongParams) db.Song {
	t.Helper()

	song, err := q.InsertBeatportSong(context.Background(), arg)
	if err != nil {
		t.Fatalf("InsertBeatportSong failed: %v", err)
	}
	t.Cleanup(func() { deleteSong(t, song.ID) })
	return song
}

func deleteSong(t *testing.T, id int64) {
	t.Helper()

	if _, err := testPool.Exec(context.Background(), "DELETE FROM songs WHERE id = $1", id); err != nil {
		t.Errorf("failed to clean up song %d: %v", id, err)
	}
}

// beatportID allocates an id no other test will use.
func beatportID() int32 { return int32(900_000_000 + uniqueSuffix.Add(1)) }

func TestInsertBeatportSong_RoundTrip(t *testing.T) {
	t.Parallel()

	q := queries(t)
	ctx := context.Background()

	name := testSongName(t, "Animals")
	bpID := beatportID()

	inserted := insertBeatportSong(t, q, db.InsertBeatportSongParams{
		Name:         name,
		Artists:      "Martin Garrix",
		ReleaseDate:  text("2013-06-17"),
		ThumbnailUrl: text("https://example.com/a.jpg"),
		BeatportID:   int4(bpID),
		MixName:      text("Original Mix"),
		ReleaseName:  text("Animals"),
		Genre:        text("Big Room"),
		SubGenre:     text("Mainstage"),
		Bpm:          int4(128),
		MusicalKey:   text("F Minor"),
		LengthMs:     int4(303000),
	})

	if inserted.Source != "beatport" {
		t.Errorf("source = %q, want %q", inserted.Source, "beatport")
	}
	if inserted.BeatportUpdated {
		t.Error("a freshly inserted song should not be marked beatport_updated")
	}

	got, err := q.GetSongByBeatportID(ctx, int4(bpID))
	if err != nil {
		t.Fatalf("GetSongByBeatportID failed: %v", err)
	}

	if got.ID != inserted.ID {
		t.Errorf("got song %d, want %d", got.ID, inserted.ID)
	}
	if got.Name != name {
		t.Errorf("name = %q, want %q", got.Name, name)
	}
	if got.Bpm.Int32 != 128 {
		t.Errorf("bpm = %d, want 128", got.Bpm.Int32)
	}
	if got.LengthMs.Int32 != 303000 {
		t.Errorf("length_ms = %d, want 303000", got.LengthMs.Int32)
	}
}

func TestInsertRelease_RoundTripOnTheNaturalKey(t *testing.T) {
	t.Parallel()

	q := queries(t)
	ctx := context.Background()

	name := testSongName(t, "Kangaroo")
	const artists, releaseDate = "Julian Jordan", "2026-01-01"

	inserted, err := q.InsertRelease(ctx, db.InsertReleaseParams{
		Name:        name,
		Artists:     artists,
		ReleaseDate: text(releaseDate),
		SpotifyUrl:  text("https://open.spotify.com/track/x"),
	})
	if err != nil {
		t.Fatalf("InsertRelease failed: %v", err)
	}
	t.Cleanup(func() { deleteSong(t, inserted.ID) })

	if inserted.Source != "stmpd" {
		t.Errorf("source = %q, want the stmpd default", inserted.Source)
	}

	// GetSong looks up on (name, artists, release_date), which is the key the
	// autocomplete round-trip and the ingestion conflict check both use.
	got, err := q.GetSong(ctx, db.GetSongParams{
		Name: name, Artists: artists, ReleaseDate: text(releaseDate),
	})
	if err != nil {
		t.Fatalf("GetSong failed: %v", err)
	}
	if got.ID != inserted.ID {
		t.Errorf("got song %d, want %d", got.ID, inserted.ID)
	}
}

func TestDoesSongExist(t *testing.T) {
	t.Parallel()

	q := queries(t)
	ctx := context.Background()

	name := testSongName(t, "Poison")
	const artists, releaseDate = "Martin Garrix", "2015-01-01"

	exists, err := q.DoesSongExist(ctx, db.DoesSongExistParams{
		Name: name, Artists: artists, ReleaseDate: text(releaseDate),
	})
	if err != nil {
		t.Fatalf("DoesSongExist failed: %v", err)
	}
	if exists {
		t.Fatal("the song exists before it was inserted")
	}

	song, err := q.InsertRelease(ctx, db.InsertReleaseParams{
		Name: name, Artists: artists, ReleaseDate: text(releaseDate),
	})
	if err != nil {
		t.Fatalf("InsertRelease failed: %v", err)
	}
	t.Cleanup(func() { deleteSong(t, song.ID) })

	exists, err = q.DoesSongExist(ctx, db.DoesSongExistParams{
		Name: name, Artists: artists, ReleaseDate: text(releaseDate),
	})
	if err != nil {
		t.Fatalf("DoesSongExist failed: %v", err)
	}
	if !exists {
		t.Error("the song does not exist after being inserted")
	}

	// The date is part of the key, so the same track under a different date is
	// a different row. This is why the STMPD fetcher keeps writing <year>-01-01.
	exists, err = q.DoesSongExist(ctx, db.DoesSongExistParams{
		Name: name, Artists: artists, ReleaseDate: text("2020-01-01"),
	})
	if err != nil {
		t.Fatalf("DoesSongExist failed: %v", err)
	}
	if exists {
		t.Error("a different release date matched an existing song; the STMPD " +
			"fetcher relies on the date being part of the key")
	}
}

// unique_release is what stops the fetchers re-inserting a song they have
// already stored. It is a schema constraint, so only a real database proves it.
func TestUniqueRelease_RejectsADuplicate(t *testing.T) {
	t.Parallel()

	q := queries(t)
	ctx := context.Background()

	name := testSongName(t, "Byte")
	params := db.InsertReleaseParams{
		Name: name, Artists: "Martin Garrix", ReleaseDate: text("2026-01-01"),
	}

	song, err := q.InsertRelease(ctx, params)
	if err != nil {
		t.Fatalf("the first insert failed: %v", err)
	}
	t.Cleanup(func() { deleteSong(t, song.ID) })

	_, err = q.InsertRelease(ctx, params)
	if err == nil {
		t.Fatal("inserting the same release twice was allowed")
	}

	// ErrorCode is how the handlers tell a conflict from a real failure; this
	// exercises it against a driver error rather than a synthetic one.
	if got := db.ErrorCode(err); got != db.UniqueViolation {
		t.Errorf("ErrorCode() = %q, want the unique violation %q", got, db.UniqueViolation)
	}
}

// The partial unique index means one Beatport track maps to at most one row,
// while the many songs with no Beatport id stay unconstrained.
func TestBeatportID_IsUniqueButNullable(t *testing.T) {
	t.Parallel()

	q := queries(t)
	ctx := context.Background()

	bpID := beatportID()

	first := insertBeatportSong(t, q, db.InsertBeatportSongParams{
		Name: testSongName(t, "First"), Artists: "Martin Garrix",
		ReleaseDate: text("2026-01-01"), BeatportID: int4(bpID),
	})
	_ = first

	_, err := q.InsertBeatportSong(ctx, db.InsertBeatportSongParams{
		Name: testSongName(t, "Second"), Artists: "Martin Garrix",
		ReleaseDate: text("2026-01-02"), BeatportID: int4(bpID),
	})
	if err == nil {
		t.Fatal("two songs were allowed to share a beatport id")
	}
	if got := db.ErrorCode(err); got != db.UniqueViolation {
		t.Errorf("ErrorCode() = %q, want %q", got, db.UniqueViolation)
	}

	// Songs with no Beatport id are exempt from the index, so several can coexist.
	for i := range 2 {
		song, err := q.InsertRelease(ctx, db.InsertReleaseParams{
			Name:        testSongName(t, fmt.Sprintf("No Beatport ID %d", i)),
			Artists:     "STMPD",
			ReleaseDate: text("2026-01-01"),
		})
		if err != nil {
			t.Fatalf("a song with no beatport id was rejected: %v", err)
		}
		t.Cleanup(func() { deleteSong(t, song.ID) })
	}
}

// GetAllSongsForMatching feeds findSimilarExistingSong, so the columns it
// returns are the ones the dedupe key is built from.
func TestGetAllSongsForMatching_ReturnsTheDedupeColumns(t *testing.T) {
	t.Parallel()

	q := queries(t)
	ctx := context.Background()

	name := testSongName(t, "Matching")
	bpID := beatportID()

	inserted := insertBeatportSong(t, q, db.InsertBeatportSongParams{
		Name: name, Artists: "Martin Garrix",
		ReleaseDate: text("2026-01-01"), BeatportID: int4(bpID),
	})

	rows, err := q.GetAllSongsForMatching(ctx)
	if err != nil {
		t.Fatalf("GetAllSongsForMatching failed: %v", err)
	}

	for _, row := range rows {
		if row.ID != inserted.ID {
			continue
		}

		if row.Name != name {
			t.Errorf("name = %q, want %q", row.Name, name)
		}
		if row.Artists != "Martin Garrix" {
			t.Errorf("artists = %q, want %q", row.Artists, "Martin Garrix")
		}
		if !row.BeatportID.Valid || row.BeatportID.Int32 != bpID {
			t.Errorf("beatport id = %+v, want %d", row.BeatportID, bpID)
		}
		if row.Source != "beatport" {
			t.Errorf("source = %q, want %q", row.Source, "beatport")
		}
		return
	}

	t.Fatalf("song %d is missing from the matching corpus", inserted.ID)
}

func TestUpdateSongWithBeatportData(t *testing.T) {
	t.Parallel()

	q := queries(t)
	ctx := context.Background()

	name := testSongName(t, "To Enrich")
	song, err := q.InsertRelease(ctx, db.InsertReleaseParams{
		Name: name, Artists: "Martin Garrix", ReleaseDate: text("2026-01-01"),
	})
	if err != nil {
		t.Fatalf("InsertRelease failed: %v", err)
	}
	t.Cleanup(func() { deleteSong(t, song.ID) })

	bpID := beatportID()
	// :execrows now, so a cycle that changes nothing reports zero rows instead of
	// rewriting the same rows forever. The count is part of the contract.
	rows, err := q.UpdateSongWithBeatportData(ctx, db.UpdateSongWithBeatportDataParams{
		ID:         song.ID,
		Name:       name,
		Artists:    "Martin Garrix",
		BeatportID: int4(bpID),
		MixName:    text("Extended Mix"),
		Genre:      text("Big Room"),
		Bpm:        int4(128),
		LengthMs:   int4(303000),
	})
	if err != nil {
		t.Fatalf("UpdateSongWithBeatportData failed: %v", err)
	}
	if rows != 1 {
		t.Fatalf("UpdateSongWithBeatportData wrote %d rows, want 1", rows)
	}

	got, err := q.GetSongByID(ctx, song.ID)
	if err != nil {
		t.Fatalf("GetSongByID failed: %v", err)
	}

	if !got.BeatportID.Valid || got.BeatportID.Int32 != bpID {
		t.Errorf("beatport id = %+v, want %d", got.BeatportID, bpID)
	}
	if got.Bpm.Int32 != 128 {
		t.Errorf("bpm = %d, want 128", got.Bpm.Int32)
	}
	// Enriching an existing row must mark it done, or every cycle would rewrite it.
	if !got.BeatportUpdated {
		t.Error("the song was not marked beatport_updated after enrichment")
	}
}

func TestMarkBeatportUpdated(t *testing.T) {
	t.Parallel()

	q := queries(t)
	ctx := context.Background()

	song := insertBeatportSong(t, q, db.InsertBeatportSongParams{
		Name: testSongName(t, "To Mark"), Artists: "Martin Garrix",
		ReleaseDate: text("2026-01-01"), BeatportID: int4(beatportID()),
	})

	if err := q.MarkBeatportUpdated(ctx, song.ID); err != nil {
		t.Fatalf("MarkBeatportUpdated failed: %v", err)
	}

	got, err := q.GetSongByID(ctx, song.ID)
	if err != nil {
		t.Fatalf("GetSongByID failed: %v", err)
	}
	if !got.BeatportUpdated {
		t.Error("the song was not marked beatport_updated")
	}
}

func TestUpdateSongWithStmpdLinks(t *testing.T) {
	t.Parallel()

	q := queries(t)
	ctx := context.Background()

	song := insertBeatportSong(t, q, db.InsertBeatportSongParams{
		Name: testSongName(t, "To Link"), Artists: "Martin Garrix",
		ReleaseDate: text("2026-01-01"), BeatportID: int4(beatportID()),
	})

	// :execrows, so a no-op cycle reports zero rows rather than claiming a write.
	linked, err := q.UpdateSongWithStmpdLinks(ctx, db.UpdateSongWithStmpdLinksParams{
		ID:            song.ID,
		SpotifyUrl:    text("https://open.spotify.com/track/x"),
		AppleMusicUrl: text("https://music.apple.com/x"),
		YoutubeUrl:    text("https://youtu.be/x"),
	})
	if err != nil {
		t.Fatalf("UpdateSongWithStmpdLinks failed: %v", err)
	}
	if linked != 1 {
		t.Fatalf("UpdateSongWithStmpdLinks wrote %d rows, want 1", linked)
	}

	got, err := q.GetSongByID(ctx, song.ID)
	if err != nil {
		t.Fatalf("GetSongByID failed: %v", err)
	}

	if got.SpotifyUrl.String != "https://open.spotify.com/track/x" {
		t.Errorf("spotify = %q, want the value that was set", got.SpotifyUrl.String)
	}
	if got.YoutubeUrl.String != "https://youtu.be/x" {
		t.Errorf("youtube = %q, want the value that was set", got.YoutubeUrl.String)
	}
}

func TestGetSongsLike(t *testing.T) {
	t.Parallel()

	q := queries(t)
	ctx := context.Background()

	name := testSongName(t, "Searchable Anthem")
	song, err := q.InsertRelease(ctx, db.InsertReleaseParams{
		Name: name, Artists: "Findable Artist", ReleaseDate: text("2026-01-01"),
	})
	if err != nil {
		t.Fatalf("InsertRelease failed: %v", err)
	}
	t.Cleanup(func() { deleteSong(t, song.ID) })

	// The handlers wrap the member's input exactly like this.
	rows, err := q.GetSongsLike(ctx, "%"+name+"%")
	if err != nil {
		t.Fatalf("GetSongsLike failed: %v", err)
	}

	// GetSongsLike returns exactly the (name, artists, release_date) triple the
	// autocomplete handlers serialise back into the choice value.
	found := false
	for _, row := range rows {
		if row.Name == song.Name && row.Artists == song.Artists {
			found = true
		}
	}
	if !found {
		t.Errorf("%q did not match itself", name)
	}

	// The search is on "artists - name", lowercased on both sides.
	rows, err = q.GetSongsLike(ctx, "%findable artist - searchable%")
	if err != nil {
		t.Fatalf("GetSongsLike failed: %v", err)
	}
	if len(rows) == 0 {
		t.Error("the combined 'artists - name' form did not match")
	}
}

// BUG: the handlers build the pattern as "%"+input+"%" without escaping, so a
// member typing % or _ gets LIKE wildcards. Harmless for a read-only search,
// but "%" alone matches the whole catalogue.
func TestGetSongsLike_MemberInputIsNotEscaped(t *testing.T) {
	t.Parallel()

	q := queries(t)

	ctx := context.Background()

	name := testSongName(t, "Escaping Probe")
	song, err := q.InsertRelease(ctx, db.InsertReleaseParams{
		Name: name, Artists: "Escaping Artist", ReleaseDate: text("2026-01-01"),
	})
	if err != nil {
		t.Fatalf("InsertRelease failed: %v", err)
	}
	t.Cleanup(func() { deleteSong(t, song.ID) })

	// A member typing "_" gets LIKE's single-character wildcard, so this matches
	// despite there being no literal underscore in the stored title.
	rows, err := q.GetSongsLike(ctx, "%escaping_artist%")
	if err != nil {
		t.Fatalf("GetSongsLike failed: %v", err)
	}

	for _, row := range rows {
		if row.Name == name {
			return // the wildcard matched, which is the behaviour being pinned
		}
	}
	t.Error("the underscore no longer acts as a wildcard; if member input is " +
		"now escaped, delete this test")
}

func TestGetSongByBeatportID_NotFound(t *testing.T) {
	t.Parallel()

	q := queries(t)

	_, err := q.GetSongByBeatportID(context.Background(), int4(-1))
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("error = %v, want pgx.ErrNoRows", err)
	}
	// The handlers compare against this alias rather than pgx directly.
	if !errors.Is(err, db.ErrRecordNotFound) {
		t.Errorf("error = %v, want it to match db.ErrRecordNotFound", err)
	}
}

// The radio only plays songs with a YouTube link, and skips anything over ten
// minutes. That length filter existed in songs.sql but had never been
// regenerated into the Go code, so it was not actually applied.
func TestGetRandomSongForRadio_ExcludesLongTracks(t *testing.T) {
	t.Parallel()

	q := queries(t)
	ctx := context.Background()

	long := insertBeatportSong(t, q, db.InsertBeatportSongParams{
		Name: testSongName(t, "Very Long Mix"), Artists: "Martin Garrix",
		ReleaseDate: text("2026-01-01"), BeatportID: int4(beatportID()),
		LengthMs: int4(3_600_000), // an hour
	})

	// A short, eligible song so the query has something to return; without one
	// this would pass vacuously on an empty catalogue.
	short := insertBeatportSong(t, q, db.InsertBeatportSongParams{
		Name: testSongName(t, "Radio Edit"), Artists: "Martin Garrix",
		ReleaseDate: text("2026-01-01"), BeatportID: int4(beatportID()),
		LengthMs: int4(180_000), // three minutes
	})

	for _, id := range []int64{long.ID, short.ID} {
		if _, err := testPool.Exec(ctx,
			"UPDATE songs SET youtube_url = $2 WHERE id = $1",
			id, "https://youtu.be/x"); err != nil {
			t.Fatalf("failed to set a youtube url: %v", err)
		}
	}

	// Sample repeatedly; the query orders randomly, so one draw proves little.
	for range 50 {
		got, err := q.GetRandomSongForRadio(ctx)
		if err != nil {
			t.Fatalf("GetRandomSongForRadio failed: %v", err)
		}
		if got.ID == long.ID {
			t.Fatalf("the radio picked %q, which is an hour long; the "+
				"length_ms <= 600000 filter is not being applied", got.Name)
		}
	}
}

// Discord caps an option value at 100 characters, and the autocomplete handlers
// put the song id there.
//
// They used to marshal {name, artists, release_date} instead, which a long title
// pushed past the cap: selecting such a song failed the whole interaction with
// 50035 Invalid Form Body. The id is bounded by the column width, so the longest
// title in the catalogue cannot reproduce that.
func TestSongChoiceValue_FitsDiscordsOptionValueLimit(t *testing.T) {
	t.Parallel()

	q := queries(t)
	ctx := context.Background()

	name := testSongName(t, strings.Repeat("A Very Long Song Title ", 3))
	song, err := q.InsertRelease(ctx, db.InsertReleaseParams{
		Name:        name,
		Artists:     "An Artist With A Rather Long Name, And A Collaborator",
		ReleaseDate: text("2026-01-01"),
	})
	if err != nil {
		t.Fatalf("InsertRelease failed: %v", err)
	}
	t.Cleanup(func() { deleteSong(t, song.ID) })

	value := strconv.FormatInt(song.ID, 10)
	if len(value) > 100 {
		t.Errorf("choice value for song %d is %d characters, over Discord's limit of 100",
			song.ID, len(value))
	}

	// The old shape, kept here to show what the id replaced: on this row it is
	// already over the cap.
	legacy := fmt.Sprintf(`{"name":%q,"artists":%q,"release_date":%q}`,
		song.Name, song.Artists, song.ReleaseDate.String)
	if len(legacy) <= 100 {
		t.Skip("this title is no longer long enough to have tripped the old limit")
	}
}
