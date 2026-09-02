package commands

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateLyrics(t *testing.T) {
	t.Parallel()

	t.Run("short lyrics are untouched", func(t *testing.T) {
		t.Parallel()

		lyrics := "You'll never walk alone\nThe dead of the night, out in the cold"
		if got := truncateLyrics(lyrics); got != lyrics {
			t.Errorf("truncateLyrics rewrote lyrics that already fit")
		}
	})

	t.Run("long lyrics are cut to the limit", func(t *testing.T) {
		t.Parallel()

		got := truncateLyrics(strings.Repeat("a", embedDescriptionLimit*2))
		if n := utf8.RuneCountInString(got); n != embedDescriptionLimit {
			t.Errorf("got %d runes, want %d", n, embedDescriptionLimit)
		}
		if !strings.HasSuffix(got, "…") {
			t.Error("a truncated song should say so")
		}
	})

	// The old byte slice could land inside a multi-byte rune, which renders as a
	// replacement character. Every accented title and lyric in the catalogue is a
	// candidate.
	t.Run("a multibyte rune is never split", func(t *testing.T) {
		t.Parallel()

		got := truncateLyrics(strings.Repeat("é", embedDescriptionLimit*2))
		if !utf8.ValidString(got) {
			t.Fatal("truncation produced invalid UTF-8")
		}
		if strings.ContainsRune(got, utf8.RuneError) {
			t.Error("truncation split a rune")
		}
		if n := utf8.RuneCountInString(got); n != embedDescriptionLimit {
			t.Errorf("got %d runes, want %d", n, embedDescriptionLimit)
		}
	})

	// Discord counts characters, not bytes, so a song of exactly the limit in runes
	// must survive whole however many bytes that is.
	t.Run("multibyte lyrics within the limit are not trimmed", func(t *testing.T) {
		t.Parallel()

		lyrics := strings.Repeat("é", embedDescriptionLimit)
		if got := truncateLyrics(lyrics); got != lyrics {
			t.Errorf("trimmed %d runes (%d bytes) that fit the limit",
				utf8.RuneCountInString(lyrics), len(lyrics))
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()

		if got := truncateLyrics(""); got != "" {
			t.Errorf("truncateLyrics(\"\") = %q", got)
		}
	})
}
