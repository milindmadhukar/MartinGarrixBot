package utils

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

// A named remix is its own recording. It shares a base key and an artist set with the
// original, so every tier that reasons about the song rather than the recording will
// offer the original as a candidate -- and the original is normally already claimed by
// some other beatport track id, which is the condition the "already represented"
// fallback was written for.
//
// Reporting the original as the remix's home meant the remix never got inserted:
// Beatport lists thirty renditions of "Scared To Be Lonely" and the table held four
// rows, so twenty-six recordings the bot is supposed to serve links for were
// unreachable and unsearchable.
func TestRemixIsNotRepresentedByItsOriginal(t *testing.T) {
	original := db.GetAllSongsForMatchingRow{
		ID:      1,
		Name:    "Scared To Be Lonely",
		Artists: "Martin Garrix, Dua Lipa",
		// Claimed by a different beatport track than any query below.
		BeatportID: pgtype.Int4{Int32: 9216656, Valid: true},
		MatchKey:   text(MatchKey("Scared To Be Lonely", "", "", "Martin Garrix, Dua Lipa")),
		BaseKey:    text(BaseKey("Scared To Be Lonely", "Martin Garrix, Dua Lipa")),
	}
	ix := NewSongIndex([]db.GetAllSongsForMatchingRow{original})

	remix := SongQuery{
		Title:      "Scared To Be Lonely",
		MixName:    "DubVision Remix",
		Artists:    "Martin Garrix, Dua Lipa",
		BeatportID: pgtype.Int4{Int32: 9092133, Valid: true},
	}
	if got, tier := ix.Lookup(remix); got != nil {
		t.Errorf("remix resolved to #%d (%q) at tier %s; want no match so it gets its own row",
			got.ID, got.Name, tier)
	}

	// The original arriving under a different track id is genuinely the same
	// recording, and must still be recognised rather than duplicated.
	same := SongQuery{
		Title:      "Scared to Be Lonely",
		MixName:    "Original Mix",
		Artists:    "Martin Garrix, Dua Lipa",
		BeatportID: pgtype.Int4{Int32: 29179374, Valid: true},
	}
	got, tier := ix.Lookup(same)
	if got == nil {
		t.Fatalf("the original re-listed under a new track id was not recognised")
	}
	if got.ID != 1 {
		t.Errorf("matched #%d at tier %s; want #1", got.ID, tier)
	}
}
