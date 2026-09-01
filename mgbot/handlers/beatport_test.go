package handlers

// In-package: findSimilarExistingSong is unexported. It takes a plain slice, so
// no database is involved.

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

func song(id int64, artists, name string) db.GetAllSongsForMatchingRow {
	return db.GetAllSongsForMatchingRow{ID: id, Artists: artists, Name: name}
}

func TestFindSimilarExistingSong(t *testing.T) {
	t.Parallel()

	catalogue := []db.GetAllSongsForMatchingRow{
		song(1, "Martin Garrix", "Poison"),
		song(2, "Julian Jordan", "Kangaroo"),
		song(3, "Mesto", "Won't Let You Go"),
	}

	tests := []struct {
		name         string
		trackName    string
		trackArtists string
		wantID       int64 // 0 means no match expected
	}{
		{
			name:         "an exact match is found",
			trackName:    "Kangaroo",
			trackArtists: "Julian Jordan",
			wantID:       2,
		},
		{
			name:         "case and punctuation differences still match",
			trackName:    "wont let you go",
			trackArtists: "MESTO",
			wantID:       3,
		},
		{
			name:         "an unrelated track does not match",
			trackName:    "Animals",
			trackArtists: "Martin Garrix",
			wantID:       0,
		},
		{
			name:         "a different artist does not match",
			trackName:    "Kangaroo",
			trackArtists: "Someone Else",
			wantID:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := findSimilarExistingSong(catalogue, tt.trackName, tt.trackArtists)

			if tt.wantID == 0 {
				if got != nil {
					t.Errorf("got a match on song %d, want none", got.ID)
				}
				return
			}

			if got == nil {
				t.Fatalf("got no match, want song %d", tt.wantID)
			}
			if got.ID != tt.wantID {
				t.Errorf("matched song %d, want %d", got.ID, tt.wantID)
			}
		})
	}
}

func TestFindSimilarExistingSong_EmptyCatalogue(t *testing.T) {
	t.Parallel()

	for _, catalogue := range [][]db.GetAllSongsForMatchingRow{nil, {}} {
		if got := findSimilarExistingSong(catalogue, "Animals", "Martin Garrix"); got != nil {
			t.Errorf("got a match against an empty catalogue: %+v", got)
		}
	}
}

// The first match wins, so ordering decides which row an incoming track is
// merged into when several are close enough.
func TestFindSimilarExistingSong_ReturnsTheFirstMatch(t *testing.T) {
	t.Parallel()

	catalogue := []db.GetAllSongsForMatchingRow{
		song(10, "Martin Garrix", "Poison"),
		song(11, "Martin Garrix", "Poison"),
	}

	got := findSimilarExistingSong(catalogue, "Poison", "Martin Garrix")
	if got == nil {
		t.Fatal("expected a match")
	}
	if got.ID != 10 {
		t.Errorf("matched song %d, want the first one (10)", got.ID)
	}
}

// The result points into the caller's slice rather than being a copy, so
// mutating it mutates the catalogue. GetBeatportReleases relies on this when it
// appends newly inserted songs to the in-memory slice mid-cycle.
func TestFindSimilarExistingSong_AliasesTheInputSlice(t *testing.T) {
	t.Parallel()

	catalogue := []db.GetAllSongsForMatchingRow{song(1, "Martin Garrix", "Poison")}

	got := findSimilarExistingSong(catalogue, "Poison", "Martin Garrix")
	if got == nil {
		t.Fatal("expected a match")
	}

	got.BeatportID = pgtype.Int4{Int32: 999, Valid: true}

	if !catalogue[0].BeatportID.Valid || catalogue[0].BeatportID.Int32 != 999 {
		t.Error("the returned pointer no longer aliases the input slice; " +
			"GetBeatportReleases depends on that")
	}
}

// BUG: a Beatport title carrying a mix suffix scores around 0.63 against the
// STMPD title for the same track, well under the 0.85 threshold, so it is
// treated as a new song. This is the most common shape of the very duplicate
// this function exists to catch. Pinned rather than fixed because changing the
// comparison re-partitions rows already stored.
func TestFindSimilarExistingSong_MissesMixSuffixVariants(t *testing.T) {
	t.Parallel()

	catalogue := []db.GetAllSongsForMatchingRow{song(1, "Martin Garrix", "Animals")}

	got := findSimilarExistingSong(catalogue, "Animals (Original Mix)", "Martin Garrix")
	if got != nil {
		t.Error("mix-suffix variants now dedupe; unskip the paired test in " +
			"utils/similarity_test.go and delete this one")
	}
}
