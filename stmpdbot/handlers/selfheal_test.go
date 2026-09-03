package handlers

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/utils/catalogue"
)

// candidateOf must agree with link-remix-parents about which row is the song, or the
// ticker and the script would fight over the same tree every six hours.
func TestSelfHealElectsTheSameCanonical(t *testing.T) {
	// The Break Through The Silence pair: beatport "Original Mix" with no links versus
	// the STMPD "Radio Edit" carrying the slug, the lyrics and the links.
	beatport := db.GetSongsForAuditRow{
		ID: 15370, Name: "Break Through the Silence",
		Artists: "Matisse, Sadko, Martin Garrix",
		MixName: pgtype.Text{String: "Original Mix", Valid: true},
	}
	stmpd := db.GetSongsForAuditRow{
		ID: 812, Name: "Break Through The Silence",
		Artists:    "Martin Garrix, Matisse & Sadko",
		MixName:    pgtype.Text{String: "Radio Edit", Valid: true},
		StmpdSlug:  pgtype.Text{String: "break-through-the-silence", Valid: true},
		HasLyrics:  true,
		SpotifyUrl: pgtype.Text{String: "https://open.spotify.com/track/x", Valid: true},
	}
	if !catalogue.BetterCanonical(candidateOf(stmpd), candidateOf(beatport)) {
		t.Error("self-heal would elect the linkless beatport row as the song")
	}
	if catalogue.BetterCanonical(candidateOf(beatport), candidateOf(stmpd)) {
		t.Error("candidateOf is not antisymmetric on the pair it exists for")
	}
}
