// Package catalogue holds the rules that say what a well-formed songs table looks like:
// which row of several is the one a listener means, and which invariants a row can
// break.
//
// It exists so that the maintenance scripts and the dashboard cannot disagree. Both
// answer the same two questions -- "which row is canonical" and "what is wrong with
// this catalogue" -- and both used to answer them in their own code. That is how the
// two canonical-election rules in scripts/ drifted apart, and it is why a beatport row
// with no streaming links was elected over the STMPD row for the same song.
package catalogue

// Candidate is a song row reduced to what decides whether it is the row a listener
// means when they name the song.
type Candidate struct {
	ID int64

	// IsCollection marks an EP, album or remix pack. A release is never a song.
	IsCollection bool

	// NamesRendition is true when the row names a rendition of a song rather than the
	// song: "Radio Edit", "DubVision Remix". Note that "Original Mix" does not count --
	// it is beatport's word for the plain recording, and SplitVariant treats it as no
	// rendition at all.
	NamesRendition bool

	// HasSlug is the STMPD catalogue's own identifier: the strongest provenance a row
	// can carry, because it means the label itself published this row.
	HasSlug bool

	// HasLyrics means hand-entered or verified words, which exist nowhere else.
	HasLyrics bool

	// HasLinks means at least one streaming service can be linked to.
	HasLinks bool

	// ArtistCount is how many acts are credited. A remix credits the remixer on top of
	// the original artists, so more credits is weak evidence of a rendition.
	ArtistCount int

	// ReleaseDate is "YYYY-MM-DD", or "" when unknown.
	ReleaseDate string
}

// BetterCanonical reports whether a is a better canonical row than b.
//
// Provenance is weighed before shape, and that ordering is the whole point of this
// function. The canonical row is the only one users ever see -- /links autocomplete,
// /lyrics, /quiz and the radio all filter on parent_song_id IS NULL -- so it has to be
// the row carrying the label's own identifier, the lyrics and the streaming links.
//
// Ranking "names no rendition" above those is exactly the bug this replaces. It elected
// a beatport "Original Mix" with no links over an STMPD "Radio Edit" carrying YouTube,
// Spotify and lyrics, because "Original Mix" reads as no rendition and so won before
// links were ever compared. Every listener asking for that song got a card with no
// buttons on it, and the row that had them was filed underneath, invisible.
func BetterCanonical(a, b Candidate) bool {
	// A release is not a song, so it can never be the entry its own tracks hang off.
	if a.IsCollection != b.IsCollection {
		return !a.IsCollection
	}
	if a.HasSlug != b.HasSlug {
		return a.HasSlug
	}
	if a.HasLyrics != b.HasLyrics {
		return a.HasLyrics
	}
	if a.HasLinks != b.HasLinks {
		return a.HasLinks
	}
	if a.NamesRendition != b.NamesRendition {
		return !a.NamesRendition
	}
	if a.ArtistCount != b.ArtistCount {
		return a.ArtistCount < b.ArtistCount
	}
	// A known date beats an absent one: without this a row with no date sorts as the
	// earliest of all and wins the "earliest release" rule below, so an unreleased row
	// would become the canonical entry for a song that has actually come out.
	if (a.ReleaseDate != "") != (b.ReleaseDate != "") {
		return a.ReleaseDate != ""
	}
	if a.ReleaseDate != b.ReleaseDate {
		return a.ReleaseDate < b.ReleaseDate
	}
	// Ties break on id so the choice is stable across runs.
	return a.ID < b.ID
}

// LockableFields is every songs column an owner may correct by hand, and therefore
// every value that may appear in songs.locked_fields.
//
// The allowlist is enforced in Go rather than by a CHECK constraint so that making a
// column editable is not a migration. It deliberately excludes the derived columns --
// match_key, base_key, search_text, normalized_name -- which are recomputed from the
// editable ones and would only ever be pinned by mistake.
func LockableFields() []string {
	return []string{
		"name", "artists", "mix_name",
		"thumbnail_url",
		"spotify_url", "apple_music_url", "youtube_url", "youtube_music_url",
		"deezer_url", "tidal_url", "amazon_music_url", "beatport_url",
		"release_date", "release_name",
		"lyrics", "genre", "sub_genre", "bpm", "musical_key",
		"is_collection", "is_instrumental", "is_unreleased",
		"parent_song_id",
	}
}

// IsLockable reports whether a field name may be locked.
func IsLockable(field string) bool {
	for _, f := range LockableFields() {
		if f == field {
			return true
		}
	}
	return false
}
