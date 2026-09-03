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

func TestMatchKeyPairsADefaultRenditionWithNone(t *testing.T) {
	// The real pair out of the catalogue: beatport files "All We Got" as an Extended
	// Mix and a Mix Cut, the STMPD dataset names no version at all. All three are the
	// same recording, and while the key disagreed the dedupe pass never compared them.
	none := MatchKey("All We Got", "", "", "Shy Baboon & Maejor")
	for _, mix := range []string{"Extended Mix", "Mix Cut", "Original Mix", "Extended Version"} {
		if got := MatchKey("All We Got", "", mix, "Maejor, Shy Baboon"); got != none {
			t.Errorf("MatchKey with mix %q = %q; want %q", mix, got, none)
		}
	}
}

func TestMatchKeyStillSeparatesANamedRendition(t *testing.T) {
	// Collapsing the defaults must not collapse a rendition that names something.
	plain := MatchKey("Catharina", "", "", "Martin Garrix")
	if MatchKey("Catharina", "", "Acoustic Version", "Martin Garrix") == plain {
		t.Error("an acoustic version must not key as the plain release")
	}
	if MatchKey("Catharina (Surf Mesa Remix)", "", "", "Martin Garrix") == plain {
		t.Error("a remix must not key as the plain release")
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

func TestSameRecordingRejectsAppleSearchNearMisses(t *testing.T) {
	// The case that makes verification non-optional: Apple has no "Drinks Up" by
	// AREA21, so its search returns the nearest AREA21 song instead. The artist is a
	// perfect match; only the title reveals that this is a different record.
	if SameRecording("Drinks Up", "AREA21", "Glad You Came", "AREA21") {
		t.Error("a different song by the same artist must not be accepted")
	}
	// "Bass" and "The Bass" are different releases, and we would rather miss a date
	// than write the wrong one.
	if SameRecording("Bass", "Julian Jordan", "The Bass (Mixed)", "Julian Jordan") {
		t.Error("a near-miss title must not be accepted")
	}
}

func TestSameRecordingAcceptsGenuineMatches(t *testing.T) {
	cases := []struct{ ta, aa, tb, ab string }{
		// Apple spells the feature into the title; the row spells it into the artists.
		{"We Are The People", "Martin Garrix ft. Bono & The Edge",
			"We Are the People (feat. Bono & The Edge)", "Martin Garrix"},
		{"Losing Ground", "Seth Hills ft. ALBA", "Losing Ground (feat. ALBA)", "Seth Hills"},
		// A rendition suffix on one side only.
		{"Gold Skies (ft. Aleesia)", "Sander van Doorn, Martin Garrix, DVBBS",
			"Gold Skies (feat. Aleesia) [Radio Edit]", "Sander van Doorn, Martin Garrix & DVBBS"},
		{"Helicopter", "Martin Garrix & Firebeatz", "Helicopter", "Martin Garrix & Firebeatz"},
	}
	for _, c := range cases {
		if !SameRecording(c.ta, c.aa, c.tb, c.ab) {
			t.Errorf("should match:\n  %q by %q\n  %q by %q", c.ta, c.aa, c.tb, c.ab)
		}
	}
}

func TestSameRecordingRejectsUnrelatedArtists(t *testing.T) {
	if SameRecording("Together", "Martin Garrix", "Together", "Some Other Act") {
		t.Error("the same title by unrelated artists must not be accepted")
	}
}

func TestRemixMustNotClaimTheOriginalsRow(t *testing.T) {
	// Guards the La La La corruption: the "Drove Remix" release matched the row
	// holding the plain original through the base-key tier, overwrote its release
	// date with the remix's, and pushed the original out into a new row.
	//
	// This is a key-level assertion of the same asymmetry SongIndex enforces: a
	// rendition and an original are not interchangeable.
	original := MatchKey("La La La", "", "Original Mix", "AREA21")
	remix := MatchKey("La La La", "Drove Remix", "", "AREA21")
	if original == remix {
		t.Errorf("a remix must not key the same as the original it derives from: %q", original)
	}
}

func TestRenditionSurvivesAFeatureClause(t *testing.T) {
	// The feature was stripped by cutting to the end of the string, which swallowed
	// any rendition that followed it. "X's feat. Icona Pop (Osrin Remix)" reduced to
	// plain "X's", so the remix keyed identically to the original -- and the remix's
	// video was matched onto the original's row.
	base, variant := SplitVariant("X's feat. Icona Pop (Osrin Remix)", "", "")
	if base != "xs" || variant != "osrinremix" {
		t.Errorf("SplitVariant = (%q, %q); want (\"xs\", \"osrinremix\")", base, variant)
	}

	original := MatchKey("X's (feat. Icona Pop)", "", "", "CMC$ & GRX")
	remix := MatchKey("X's feat. Icona Pop (Osrin Remix)", "", "", "CMC$ & GRX")
	if original == remix {
		t.Errorf("the remix must not key the same as the original: %q", original)
	}

	// The featured artist still counts, and the remixer is not mistaken for one.
	if got := creditKey("X's feat. Icona Pop (Osrin Remix)", "CMC$ & GRX"); got != creditKey("X's", "CMC$ & GRX, Icona Pop") {
		t.Errorf("credit key folded the rendition into the artists: %q", got)
	}
}

func TestIsCollectionName(t *testing.T) {
	for _, name := range []string{
		"Dawn EP", "Half Human [ALBUM]", "Catharina (Remixes)", "Eyes On Me [EP]",
		"STMPD RCRDS Mixtape 2025 Side A", "Another World (Festival Edits Part I)",
		"Ocean [Remixes Vol. 1]", "XXX EP",
	} {
		if !IsCollectionName(name) {
			t.Errorf("IsCollectionName(%q) = false; it is a release, not a track", name)
		}
	}

	// The markers must not fire inside longer words, or ordinary songs get pulled
	// out of the radio rotation.
	for _, name := range []string{
		"Deep End", "Help Me", "Sleepless Nights", "Repeat It", "Epiphany",
		"Remix Contest Winner", "Alps",
	} {
		if IsCollectionName(name) {
			t.Errorf("IsCollectionName(%q) = true; it is a song", name)
		}
	}
}

func TestArtistDisambiguationSuffixIsNotPartOfTheName(t *testing.T) {
	// Beatport appends a country tag when two acts share a name. Keeping it made
	// "Brooks" and "Brooks (NL)" two different artists, so "Boomerang" had two rows
	// that could never match each other.
	if ArtistSetKey("GRX, Brooks (NL)") != ArtistSetKey("Brooks, GRX") {
		t.Errorf("disambiguated name did not normalize: %q vs %q",
			ArtistSetKey("GRX, Brooks (NL)"), ArtistSetKey("Brooks, GRX"))
	}
	if ArtistSetKey("Carola (BR)") != ArtistSetKey("Carola") {
		t.Error("two-letter country tag should be stripped")
	}
	// A parenthetical that is not a country tag must survive.
	if ArtistSetKey("Matisse & Sadko") == ArtistSetKey("Matisse") {
		t.Error("stripping went too far")
	}
}

func TestIsCollectionCatchesSetsAndLongRecordings(t *testing.T) {
	// A DJ set whose title says nothing about being one.
	if !IsCollection("Tomorrowland 2016: The Elixir Of Life", "Continuous Mix 5", 1758191) {
		t.Error("a 29-minute continuous mix is a set, not a track")
	}
	// Length alone is enough.
	if !IsCollection("Some Long Thing", "", 20*60*1000) {
		t.Error("a 20-minute recording is not a track")
	}
	// An ordinary song is not caught by either rule.
	if IsCollection("Animals", "Original Mix", 302000) {
		t.Error("a five-minute original mix is a track")
	}
}

func TestStrokeLettersFoldToPlainLetters(t *testing.T) {
	// NFD decomposes an accent away from its letter, but the stroke in "Ø" is part of
	// the glyph, so "NØ SIGNE" and "NO SIGNE" stayed two different artists and their
	// one song stayed two rows.
	if ArtistSetKey("NØ SIGNE") != ArtistSetKey("NO SIGNE") {
		t.Errorf("Ø did not fold: %q vs %q", ArtistSetKey("NØ SIGNE"), ArtistSetKey("NO SIGNE"))
	}
	for _, pair := range [][2]string{
		{"Mø", "Mo"}, {"Æther", "Aether"}, {"Þor", "Thor"}, {"Łukasz", "Lukasz"},
	} {
		if NormalizeToken(pair[0]) != NormalizeToken(pair[1]) {
			t.Errorf("%q did not fold to %q: %q vs %q",
				pair[0], pair[1], NormalizeToken(pair[0]), NormalizeToken(pair[1]))
		}
	}
}

// A release keeps its identity once re-keying has moved the rendition out of the
// title: "Hero (Remixes)" and name "Hero" + mix "Remixes" are the same row, and the
// catalogue must not start offering it as a song halfway through a maintenance pass.
func TestIsCollectionReadsTheRendition(t *testing.T) {
	cases := []struct {
		name, mix string
		want      bool
	}{
		{"Hero", "Remixes", true},
		{"Hero", "", false},
		{"Scared To Be Lonely Remixes Vol. 1", "Remixes Vol. 1", true},
		{"Dreamer", "Remixes Vol. 2", true},
		{"Another World", "Festival Edits Part I", true},
		{"Hero", "Space Ducks Extended Remix", false},
		{"Animals", "Original Mix", false},
		{"Catharina", "Extended Mix", false},
		// "Remix" singular is one track; only the plural names a package.
		{"Bad Blood", "Julian Jordan Remix", false},
		// mix_name here is the package the track came on, not the track's variant.
		{"Higher Ground (DubVision Remix)", "Remixes", false},
		// ...but when the title's own variant is itself a package, it stays one.
		{"So Far Away (Remixes Vol.I)", "Remixes Vol. 1", true},
	}

	for _, c := range cases {
		if got := IsCollection(c.name, c.mix, 0); got != c.want {
			t.Errorf("IsCollection(%q, %q) = %v, want %v", c.name, c.mix, got, c.want)
		}
	}
}

// The catalogue slug distinguishes a release from the single of the same name, which
// nothing in the title, artist or rendition can do: "Void" and the "Void EP" are one
// title, one artist and no rendition apart.
func TestStmpdSlugNamesRelease(t *testing.T) {
	cases := []struct {
		name, slug string
		want       bool
	}{
		{"Void", "seth-hills-void-ep-2020-8-10", true},
		{"Void", "seth-hills-void-2020-5-14", false},
		// A track on someone else's EP is not that EP.
		{"Mind The Grind", "bombai-ep-2018-3-2", false},
		{"Higher Ground", "martin-garrix-higher-ground-remixes-2021-1-1", true},
		{"Breathe", "mesto-breathe-2024-5-10", false},
		{"", "seth-hills-void-ep-2020-8-10", false},
	}

	for _, c := range cases {
		if got := StmpdSlugNamesRelease(c.name, c.slug); got != c.want {
			t.Errorf("StmpdSlugNamesRelease(%q, %q) = %v, want %v", c.name, c.slug, got, c.want)
		}
	}
}
