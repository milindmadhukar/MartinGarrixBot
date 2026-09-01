package utils

import (
	"fmt"
	"strings"
)

// SongTitleWithRendition names a song the way a listener would ask for it, keeping
// the rendition when there is one: "Hero (Space Ducks Extended Remix)".
//
// Only "Original Mix" and its synonyms are dropped. They are Beatport's way of
// writing "the plain song", so printing them adds nothing. Everything else names a
// genuinely different recording and must survive: an extended mix and its original
// are two rows, two lengths and two sets of links, and rendering both as plain
// "Animals" is what made the catalogue look full of duplicates.
func SongTitleWithRendition(name, mix string) string {
	mix = strings.TrimSpace(mix)
	if mix == "" || isPlainRendition(mix) {
		return name
	}

	// Rows the re-keying pass could not normalise still carry the rendition in the
	// title -- "Chills (Orchestral Version)" with mix "Orchestral Version" -- and
	// appending it again reads as a stutter.
	if strings.Contains(strings.ToLower(name), strings.ToLower(mix)) {
		return name
	}

	return fmt.Sprintf("%s (%s)", name, mix)
}

// SongHeading is the "<artists> - <song>" line used as an embed title.
func SongHeading(artists, name, mix string) string {
	return fmt.Sprintf("%s - %s", artists, SongTitleWithRendition(name, mix))
}

func isPlainRendition(mix string) bool {
	switch strings.ToLower(strings.TrimSpace(mix)) {
	case "original mix", "original version", "original":
		return true
	}
	return false
}
