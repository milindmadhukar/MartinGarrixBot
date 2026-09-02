package utils_test

import (
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// link is a non-null URL column value. Named for what it holds rather than for its
// type, because utils/pagination_test.go already has a text helper.
func link(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

// The signature is what decides whether a posted announcement gets edited, so the only
// thing that matters is that it changes exactly when the buttons would.
func TestSongLinkSignature(t *testing.T) {
	t.Parallel()

	base := db.Song{Name: "Breach", SpotifyUrl: link("https://open.spotify.com/track/abc")}

	t.Run("stable across calls", func(t *testing.T) {
		t.Parallel()
		if utils.SongLinkSignature(base) != utils.SongLinkSignature(base) {
			t.Error("the same row produced two different signatures")
		}
	})

	t.Run("a new link changes it", func(t *testing.T) {
		t.Parallel()
		gained := base
		gained.AppleMusicUrl = link("https://music.apple.com/album/1")
		if utils.SongLinkSignature(gained) == utils.SongLinkSignature(base) {
			t.Error("gaining an Apple link left the signature unchanged")
		}
	})

	// A retargeted link leaves the button count identical. resolve-youtube-links
	// replaces playlist URLs with real videos and would otherwise be invisible here.
	t.Run("a redirected link changes it", func(t *testing.T) {
		t.Parallel()
		before := base
		before.YoutubeUrl = link("https://youtube.com/playlist?list=PL1")
		after := base
		after.YoutubeUrl = link("https://youtube.com/watch?v=abc")
		if utils.SongLinkSignature(before) == utils.SongLinkSignature(after) {
			t.Error("changing where a button points left the signature unchanged")
		}
	})

	// Fields that are not buttons must not move it, or the hourly artwork and
	// release-date enrichment would trigger an edit on every announcement it touches.
	t.Run("non-link fields do not change it", func(t *testing.T) {
		t.Parallel()
		enriched := base
		enriched.ThumbnailUrl = link("https://example.invalid/cover.jpg")
		enriched.ReleaseDate = link("2026-08-26")
		enriched.Bpm = pgtype.Int4{Int32: 128, Valid: true}
		if utils.SongLinkSignature(enriched) != utils.SongLinkSignature(base) {
			t.Error("enriching artwork or metadata changed the button signature")
		}
	})

	t.Run("a song with no links has an empty signature", func(t *testing.T) {
		t.Parallel()
		if got := utils.SongLinkSignature(db.Song{Name: "Unreleased"}); got != "" {
			t.Errorf("signature = %q, want empty", got)
		}
	})

	// The signature must describe the buttons that are actually rendered, or the very
	// first refresh would "correct" a message that was already right.
	t.Run("agrees with the rendered buttons", func(t *testing.T) {
		t.Parallel()
		song := db.Song{
			Name:            "Breach",
			SpotifyUrl:      link("https://open.spotify.com/track/abc"),
			YoutubeUrl:      link("https://youtube.com/watch?v=abc"),
			AppleMusicUrl:   link("https://music.apple.com/album/1"),
			DeezerUrl:       link("https://deezer.com/track/1"),
			TidalUrl:        link("https://tidal.com/track/1"),
			AmazonMusicUrl:  link("https://music.amazon.com/albums/1"),
			YoutubeMusicUrl: link("https://music.youtube.com/watch?v=abc"),
		}

		lines := strings.Count(utils.SongLinkSignature(song), "\n") + 1
		if got := len(utils.GetSongButtons(song)); got != lines {
			t.Errorf("%d buttons rendered but the signature names %d links", got, lines)
		}
	})
}

// The beatport announcement leads with the beatport link. It used to do that by
// prepending one unconditionally, which produced two identical Beatport buttons on
// every announcement, because a beatport-sourced row already renders one.
func TestLeadWith(t *testing.T) {
	t.Parallel()

	song := db.Song{
		Name:         "Breach",
		SpotifyUrl:   link("https://open.spotify.com/track/abc"),
		BeatportID:   pgtype.Int4{Int32: 42, Valid: true},
		BeatportSlug: link("breach"),
	}

	buttons := utils.GetSongButtons(song)
	before := len(buttons)

	got := utils.LeadWith(buttons, "Beatport", utils.BeatportButton("https://example.invalid"))

	if len(got) != before {
		t.Errorf("LeadWith changed the button count from %d to %d", before, len(got))
	}
	if label := labelOf(t, got[0]); label != "Beatport" {
		t.Errorf("first button is %q, want Beatport", label)
	}

	seen := 0
	for _, b := range got {
		if labelOf(t, b) == "Beatport" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("found %d Beatport buttons, want exactly 1", seen)
	}
}

// When the row itself cannot render the button, the fallback still has to appear --
// otherwise a beatport announcement for a row with no slug loses its own link.
func TestLeadWith_AddsTheFallbackWhenAbsent(t *testing.T) {
	t.Parallel()

	song := db.Song{Name: "Breach", SpotifyUrl: link("https://open.spotify.com/track/abc")}
	buttons := utils.GetSongButtons(song)

	got := utils.LeadWith(buttons, "Beatport", utils.BeatportButton("https://beatport.invalid/track/breach/42"))

	if len(got) != len(buttons)+1 {
		t.Errorf("got %d buttons, want %d", len(got), len(buttons)+1)
	}
	if label := labelOf(t, got[0]); label != "Beatport" {
		t.Errorf("first button is %q, want Beatport", label)
	}
}

// LeadWith must not reorder anything but the button it moves.
func TestLeadWith_PreservesTheRestOfTheOrder(t *testing.T) {
	t.Parallel()

	song := db.Song{
		Name:          "Breach",
		SpotifyUrl:    link("https://open.spotify.com/track/abc"),
		YoutubeUrl:    link("https://youtube.com/watch?v=abc"),
		AppleMusicUrl: link("https://music.apple.com/album/1"),
		BeatportID:    pgtype.Int4{Int32: 42, Valid: true},
		BeatportSlug:  link("breach"),
		DeezerUrl:     link("https://deezer.com/track/1"),
	}

	got := utils.LeadWith(utils.GetSongButtons(song), "Beatport",
		utils.BeatportButton("https://example.invalid"))

	want := []string{"Beatport", "Spotify", "Youtube", "Apple Music", "Deezer"}
	if len(got) != len(want) {
		t.Fatalf("got %d buttons, want %d", len(got), len(want))
	}
	for i, label := range want {
		if got := labelOf(t, got[i]); got != label {
			t.Errorf("button %d is %q, want %q", i, got, label)
		}
	}
}

func labelOf(t *testing.T, c discord.InteractiveComponent) string {
	t.Helper()

	button, ok := c.(discord.ButtonComponent)
	if !ok {
		t.Fatalf("component %T is not a button", c)
	}
	return button.Label
}
