package catalogue

import "testing"

// The defect this comparator was rewritten for.
//
// Production had #15370 -- a beatport listing of "Break Through the Silence", mix name
// "Original Mix", crediting three acts, carrying no streaming links at all -- elected
// as the canonical row, with #812 -- the STMPD row carrying YouTube, Spotify and the
// lyrics -- filed underneath it as a rendition. Only the canonical is ever shown, so
// the song's card had no buttons on it and its lyrics were unreachable.
//
// The old ordering caused it precisely: "Original Mix" folds to no rendition at all, so
// the beatport row won the second comparison, and links were never reached.
func TestBetterCanonicalPrefersTheRowWithLinksOverTheOneThatMerelyLooksOriginal(t *testing.T) {
	beatport := Candidate{
		ID: 15370, NamesRendition: false, HasSlug: false, HasLyrics: false,
		HasLinks: false, ArtistCount: 3, ReleaseDate: "2016-07-15",
	}
	stmpd := Candidate{
		ID: 812, NamesRendition: true, HasSlug: true, HasLyrics: true,
		HasLinks: true, ArtistCount: 3, ReleaseDate: "2016-02-05",
	}

	if !BetterCanonical(stmpd, beatport) {
		t.Error("the row with the slug, the lyrics and the links must be canonical")
	}
	if BetterCanonical(beatport, stmpd) {
		t.Error("comparator is not antisymmetric on the case it exists for")
	}
}

// Provenance is weighed one field at a time, and each field must beat every weaker one
// on its own. Written as a ladder so a reordering shows up as a specific failure.
func TestBetterCanonicalOrdering(t *testing.T) {
	base := Candidate{ID: 2, ArtistCount: 2, ReleaseDate: "2020-01-01"}

	for _, tc := range []struct {
		name   string
		winner Candidate
		loser  Candidate
	}{
		{"a song beats a release",
			func() Candidate { c := base; c.HasSlug = false; return c }(),
			func() Candidate {
				c := base
				c.IsCollection = true
				c.HasSlug = true
				c.HasLyrics = true
				c.HasLinks = true
				return c
			}()},
		{"the slug beats lyrics, links and shape",
			func() Candidate { c := base; c.HasSlug = true; c.NamesRendition = true; return c }(),
			func() Candidate { c := base; c.HasLyrics = true; c.HasLinks = true; return c }()},
		{"lyrics beat links",
			func() Candidate { c := base; c.HasLyrics = true; return c }(),
			func() Candidate { c := base; c.HasLinks = true; return c }()},
		{"links beat naming no rendition",
			func() Candidate { c := base; c.HasLinks = true; c.NamesRendition = true; return c }(),
			func() Candidate { c := base; return c }()},
		{"naming no rendition beats a smaller credit",
			func() Candidate { c := base; c.ArtistCount = 5; return c }(),
			func() Candidate { c := base; c.NamesRendition = true; c.ArtistCount = 1; return c }()},
		{"a known date beats an absent one",
			func() Candidate { c := base; return c }(),
			func() Candidate { c := base; c.ReleaseDate = ""; return c }()},
		{"the earlier date wins",
			func() Candidate { c := base; c.ReleaseDate = "2019-01-01"; return c }(),
			func() Candidate { c := base; return c }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !BetterCanonical(tc.winner, tc.loser) {
				t.Errorf("expected %+v to beat %+v", tc.winner, tc.loser)
			}
			if BetterCanonical(tc.loser, tc.winner) {
				t.Errorf("not antisymmetric: %+v also beat %+v", tc.loser, tc.winner)
			}
		})
	}
}

// Ties must break on id, or the pass elects a different canonical on every run and
// rewrites the whole tree each time.
func TestBetterCanonicalIsStable(t *testing.T) {
	a := Candidate{ID: 7, ReleaseDate: "2020-01-01"}
	b := Candidate{ID: 9, ReleaseDate: "2020-01-01"}
	if !BetterCanonical(a, b) || BetterCanonical(b, a) {
		t.Error("identical rows must order by id")
	}
}
