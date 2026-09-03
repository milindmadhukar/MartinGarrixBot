package utils

import (
	"strings"
	"testing"
)

// matches reports whether every term is a substring of the haystack, which is what
// `haystack LIKE ALL (terms)` does in SQL.
func matches(haystack string, terms []string) bool {
	for _, t := range terms {
		if !strings.Contains(haystack, strings.Trim(t, "%")) {
			return false
		}
	}
	return true
}

// The song this whole change exists for. It is credited to three acts, and a searcher
// naming two of them in the order they remember found nothing at all under the old
// contiguous LIKE -- so the song read as missing from the catalogue.
func TestSearchFindsASongByPartOfItsCredits(t *testing.T) {
	haystack := SearchText("Matisse & Sadko, Aspyer, Matluck", "Don't Tell Me", "", "")

	for _, query := range []string{
		"matisse sadko dont tell me",
		"Matisse & Sadko - Don't Tell Me",
		"dont tell me matisse",
		"matluck tell me",
		"DON'T TELL ME",
	} {
		if !matches(haystack, SearchTerms(query)) {
			t.Errorf("query %q did not find %q", query, haystack)
		}
	}
}

// The catalogue holds both ' and ’ -- five names carry the typographic one -- so a
// searcher had to guess which a row used. Folding drops the apostrophe on both sides,
// which makes the two spellings the same string.
func TestSearchIsBlindToApostropheShape(t *testing.T) {
	curly := SearchText("Matisse & Sadko", "Don’t Tell Me", "", "")
	straight := SearchText("Matisse & Sadko", "Don't Tell Me", "", "")
	if curly != straight {
		t.Fatalf("apostrophe shape changed the haystack:\n curly    = %q\n straight = %q", curly, straight)
	}
	if !matches(curly, SearchTerms("dont tell me")) {
		t.Errorf("plain query did not find the curly-quoted row")
	}
}

// Accents and stroked letters fold the same way match keys do, so a searcher need not
// reproduce them. NormalizeToken already owns these rules; this pins that search uses
// them rather than a second, weaker set.
func TestSearchFoldsAccentsAndStrokes(t *testing.T) {
	for _, tc := range []struct{ stored, typed string }{
		{"Düncan Musique", "duncan"},
		{"NØ SIGNE", "no signe"},
		{"Kølle", "kolle"},
		{"Sigala", "SIGALA"},
	} {
		haystack := SearchText(tc.stored, "Some Song", "", "")
		if !matches(haystack, SearchTerms(tc.typed)) {
			t.Errorf("typing %q did not find %q (folded to %q)", tc.typed, tc.stored, haystack)
		}
	}
}

// An empty query must restrict nothing, and it must do so as an EMPTY slice rather than
// a nil one.
//
// `LIKE ALL` over an empty array is vacuously true and matches every row. But pgx
// encodes a nil slice as SQL NULL, and `LIKE ALL (NULL)` is NULL, which matches none --
// so a nil here turns the unfiltered catalogue page into an empty one. That shipped
// once; this is what catches it.
func TestSearchTermsIsEmptyNotNilWhenThereIsNothingToSearchFor(t *testing.T) {
	for _, q := range []string{"", "   ", "&", "- / -"} {
		terms := SearchTerms(q)
		if terms == nil {
			t.Errorf("SearchTerms(%q) returned nil; pgx sends that as NULL and LIKE ALL (NULL) matches nothing", q)
		}
		if len(terms) != 0 {
			t.Errorf("SearchTerms(%q) = %v, want no terms", q, terms)
		}
	}
}

// The haystack must not depend on which source wrote the row, or the same song found
// by one importer's spelling would be unfindable by the other's.
func TestSearchTextSkipsEmptyParts(t *testing.T) {
	withMix := SearchText("Martin Garrix", "Animals", "", "")
	if strings.Contains(withMix, "  ") {
		t.Errorf("empty parts left a double space: %q", withMix)
	}
	if got, want := SearchText("Martin Garrix", "Animals", "Radio Edit", "Animals EP"),
		"martin garrix animals radio edit animals ep"; got != want {
		t.Errorf("SearchText = %q, want %q", got, want)
	}
}

// A separator between words must split the query, not glue across it.
//
// "escaping_artist" folded to the single term "escapingartist", which is not a
// substring of the stored "escaping artist", so a query containing any punctuation
// between words silently found nothing. An apostrophe is the opposite case: it sits
// inside a word and has to close up, because the haystack stores "dont".
func TestSearchTermsSplitsOnSeparatorsButNotApostrophes(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"escaping_artist", []string{"%escaping%", "%artist%"}},
		{"rock-n-roll", []string{"%rock%", "%n%", "%roll%"}},
		{"martin garrix / dua lipa", []string{"%martin%", "%garrix%", "%dua%", "%lipa%"}},
		{"don't tell me", []string{"%dont%", "%tell%", "%me%"}},
		{"don’t tell me", []string{"%dont%", "%tell%", "%me%"}},
	} {
		got := SearchTerms(tc.query)
		if len(got) != len(tc.want) {
			t.Errorf("SearchTerms(%q) = %v, want %v", tc.query, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("SearchTerms(%q) = %v, want %v", tc.query, got, tc.want)
				break
			}
		}
	}
}
