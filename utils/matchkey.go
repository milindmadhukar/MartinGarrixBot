package utils

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Matching songs across sources is the whole problem this file exists to solve.
// STMPD writes "Martin Garrix & Ed Sheeran"; Beatport writes the same collaboration
// as "Ed Sheeran, Martin Garrix". A Levenshtein ratio over "<artists> - <name>"
// scores that pair below any usable threshold, because the separator AND the order
// both differ -- which is why 497 beatport rows sat with no streaming links while a
// matching STMPD release was published every fortnight.
//
// The fix is to stop treating the artist string as text and start treating it as a
// set. Both spellings reduce to edsheeran+martingarrix.

// artistSeparators are the ways the two sources join collaborators. Splitting on
// "&" does not require surrounding whitespace: the dataset contains genuinely
// malformed values such as "Martin Garrix &DallasK feat. Sasha Alex Sloan".
var artistSeparators = []string{
	" featuring ", " feat. ", " feat ", " ft. ", " ft ",
	" with ", " pres. ", " presents ", " versus ", " vs. ", " vs ",
	" and ", " x ", " X ", "&", ",", ";", "/", "+",
}

// variantTerms name a rendition of a song rather than a different song. They are
// stripped from the title to produce a base key, so that STMPD's "Catharina" and
// Beatport's "Catharina (Extended Mix)" collapse onto each other.
var variantTerms = []string{
	"original mix", "extended mix", "extended version", "extended",
	"radio edit", "radio mix", "club mix", "dub mix", "vip mix", "vip",
	"instrumental mix", "instrumental", "acoustic version", "acoustic",
	"remix", "edit", "rework", "bootleg", "mix cut", "mixcut",
	"festival edit", "live version",
}

// NormalizeToken lowercases, folds diacritics away and drops everything that is not
// a letter or a digit. Punctuation and spacing are the noisiest difference between
// the two catalogues ("Bad B*tch", "Steppin'", "&friends") and carry no meaning.
func NormalizeToken(s string) string {
	folded, _, err := transform.String(
		transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC),
		s,
	)
	if err != nil {
		folded = s
	}

	var b strings.Builder
	for _, r := range strings.ToLower(folded) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SplitArtists breaks a credit string into its individual artists.
func SplitArtists(s string) []string {
	// Lowercase only for separator detection; the separators are matched
	// case-insensitively but the parts themselves are normalized afterwards anyway.
	work := s
	for _, sep := range artistSeparators {
		work = replaceFold(work, sep, "\x00")
	}

	var out []string
	for _, part := range strings.Split(work, "\x00") {
		if n := NormalizeToken(part); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// replaceFold replaces every case-insensitive occurrence of old with replacement.
func replaceFold(s, old, replacement string) string {
	if old == "" {
		return s
	}

	lowerS, lowerOld := strings.ToLower(s), strings.ToLower(old)

	var b strings.Builder
	for {
		idx := strings.Index(lowerS, lowerOld)
		if idx < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:idx])
		b.WriteString(replacement)
		s, lowerS = s[idx+len(old):], lowerS[idx+len(old):]
	}
}

// ArtistSetKey renders a credit string as an order-independent, spelling-independent
// key: the normalized artists, deduplicated, sorted, joined with "+".
func ArtistSetKey(artists string) string {
	parts := SplitArtists(artists)
	if len(parts) == 0 {
		return ""
	}

	seen := make(map[string]struct{}, len(parts))
	unique := parts[:0]
	for _, p := range parts {
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		unique = append(unique, p)
	}

	sort.Strings(unique)
	return strings.Join(unique, "+")
}

// SplitVariant separates a song's base title from the rendition it names.
//
// The variant can arrive in three places depending on the source: inside the title
// in parentheses ("Catharina (Surf Mesa Extended Remix)"), in the dataset's separate
// version field, or in songs.mix_name from the Beatport API. All three are folded
// into one normalized variant token, and an "original mix" is treated as no variant
// at all so that it matches a source that simply omits it.
func SplitVariant(title, version, mixName string) (base, variant string) {
	base = stripFeature(title)
	extra := ""

	// A trailing parenthesised or bracketed group is a variant only if it actually
	// names one -- "Angels For Each Other (feat. Arijit Singh)" must keep its
	// credit out of the variant slot.
	if open, close := lastGroup(base); open >= 0 {
		inner := base[open+1 : close]
		if isVariantPhrase(inner) {
			extra = inner
			base = strings.TrimSpace(base[:open])
		}
	}

	for _, candidate := range []string{version, mixName, extra} {
		if v := NormalizeToken(candidate); v != "" && v != NormalizeToken("original mix") {
			variant = v
			break
		}
	}

	return NormalizeToken(base), variant
}

// featureMarkers introduce a featured-artist clause in a title.
var featureMarkers = []string{"(feat.", "[feat.", "(ft.", "[ft.", "(featuring",
	" feat. ", " ft. ", " featuring "}

// stripFeature removes a featured-artist clause from a title.
//
// The two sources disagree about where the feature belongs: STMPD files "Set Me Free"
// with artists "Martin Garrix & Arcando feat. Bonn", while beatport files the track as
// "Set Me Free feat. Bonn" with Bonn in the artists as well. Keeping the clause in the
// title makes those two titles differ, and 122 of the rows still missing links after
// the first backfill differed in exactly this way.
//
// Dropping it loses nothing: the featured artist is in the artist set on both sides,
// and the artist set is part of every key, so two genuinely different songs cannot be
// conflated by this alone.
func stripFeature(title string) string {
	lower := strings.ToLower(title)
	cut := -1
	for _, marker := range featureMarkers {
		if i := strings.Index(lower, marker); i >= 0 && (cut < 0 || i < cut) {
			cut = i
		}
	}
	if cut <= 0 {
		return title
	}
	return strings.TrimSpace(title[:cut])
}

// lastGroup returns the index range of the final (...) or [...] group in s.
func lastGroup(s string) (open, close int) {
	for _, pair := range [][2]byte{{'(', ')'}, {'[', ']'}} {
		c := strings.LastIndexByte(s, pair[1])
		if c != len(s)-1 {
			continue
		}
		o := strings.LastIndexByte(s[:c], pair[0])
		if o >= 0 {
			return o, c
		}
	}
	return -1, -1
}

// isVariantPhrase reports whether a parenthesised group names a rendition rather
// than, say, a featured artist.
func isVariantPhrase(s string) bool {
	lower := strings.ToLower(s)
	for _, term := range variantTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

// MatchKey identifies one specific recording: this song, in this rendition, by this
// set of artists.
func MatchKey(title, version, mixName, artists string) string {
	base, variant := SplitVariant(title, version, mixName)
	return ArtistSetKey(artists) + "|" + base + "|" + variant
}

// BaseKey identifies a song irrespective of rendition, so that every remix of
// "Told You So" shares one key with the original.
func BaseKey(title, artists string) string {
	base, _ := SplitVariant(title, "", "")
	return ArtistSetKey(artists) + "|" + base
}

// TitleKey is the base title alone, without the artist set.
//
// Remix grouping cannot key on the artist set the way matching does: beatport credits
// a remix to the original artist plus the remixer, so "Catharina" by Martin Garrix
// and "Catharina" by Martin Garrix & Surf Mesa have different artist sets by
// construction. They are still the same song.
func TitleKey(title, version, mixName string) string {
	base, _ := SplitVariant(title, version, mixName)
	return base
}

// ArtistsSubsume reports whether every artist in inner also appears in outer.
//
// This is the relationship between an original and its remix: the remix's credits are
// the original's plus the remixer. Requiring containment rather than equality is what
// lets a remix find its parent, while still refusing to pair two unrelated songs that
// happen to share a title.
func ArtistsSubsume(outer, inner string) bool {
	outerSet := make(map[string]struct{})
	for _, a := range SplitArtists(outer) {
		outerSet[a] = struct{}{}
	}

	innerParts := SplitArtists(inner)
	if len(innerParts) == 0 {
		return false
	}
	for _, a := range innerParts {
		if _, ok := outerSet[a]; !ok {
			return false
		}
	}
	return true
}
