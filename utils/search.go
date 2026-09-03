// Search folding for the catalogue.
//
// The old search was a single contiguous LIKE over `artists || ' - ' || name`, which
// cannot find a song whose credits the searcher does not reproduce verbatim and in
// order. "Don't Tell Me" is stored against "Matisse & Sadko, Aspyer, Matluck"; typing
// "matisse sadko dont tell me" matched nothing, and the song read as missing from the
// catalogue entirely. The apostrophe made it worse: the table holds both ' and ’, so
// even a searcher who typed the credits exactly had to guess which one the row used.
//
// The fix is to fold both sides of the comparison the same way and require every term
// separately rather than the whole phrase contiguously.
package utils

import (
	"strings"
	"unicode"
)

// SearchText is the folded haystack a catalogue row is found by, stored in
// songs.search_text.
//
// Release name is included because it is how someone looks for a track by the EP it
// came on, which is often all they remember.
func SearchText(artists, name, mixName, releaseName string) string {
	parts := make([]string, 0, 4)
	for _, s := range []string{artists, name, mixName, releaseName} {
		if folded := NormalizeWords(s); folded != "" {
			parts = append(parts, folded)
		}
	}
	return strings.Join(parts, " ")
}

// SearchTerms folds a typed query into one LIKE pattern per term.
//
// Every term must appear somewhere in the haystack, in any order -- which is what makes
// a partial credit string ("matisse sadko") find a row crediting more people than that.
//
// A query with nothing searchable in it returns an empty slice, never nil, and the
// difference is load-bearing: `LIKE ALL` over an empty array is vacuously true and
// matches every row, which is what "no search" should mean -- but pgx encodes a nil
// slice as SQL NULL, and `LIKE ALL (NULL)` is NULL, which matches nothing. An unfiltered
// catalogue page came back empty for exactly that reason.
func SearchTerms(query string) []string {
	fields := strings.Fields(NormalizeWords(splitPunctuation(query)))
	terms := make([]string, 0, len(fields))
	for _, f := range fields {
		terms = append(terms, "%"+f+"%")
	}
	return terms
}

// splitPunctuation prepares a typed query for folding by dropping apostrophes and
// turning every other separator into a space.
//
// The two are treated differently on purpose. An apostrophe is inside a word -- "Don't"
// is one word and has to fold to "dont", matching how the haystack stores it. A hyphen,
// underscore or slash is between words, and gluing across one produces a term that
// matches nothing: "escaping_artist" became "escapingartist", which is not a substring
// of the stored "escaping artist".
//
// Only the query is split this way, never the haystack. Splitting a term into more,
// smaller terms can only make a match more likely, since every term is a substring
// test; splitting the haystack would break the reverse case, where someone types
// "rocknroll" for a row stored as "Rock-N-Roll".
func splitPunctuation(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\'' || r == '\u2019' || r == '\u2018' || r == '`':
			// dropped, so the word closes up
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r):
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return b.String()
}
