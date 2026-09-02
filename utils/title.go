package utils

import (
	"regexp"
	"strings"

	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

// A stored songs.name is not the answer to "what is this song called". It carries
// whatever the source that supplied it wrote down, and the sources write down three
// different kinds of thing after the title:
//
//	Drown (Alle Farben Remix)                     a rendition
//	Sun Is Never Going Down (feat. Dawn Golden)   a featured-artist credit
//	Breach (Walk Alone)                           a second title
//
// matchkey.go already knows the first two, because cross-source identity depends on
// them. The third has never been recognised anywhere, and it is what made the quiz
// reject "Breach" for a song called "Breach (Walk Alone)" -- 0.40 against a 0.6
// threshold. This file names all three so that the quiz can ask about the song rather
// than about the spelling, and so that songs.normalized_name has something to hold.
//
// Everything here preserves the original spelling. These strings are shown to people
// and compared against what people type; they are not keys.

// TitleParts is a stored name taken apart into the pieces a listener would and would
// not name.
type TitleParts struct {
	Base      string // "Breach", "Starlight", "Howling (Pt. II)"
	Subtitle  string // "Walk Alone" -- empty when there is none
	Featured  string // "Dawn Golden" -- empty when there is none
	Rendition string // "Alle Farben Remix" -- empty when there is none
}

// titleCreditPrefixes introduce a featured-artist credit inside a parenthesised group.
//
// Deliberately a separate list from featureMarkers rather than an extension of it.
// featureMarkers feeds splitFeature, which feeds creditKey, which feeds every
// match_key and base_key in the database -- adding "with" there would silently
// rewrite all of them and require a re-keying pass plus a dedupe review. This list is
// read only by code that produces human-facing answers, so it can afford to be wider.
var titleCreditPrefixes = []string{
	"feat.", "feat ", "featuring ", "ft.", "ft ",
	"with ", "w/ ", "pres.", "presents ", "vs.", "vs ",
}

// partNumberPattern matches a group that numbers a title rather than describing it.
//
// A part number is not a droppable subtitle. "Howling" and "Howling (Pt. II)" are two
// separate rows for two separate songs, and reducing the second to "Howling" would
// both collide with the first in normalized_name and credit a player for naming the
// wrong song. It stays glued to the base title.
var partNumberPattern = regexp.MustCompile(`(?i)^(pt\.?|part|vol\.?|volume|chapter|ch\.?|act|side)\s*([0-9]+|[ivxlcdm]+|[a-z])$`)

// quoteRunes are the ways a source can wrap a credit it wrote into the title, as in
// `Now that I've Found You Feat. "John & Michel"`.
const quoteRunes = "\"'‘’“”«»„‟`"

// trimCredit strips the quotes around a credit.
//
// Scoped to the credit and never applied to the title: an apostrophe in a title is
// load-bearing ("Rockin'", "It's Alright") and trimming it would corrupt the answer.
func trimCredit(s string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(s), quoteRunes))
}

// groupKind names what a trailing (...) or [...] group is.
type groupKind int

const (
	groupSubtitle groupKind = iota
	groupRendition
	groupFeature
	groupPart
)

// classifyGroup decides which of the three things a trailing group is.
//
// Order matters. A collection marker has to be read before the part check or
// "(Remixes Vol. 1)" is scored as a part number rather than a rendition.
//
// The subtitle case is last and is the only one that produces an accepted answer, so
// the bias runs the safe way: reading a subtitle as a credit merely shrinks the
// accepted set, and the player types the full title instead. Reading a credit as a
// subtitle puts "Dawn Golden" among the correct answers and pays coins out for naming
// a guest vocalist.
func classifyGroup(inner string) groupKind {
	if IsCollectionName(inner) || isVariantPhrase(inner) {
		return groupRendition
	}
	if partNumberPattern.MatchString(strings.TrimSpace(inner)) {
		return groupPart
	}

	lower := strings.ToLower(strings.TrimSpace(inner))
	for _, prefix := range titleCreditPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return groupFeature
		}
	}

	// A group too short to be a title is not one. Four characters is the floor
	// because a subtitle becomes an accepted quiz answer in its own right, and
	// "(Not)" from "It's Alright (Not)" is not a name anybody would guess -- it is
	// three letters that half the English language is one edit away from. The base
	// title still answers for those rows.
	//
	// The pattern check catches the country tag beatport appends to a disambiguated
	// artist name: "(NL)", "(US)".
	if len(NormalizeToken(inner)) < 4 || disambiguationSuffix.MatchString("("+inner+")") {
		return groupRendition
	}

	return groupSubtitle
}

// maxTitleGroups bounds the peel loop. Nothing in the catalogue nests deeper than
// "Starlight (Keep Me Afloat) feat. Shaun Farrugia", and a bound means a pathological
// name cannot spin.
const maxTitleGroups = 4

// SplitTitleParts decomposes a stored songs.name.
//
// Each iteration checks the parenthesised group *before* the bare credit, and that
// order is the whole trick. splitFeature cuts from its marker to the end of the
// string, so on "Now That I've Found You feat. John & Michel (Extended Mix)" it would
// swallow the rendition along with the credit -- the same failure splitTrailingVariant
// documents. Taking the group off first means the credit is only ever looked for in
// what is left.
func SplitTitleParts(name string) TitleParts {
	var p TitleParts
	work := strings.TrimSpace(name)

peel:
	for range maxTitleGroups {
		if open, closeIdx := lastGroup(work); open > 0 {
			inner := strings.TrimSpace(work[open+1 : closeIdx])
			switch classifyGroup(inner) {
			case groupRendition:
				if p.Rendition == "" {
					p.Rendition = inner
				}
				work = strings.TrimSpace(work[:open])
				continue
			case groupFeature:
				p.Featured = joinCredit(p.Featured, trimCredit(stripCreditPrefix(inner)))
				work = strings.TrimSpace(work[:open])
				continue
			case groupPart:
				break peel
			case groupSubtitle:
				if p.Subtitle != "" {
					break peel // one subtitle is all a title gets
				}
				p.Subtitle = inner
				work = strings.TrimSpace(work[:open])
				continue
			}
		}

		if rest, credit := splitFeature(work); credit != "" {
			p.Featured = joinCredit(p.Featured, trimCredit(credit))
			work = strings.TrimSpace(rest)
			continue
		}

		break
	}

	p.Base = strings.TrimSpace(work)
	// A name that was nothing but a group leaves nothing behind. Keep it whole rather
	// than reducing a real song to the empty string.
	if p.Base == "" {
		p.Base = strings.TrimSpace(name)
		p.Subtitle = ""
	}
	return p
}

// stripCreditPrefix removes the "feat." / "with" marker from a credit group, leaving
// the names.
func stripCreditPrefix(inner string) string {
	trimmed := strings.TrimSpace(inner)
	lower := strings.ToLower(trimmed)
	for _, prefix := range titleCreditPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(trimmed[len(prefix):])
		}
	}
	return trimmed
}

// joinCredit accumulates credits found in more than one place on the same title.
func joinCredit(existing, add string) string {
	switch {
	case add == "":
		return existing
	case existing == "":
		return add
	default:
		return existing + ", " + add
	}
}

// NormalizedTitle is the answerable form of a stored name: the title with any
// rendition and any featured-artist credit removed.
//
//	Breach (Walk Alone)                           -> Breach
//	Sun Is Never Going Down (feat. Dawn Golden)   -> Sun Is Never Going Down
//	Now that I've Found You Feat. "John & Michel" -> Now that I've Found You
//	Howling (Pt. II)                              -> Howling (Pt. II)
func NormalizedTitle(name string) string {
	return SplitTitleParts(name).Base
}

// AcceptedTitles returns every spelling of a stored name that a quiz answer may take:
// the name as stored, its base title, and its subtitle. A rendition and a
// featured-artist credit are deliberately absent -- naming the guest vocalist on a
// song is not naming the song.
func AcceptedTitles(name string) []string {
	p := SplitTitleParts(name)
	return dedupeTitles([]string{strings.TrimSpace(name), p.Base, p.Subtitle})
}

// SongAnswers is AcceptedTitles for a stored row, preferring the normalized_name the
// keying pass wrote.
//
// The column is an override with a computed default, the same shape SongIndex.add
// uses for match_key: a row the keying pass has not reached, or one whose name was
// rewritten by SQL, still answers correctly instead of answering wrongly.
func SongAnswers(song db.Song) []string {
	p := SplitTitleParts(song.Name)
	return dedupeTitles([]string{strings.TrimSpace(song.Name), songBase(song, p), p.Subtitle})
}

// songBase is the row's base title: the stored override when there is one, otherwise
// what the decomposition derived.
func songBase(song db.Song, p TitleParts) string {
	if song.NormalizedName.Valid && strings.TrimSpace(song.NormalizedName.String) != "" {
		return strings.TrimSpace(song.NormalizedName.String)
	}
	return p.Base
}

// dedupeTitles drops empties and forms that normalize to something already present,
// preserving order.
func dedupeTitles(candidates []string) []string {
	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		key := NormalizeToken(c)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	return out
}

// QuizThreshold is the similarity a guess needs against a fuzzily matched title. It is
// the value the quiz has always used, kept so the behaviour players are used to on
// ordinary titles does not change.
const QuizThreshold = 0.6

// GuessMatchesSong reports whether what a member typed names this song.
//
// The stored name and its base title are matched fuzzily at QuizThreshold, exactly as
// before. A subtitle has to be exact after normalization: subtitles are short,
// ordinary words -- "Not" from "It's Alright (Not)", "Tasty" from "Melt (Tasty)" --
// and a 0.6 ratio over three characters buys a whole free edit, so "no" and "nasty"
// would be paid out as correct answers.
func GuessMatchesSong(song db.Song, guess string) bool {
	if NormalizeToken(guess) == "" {
		return false
	}

	p := SplitTitleParts(song.Name)
	for _, form := range [2]string{song.Name, songBase(song, p)} {
		if strings.TrimSpace(form) != "" && IsCloseMatch(form, guess, QuizThreshold) {
			return true
		}
	}

	return p.Subtitle != "" && NormalizeToken(p.Subtitle) == NormalizeToken(guess)
}

// TitleAppearsIn reports whether a lyric line gives a title away.
//
// Word boundaries matter here more than anywhere else in this file. A naive substring
// test on the subtitle of "It's Alright (Not)" hides every line containing "nothing"
// or "cannot", which is most of a song.
func TitleAppearsIn(line, title string) bool {
	t := normalizeWords(title)
	if t == "" {
		return false
	}
	return containsWord(normalizeWords(line), t)
}

// normalizeWords is NormalizeToken with the word breaks kept, so that a title can be
// looked for inside a sentence.
func normalizeWords(s string) string {
	var b strings.Builder
	for _, word := range strings.Fields(s) {
		if n := NormalizeToken(word); n != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(n)
		}
	}
	return b.String()
}
