package utils

import "testing"

func TestArtistSetKeyIsOrderAndSeparatorIndependent(t *testing.T) {
	// The pair that motivated this file: the same collaboration as each source
	// spells it. These must produce an identical key or the link backfill cannot
	// find the row it is meant to enrich.
	tests := []struct {
		name string
		a, b string
	}{
		{"stmpd ampersand vs beatport comma", "Martin Garrix & Ed Sheeran", "Ed Sheeran, Martin Garrix"},
		{"three-way reordered", "Skytech, R3HAB, Martin Garrix", "Martin Garrix & R3HAB & Skytech"},
		{"and spelled out", "Martin Garrix and Matisse & Sadko", "Matisse & Sadko, Martin Garrix"},
		{"feat is just another separator", "Martin Garrix feat. Bonn", "Bonn, Martin Garrix"},
		{"missing space after ampersand", "Martin Garrix &DallasK", "DallasK, Martin Garrix"},
		{"case and punctuation", "NOME. & Merow", "merow & nome"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, want := ArtistSetKey(tt.a), ArtistSetKey(tt.b)
			if got != want {
				t.Errorf("ArtistSetKey(%q) = %q, ArtistSetKey(%q) = %q; want equal",
					tt.a, got, tt.b, want)
			}
			if got == "" {
				t.Errorf("ArtistSetKey(%q) is empty", tt.a)
			}
		})
	}
}

func TestArtistSetKeySeparatesDifferentCredits(t *testing.T) {
	if ArtistSetKey("Martin Garrix") == ArtistSetKey("Martin Garrix & Ed Sheeran") {
		t.Error("a solo credit must not key the same as a collaboration")
	}
}

func TestSplitVariant(t *testing.T) {
	tests := []struct {
		name                    string
		title, version, mixName string
		wantBase, wantVariant   string
	}{
		{"plain title", "Catharina", "", "", "catharina", ""},
		{"original mix is not a variant", "Catharina", "", "Original Mix", "catharina", ""},
		{"beatport extended mix", "Catharina", "", "Extended Mix", "catharina", "extendedmix"},
		{"dataset version field", "Repeat It", "Acoustic Version", "", "repeatit", "acousticversion"},
		{"variant inside the title", "Catharina (Surf Mesa Extended Remix)", "", "",
			"catharina", "surfmesaextendedremix"},
		// A featured credit is neither a rendition nor part of the title. The two
		// sources disagree about where to put it -- beatport in the title, STMPD in
		// the artists -- and it is present in the artist set either way, so it is
		// dropped from both slots.
		{"featured artist dropped from the title", "Angels For Each Other (feat. Arijit Singh)", "", "",
			"angelsforeachother", ""},
		{"featured artist dropped without brackets", "Set Me Free feat. Bonn", "", "",
			"setmefree", ""},
		{"punctuation folded", "Bad B*tch", "", "", "badbtch", ""},
		{"apostrophe folded", "Steppin'", "", "", "steppin", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, variant := SplitVariant(tt.title, tt.version, tt.mixName)
			if base != tt.wantBase || variant != tt.wantVariant {
				t.Errorf("SplitVariant(%q, %q, %q) = (%q, %q); want (%q, %q)",
					tt.title, tt.version, tt.mixName, base, variant, tt.wantBase, tt.wantVariant)
			}
		})
	}
}

func TestBaseKeyGroupsRemixesWithTheOriginal(t *testing.T) {
	// "Catharina" is six rows in production: the original plus five named remixes.
	// They must share a base key so the presentation layer can collapse them.
	original := BaseKey("Catharina", "Martin Garrix")
	for _, remix := range []string{
		"Catharina (Surf Mesa Extended Remix)",
		"Catharina (Sentinel Extended Remix)",
		"Catharina (Robbie Mendez Extended Remix)",
	} {
		if got := BaseKey(remix, "Martin Garrix"); got != original {
			t.Errorf("BaseKey(%q) = %q; want %q", remix, got, original)
		}
	}
}

func TestMatchKeySeparatesRenditions(t *testing.T) {
	// Base keys group them; match keys must still tell them apart, because each
	// remix is a distinct recording with its own beatport id, BPM and length.
	a := MatchKey("Catharina", "", "Surf Mesa Extended Remix", "Martin Garrix, Surf Mesa")
	b := MatchKey("Catharina", "", "Sentinel Extended Remix", "Martin Garrix, Sentinel")
	if a == b {
		t.Errorf("two different remixes share a match key: %q", a)
	}
}

func TestMatchKeyPairsAcrossSources(t *testing.T) {
	// The real cross-source pair: the STMPD dataset document and the beatport row.
	stmpd := MatchKey("Repeat It", "Acoustic Version", "", "Martin Garrix & Ed Sheeran")
	beatport := MatchKey("Repeat It", "", "Acoustic Version", "Ed Sheeran, Martin Garrix")
	if stmpd != beatport {
		t.Errorf("cross-source match keys differ:\n stmpd    = %q\n beatport = %q", stmpd, beatport)
	}
}

func TestBaseKeyPairsOriginalWithExtendedMix(t *testing.T) {
	// STMPD publishes the original; beatport lists the extended mix. Tier 5 of the
	// matcher relies on these sharing a base key.
	if BaseKey("Catharina", "Martin Garrix") != BaseKey("Catharina (Extended Mix)", "Martin Garrix") {
		t.Error("an extended mix must share a base key with its original")
	}
}

func TestArtistsSubsume(t *testing.T) {
	// A beatport remix row credits the original artist plus the remixer, which is
	// why remix-to-parent linking cannot require the artist sets to be equal.
	if !ArtistsSubsume("Martin Garrix, Surf Mesa", "Martin Garrix") {
		t.Error("a remix's credits must subsume the original's")
	}
	if !ArtistsSubsume("Sentinel, Martin Garrix", "Martin Garrix") {
		t.Error("order must not matter")
	}
	if ArtistsSubsume("Martin Garrix", "Martin Garrix, Surf Mesa") {
		t.Error("the original must not subsume the remix")
	}
	if ArtistsSubsume("Martin Garrix, Surf Mesa", "Armin van Buuren") {
		t.Error("unrelated artists must not subsume")
	}
	if ArtistsSubsume("Martin Garrix", "") {
		t.Error("an empty credit must not subsume anything")
	}
}

func TestTitleKeyIgnoresArtists(t *testing.T) {
	if TitleKey("Catharina", "", "Surf Mesa Extended Remix") != TitleKey("Catharina", "", "Original Mix") {
		t.Error("TitleKey must ignore the rendition and key on the title alone")
	}
}

func TestFeatureClauseIsNormalizedAwayAcrossSources(t *testing.T) {
	// Beatport puts the feature in the title, STMPD puts it in the artists. Both
	// name the same recording and must produce the same key.
	beatport := MatchKey("Set Me Free feat. Bonn", "", "", "Martin Garrix, Arcando, Bonn")
	stmpd := MatchKey("Set Me Free", "", "", "Martin Garrix & Arcando feat. Bonn")
	if beatport != stmpd {
		t.Errorf("feature placement changed the key:\n beatport = %q\n stmpd    = %q", beatport, stmpd)
	}
}

func TestFeatureCreditedOnlyInTheTitleStillCounts(t *testing.T) {
	// The shape that produced three separate "Love Runs Out" entries in autocomplete:
	// one row credits the featured artists only in the title, one only in the artists
	// field. Both name the same recording and must key identically.
	titleOnly := MatchKey("Love Runs Out (feat. G-Eazy & Sasha Alex Sloan)", "", "", "Martin Garrix")
	inArtists := MatchKey("Love Runs Out", "", "Original Mix", "Martin Garrix, G-Eazy, Sasha Alex Sloan")
	if titleOnly != inArtists {
		t.Errorf("feature placement changed the key:\n title-only = %q\n in-artists = %q", titleOnly, inArtists)
	}
}

func TestFeatureInTitleDoesNotCollapseDistinctSongs(t *testing.T) {
	// Folding the title's credit into the artist set must not make a solo track and a
	// collaboration look the same.
	solo := MatchKey("Love Runs Out", "", "", "Martin Garrix")
	collab := MatchKey("Love Runs Out (feat. G-Eazy)", "", "", "Martin Garrix")
	if solo == collab {
		t.Error("a solo recording must not key the same as one with a featured artist")
	}
}
