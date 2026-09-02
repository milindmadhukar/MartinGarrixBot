package utils

import (
	"regexp"
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

// strokeLetters are the letters NFD cannot decompose, because the mark is part of the
// glyph rather than a combining accent. "NØ SIGNE" and "NO SIGNE" are the same act,
// and no amount of Unicode normalisation will tell you that on its own.
var strokeLetters = strings.NewReplacer(
	"ø", "o", "Ø", "o",
	"æ", "ae", "Æ", "ae",
	"œ", "oe", "Œ", "oe",
	"ð", "d", "Ð", "d",
	"đ", "d", "Đ", "d",
	"þ", "th", "Þ", "th",
	"ł", "l", "Ł", "l",
	"ß", "ss",
	"ı", "i", "İ", "i",
)

// NormalizeToken lowercases, folds diacritics away and drops everything that is not
// a letter or a digit. Punctuation and spacing are the noisiest difference between
// the two catalogues ("Bad B*tch", "Steppin'", "&friends") and carry no meaning.
func NormalizeToken(s string) string {
	folded, _, err := transform.String(
		transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC),
		strokeLetters.Replace(s),
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

// disambiguationSuffix matches the country tag beatport appends when two acts share
// a name: "Brooks (NL)", "Carola (BR)", "Jonah (US)". It is not part of the name, and
// keeping it split "Brooks" and "Brooks (NL)" into two different artists -- which in
// turn gave "Boomerang" two rows that never matched each other.
var disambiguationSuffix = regexp.MustCompile(`\s*\([A-Za-z]{2,3}\)\s*$`)

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
		part = disambiguationSuffix.ReplaceAllString(strings.TrimSpace(part), "")
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
	work, extra := splitTrailingVariant(title)
	base = stripFeature(work)

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

// splitFeature separates a title from any featured-artist clause it carries.
//
// The sources disagree about where a feature belongs. STMPD files "Set Me Free" with
// artists "Martin Garrix & Arcando feat. Bonn"; beatport files the same track as
// "Set Me Free feat. Bonn" with Bonn in the artists too. But a third shape exists and
// is the one that matters here: "Love Runs Out (feat. G-Eazy & Sasha Alex Sloan)"
// credited to "Martin Garrix" alone, with the featured artists appearing *only* in
// the title.
//
// So the clause cannot simply be discarded. Dropping it from the title is right --
// otherwise the titles differ -- but its artists have to be folded into the artist
// set, or that row keys as a Martin Garrix solo track and never matches the copies
// that credit all three.
func splitFeature(title string) (base, featured string) {
	lower := strings.ToLower(title)
	cut, markerLen := -1, 0
	for _, marker := range featureMarkers {
		if i := strings.Index(lower, marker); i >= 0 && (cut < 0 || i < cut) {
			cut, markerLen = i, len(marker)
		}
	}
	if cut <= 0 {
		return title, ""
	}

	featured = strings.TrimSpace(title[cut+markerLen:])
	featured = strings.TrimRight(featured, ")]")
	return strings.TrimSpace(title[:cut]), featured
}

// stripFeature returns just the title part, for callers that do not need the credit.
func stripFeature(title string) string {
	base, _ := splitFeature(title)
	return base
}

// splitTrailingVariant peels a rendition off the end of a title, returning the rest
// and the rendition.
//
// This has to happen before the featured-artist clause is removed, not after. The
// feature is stripped by cutting from "feat." to the end of the string, which
// swallows anything following it: "X's feat. Icona Pop (Osrin Remix)" came back as
// plain "X's" with no rendition at all, so the remix looked like the original and a
// remix video was matched to the original's row.
//
// A trailing group counts as a rendition only if it names one. "Angels For Each Other
// (feat. Arijit Singh)" must keep its credit out of the rendition slot.
func splitTrailingVariant(title string) (rest, variant string) {
	if open, close := lastGroup(title); open >= 0 {
		inner := title[open+1 : close]
		if isVariantPhrase(inner) {
			return strings.TrimSpace(title[:open]), inner
		}
	}
	return title, ""
}

// lastGroup returns the index range of the final (...) or [...] group in s.
func lastGroup(s string) (open, close int) {
	// An empty string has no group, and the check below cannot say so: LastIndexByte
	// returns -1, len(s)-1 is also -1, so the "does it end with the closing bracket"
	// test passes and s[:c] slices to -1 and panics.
	if s == "" {
		return -1, -1
	}

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

// creditKey renders the full set of artists on a recording, including any credited
// only inside the title.
func creditKey(title, artists string) string {
	// Peel the rendition off first. Otherwise the feature clause of "X's feat. Icona
	// Pop (Osrin Remix)" reads as "Icona Pop (Osrin Remix)" and "osrinremix" is
	// folded into the artist set as though it were a person.
	work, _ := splitTrailingVariant(title)
	if _, featured := splitFeature(work); featured != "" {
		artists = artists + ", " + featured
	}
	return ArtistSetKey(artists)
}

// MatchKey identifies one specific recording: this song, in this rendition, by this
// set of artists.
func MatchKey(title, version, mixName, artists string) string {
	base, variant := SplitVariant(title, version, mixName)
	return creditKey(title, artists) + "|" + base + "|" + variant
}

// BaseKey identifies a song irrespective of rendition, so that every remix of
// "Told You So" shares one key with the original.
func BaseKey(title, artists string) string {
	base, _ := SplitVariant(title, "", "")
	return creditKey(title, artists) + "|" + base
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

// SameRecording reports whether two (title, artists) pairs describe the same
// recording, strictly enough to trust a search result with.
//
// Base titles must be equal after normalization -- not merely similar. Apple's search
// answers "AREA21 Drinks Up" with "AREA21 - Glad You Came", which shares the artist
// exactly and would sail past any artist-only check; only the title catches it.
//
// Artists need to overlap rather than match exactly, because the two sides routinely
// credit a different subset of the same collaboration: a row filed under "Martin
// Garrix ft. Bono & The Edge" is the same record Apple lists under "Martin Garrix".
func SameRecording(titleA, artistsA, titleB, artistsB string) bool {
	baseA, _ := SplitVariant(titleA, "", "")
	baseB, _ := SplitVariant(titleB, "", "")
	if baseA == "" || baseA != baseB {
		return false
	}

	setA := SplitArtists(artistsA + featureSuffix(titleA))
	setB := SplitArtists(artistsB + featureSuffix(titleB))
	if len(setA) == 0 || len(setB) == 0 {
		return false
	}

	in := make(map[string]struct{}, len(setA))
	for _, a := range setA {
		in[a] = struct{}{}
	}
	for _, b := range setB {
		if _, ok := in[b]; ok {
			return true
		}
	}
	return false
}

func featureSuffix(title string) string {
	work, _ := splitTrailingVariant(title)
	if _, featured := splitFeature(work); featured != "" {
		return ", " + featured
	}
	return ""
}

// defaultRenditions are the ones that mean "the standard release" rather than a
// distinct artistic version. Beatport files nearly every plain release as an
// "Extended Mix"; the STMPD catalogue names no version for the same release. Reading
// that as a disagreement would call 389 correctly-matched rows mismatched.
//
// A named remix, an acoustic version or a radio edit is a different matter: those
// name a specific rendition, and a row carrying one does not belong to a release that
// names none.
var defaultRenditions = map[string]struct{}{
	"":                {},
	"originalmix":     {},
	"extendedmix":     {},
	"extended":        {},
	"extendedversion": {},
	"mixcut":          {},
	"originalversion": {},
}

// IsDefaultRendition reports whether a normalized variant just means "the release
// itself" rather than naming a particular version of it.
func IsDefaultRendition(variant string) bool {
	_, ok := defaultRenditions[variant]
	return ok
}

// RenditionsAgree reports whether two renditions describe the same version of a song,
// treating the house defaults above as "unspecified" on either side.
func RenditionsAgree(a, b string) bool {
	da, dbf := IsDefaultRendition(a), IsDefaultRendition(b)
	if da && dbf {
		return true
	}
	if da != dbf {
		return false
	}
	if a == b {
		return true
	}
	// One elaborating on the other is agreement: beatport writes "Drove Extended
	// Remix" where the catalogue writes "Drove Remix".
	return strings.Contains(a, b) || strings.Contains(b, a)
}

// collectionMarkers name a release that contains songs rather than being one: an EP,
// an album, a remix package, a mixtape. The word-boundary check matters -- "EP" must
// not fire on "Deep", "LP" must not fire on "Help".
var collectionMarkers = []string{"ep", "album", "lp", "remixes", "mixtape", "festival edits"}

// continuousMixMarkers name a recording that is a DJ set rather than a track. A
// 29-minute "Tomorrowland 2016: The Elixir Of Life (Continuous Mix 5)" is a set, and
// nothing in its title says so.
var continuousMixMarkers = []string{"continuous mix", "dj mix", "mixed by", "live set", "mix cut"}

// setLengthMs is the running time past which a recording is a set, not a song. The
// longest genuine track in the catalogue is comfortably under a quarter of an hour.
const setLengthMs = 15 * 60 * 1000

// IsCollection reports whether a row is a release or a DJ set rather than a track,
// using everything the row knows rather than its title alone.
func IsCollection(name, mixName string, lengthMs int32) bool {
	if IsCollectionName(name) {
		return true
	}

	for _, field := range [2]string{mixName, name} {
		lower := strings.ToLower(field)
		for _, marker := range continuousMixMarkers {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}

	if lengthMs > setLengthMs {
		return true
	}

	// A title that names its own rendition is one track, whatever mix_name says.
	// On rows the re-keying pass could not normalise -- because mix_name was already
	// occupied -- mix_name holds the package the track was released on rather than
	// the track's own variant: "Higher Ground (DubVision Remix)" carries mix
	// "Remixes". Reading mix_name alone there turns a remix into the remix EP.
	if _, own := SplitVariant(name, "", ""); own != "" && !IsCollectionName(own) {
		return false
	}

	// Otherwise the rendition counts as much as the title. Re-keying moves a trailing
	// variant out of the name into mix_name, so "Hero (Remixes)" becomes name "Hero"
	// with mix "Remixes" -- the same release, invisible to a rule reading only the
	// title. Reading both means the answer does not depend on whether the row has
	// been through the re-keying pass yet.
	return IsCollectionName(mixName)
}

// IsCollectionName reports whether a title names a release rather than a track.
//
// This has to exist in Go, not only in the migration that first populated the column,
// or every EP published from now on arrives flagged as a song -- and a song is
// something the radio will try to stream end to end.
func IsCollectionName(name string) bool {
	lower := strings.ToLower(name)
	for _, marker := range collectionMarkers {
		if containsWord(lower, marker) {
			return true
		}
	}
	return false
}

// containsWord reports whether lower contains marker delimited by non-alphanumerics,
// so that a marker inside a longer word does not count.
func containsWord(lower, marker string) bool {
	for i := 0; ; {
		j := strings.Index(lower[i:], marker)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(marker)
		beforeOK := start == 0 || !isAlnum(rune(lower[start-1]))
		afterOK := end == len(lower) || !isAlnum(rune(lower[end]))
		if beforeOK && afterOK {
			return true
		}
		i = start + 1
	}
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// SplitTitleRendition separates a stored title from a rendition written into it,
// returning the title unchanged when there is none. Unlike SplitVariant it preserves
// the original spelling rather than normalising, because the result is written back.
func SplitTitleRendition(title string) (base, rendition string) {
	rest, variant := splitTrailingVariant(title)
	if variant == "" {
		return title, ""
	}
	return strings.TrimSpace(rest), strings.TrimSpace(variant)
}

// SplitTitleFeature separates a stored title from a featured-artist clause written
// into it, preserving the original spelling because the result is written back.
func SplitTitleFeature(title string) (base, featured string) {
	rest, variant := splitTrailingVariant(title)
	stripped, credit := splitFeature(rest)
	if credit == "" {
		return title, ""
	}
	if variant != "" {
		stripped = strings.TrimSpace(stripped) + " (" + variant + ")"
	}
	return strings.TrimSpace(stripped), strings.TrimSpace(credit)
}

// stmpdSlugDate matches the date the STMPD catalogue appends to every slug, as in
// "seth-hills-void-ep-2020-8-10".
var stmpdSlugDate = regexp.MustCompile(`-\d{4}-\d{1,2}-\d{1,2}$`)

// StmpdSlugNamesRelease reports whether a catalogue slug names a multi-track release
// that this row is itself named after.
//
// The slug is the most reliable signal the catalogue gives: "Void" the single and
// "Void" the EP are two rows with the same title, the same artist and no rendition to
// tell them apart, so every title-based rule sees one song stored twice. Their slugs
// -- seth-hills-void and seth-hills-void-ep -- say plainly which is which.
//
// The suffix alone is not enough, for the same reason it is not enough on an Apple
// URL: "Mind The Grind" is filed under the bombai-ep slug because it is a track on
// that EP. Only when what remains after stripping the release suffix ends with the
// row's own title is the row the release rather than something on it.
func StmpdSlugNamesRelease(name, slug string) bool {
	if slug == "" || name == "" {
		return false
	}

	trimmed := stmpdSlugDate.ReplaceAllString(strings.ToLower(slug), "")

	stem := ""
	for _, marker := range collectionMarkers {
		suffix := "-" + strings.ReplaceAll(marker, " ", "-")
		if strings.HasSuffix(trimmed, suffix) {
			stem = strings.TrimSuffix(trimmed, suffix)
			break
		}
	}
	if stem == "" {
		return false
	}

	// The stem still carries the artist prefix, so compare on the tail.
	return strings.HasSuffix(NormalizeToken(strings.ReplaceAll(stem, "-", " ")), NormalizeToken(name))
}
