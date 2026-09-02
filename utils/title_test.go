package utils_test

// The cases here are real songs.name values, taken from a sweep of the catalogue.
// The three classes of trailing group -- rendition, featured-artist credit, second
// title -- look identical to a substring test and mean entirely different things to a
// player, which is the whole reason utils/title.go exists.

import (
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

func TestSplitTitleParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		title     string
		base      string
		subtitle  string
		featured  string
		rendition string
	}{
		// The three the community complained about.
		{"subtitle", "Breach (Walk Alone)", "Breach", "Walk Alone", "", ""},
		{"parenthesised credit", "Sun Is Never Going Down (feat. Dawn Golden)",
			"Sun Is Never Going Down", "", "Dawn Golden", ""},
		{"quoted bare credit", `Now that I've Found You Feat. "John & Michel"`,
			"Now that I've Found You", "", "John & Michel", ""},

		// Both at once, which is why the peel loop takes the group before the credit.
		{"subtitle then credit", "Starlight (Keep Me Afloat) feat. Shaun Farrugia",
			"Starlight", "Keep Me Afloat", "Shaun Farrugia", ""},

		// A rendition is not a subtitle. Getting this wrong would offer "Alle Farben
		// Remix" as a correct answer to "name this song".
		{"named remix", "Drown (Alle Farben Remix)", "Drown", "", "", "Alle Farben Remix"},
		{"club mix", "Mistaken (Club Mix)", "Mistaken", "", "", "Club Mix"},
		{"acoustic", "Scared To Be Lonely (Acoustic Version)",
			"Scared To Be Lonely", "", "", "Acoustic Version"},
		{"remix package", "Hero (Remixes)", "Hero", "", "", "Remixes"},
		{"numbered remix package", "Told You So (Remixes Vol. 1)",
			"Told You So", "", "", "Remixes Vol. 1"},
		{"ep", "GREED (EP)", "GREED", "", "", "EP"},

		// "with" is a credit marker here but deliberately not in featureMarkers, which
		// feeds every match_key in the database.
		{"with credit", "Fire (with Elderbrook)", "Fire", "", "Elderbrook", ""},

		// A part number stays glued to the base: "Howling" is a different song.
		{"part number", "Howling (Pt. II)", "Howling (Pt. II)", "", "", ""},
		{"part number roman", "Prayer (Pt. II)", "Prayer (Pt. II)", "", "", ""},

		// A group too short to be a name is not treated as one.
		{"three letter group", "It's Alright (Not)", "It's Alright", "", "", "Not"},

		{"bare credit", "Bouncybob Feat. Justin Mylo & Mesto",
			"Bouncybob", "", "Justin Mylo & Mesto", ""},
		{"repeated bare credit", "Break Through The Silence feat. Matisse feat. Sadko",
			"Break Through The Silence", "", "Matisse feat. Sadko", ""},

		{"plain title", "Animals", "Animals", "", "", ""},
		{"nothing but a group", "(Interlude)", "(Interlude)", "", "", ""},
		{"empty", "", "", "", "", ""},

		// The rest of the subtitle class from the sweep.
		{"subtitle tasty", "Melt (Tasty)", "Melt", "Tasty", "", ""},
		{"subtitle question", "Virus (How About Now)", "Virus", "How About Now", "", ""},
		{"subtitle anthem", "Tremor (Sensation 2014 Anthem)",
			"Tremor", "Sensation 2014 Anthem", "", ""},
		{"subtitle the wire", "I Don't Know Your Name (The Wire)",
			"I Don't Know Your Name", "The Wire", "", ""},
		{"subtitle clause", "Feelings (That I Can't Deny)",
			"Feelings", "That I Can't Deny", "", ""},
		{"subtitle spanish", "Baila (La Banda)", "Baila", "La Banda", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.SplitTitleParts(tt.title)
			if got.Base != tt.base {
				t.Errorf("Base = %q, want %q", got.Base, tt.base)
			}
			if got.Subtitle != tt.subtitle {
				t.Errorf("Subtitle = %q, want %q", got.Subtitle, tt.subtitle)
			}
			if got.Featured != tt.featured {
				t.Errorf("Featured = %q, want %q", got.Featured, tt.featured)
			}
			if got.Rendition != tt.rendition {
				t.Errorf("Rendition = %q, want %q", got.Rendition, tt.rendition)
			}
		})
	}
}

func TestAcceptedTitles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  []string
	}{
		{"subtitle offers three forms", "Breach (Walk Alone)",
			[]string{"Breach (Walk Alone)", "Breach", "Walk Alone"}},
		{"credit offers two", "Sun Is Never Going Down (feat. Dawn Golden)",
			[]string{"Sun Is Never Going Down (feat. Dawn Golden)", "Sun Is Never Going Down"}},
		// A rendition never becomes a standalone answer, though the stored name that
		// contains it does: a player typing the title as printed is not wrong.
		{"rendition is not an answer", "Drown (Alle Farben Remix)",
			[]string{"Drown (Alle Farben Remix)", "Drown"}},
		// The credit is absent. Naming the guest vocalist is not naming the song.
		{"credit is not an answer", `Now that I've Found You Feat. "John & Michel"`,
			[]string{`Now that I've Found You Feat. "John & Michel"`, "Now that I've Found You"}},
		{"plain title collapses to one", "Animals", []string{"Animals"}},
		{"part number collapses to one", "Howling (Pt. II)", []string{"Howling (Pt. II)"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := utils.AcceptedTitles(tt.title); !slices.Equal(got, tt.want) {
				t.Errorf("AcceptedTitles(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestGuessMatchesSong(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		guess string
		want  bool
	}{
		// The reported failures. All three are correct answers that were rejected.
		{"base title of a subtitled song", "Breach (Walk Alone)", "Breach", true},
		{"the subtitle itself", "Breach (Walk Alone)", "walk alone", true},
		{"the full stored name", "Breach (Walk Alone)", "Breach (Walk Alone)", true},
		{"title without the credit", "Sun Is Never Going Down (feat. Dawn Golden)",
			"sun is never going down", true},
		{"title without a quoted credit", `Now that I've Found You Feat. "John & Michel"`,
			"now that ive found you", true},

		// Ordinary fuzz on the base title is unchanged.
		{"typo in the base title", "Breach (Walk Alone)", "Breachh", true},
		{"case and punctuation ignored", "It's Alright (Not)", "its alright", true},

		// A subtitle is matched exactly, because subtitles are short ordinary words
		// and 0.6 over a handful of characters buys a whole free edit.
		{"near miss on a subtitle is rejected", "Melt (Tasty)", "nasty", false},
		{"exact subtitle is accepted", "Melt (Tasty)", "Tasty", true},

		// Naming the guest vocalist or the remixer is not naming the song.
		{"the featured artist is not an answer", "Sun Is Never Going Down (feat. Dawn Golden)",
			"Dawn Golden", false},
		{"the remixer is not an answer", "Drown (Alle Farben Remix)", "Alle Farben", false},

		{"a different song is rejected", "Breach (Walk Alone)", "Animals", false},
		{"empty is rejected", "Breach (Walk Alone)", "", false},
		{"punctuation only is rejected", "Breach (Walk Alone)", "???", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			song := db.Song{Name: tt.title}
			if got := utils.GuessMatchesSong(song, tt.guess); got != tt.want {
				t.Errorf("GuessMatchesSong(%q, %q) = %v, want %v", tt.title, tt.guess, got, tt.want)
			}
		})
	}
}

// A stored normalized_name is an override, not a cache: it wins over what the parser
// would derive, which is the only way a human can correct a title the classifier gets
// wrong.
func TestSongAnswers_StoredNameOverrides(t *testing.T) {
	t.Parallel()

	song := db.Song{
		Name:           "Tremor (Sensation 2014 Anthem)",
		NormalizedName: pgtype.Text{String: "Tremor Anthem", Valid: true},
	}

	want := []string{"Tremor (Sensation 2014 Anthem)", "Tremor Anthem", "Sensation 2014 Anthem"}
	if got := utils.SongAnswers(song); !slices.Equal(got, want) {
		t.Errorf("SongAnswers = %q, want %q", got, want)
	}
	if !utils.GuessMatchesSong(song, "tremor anthem") {
		t.Error("the stored normalized name should be an accepted answer")
	}
}

// An absent normalized_name has to fall back to deriving one, or the quiz breaks for
// every row until rekey-songs has run.
func TestSongAnswers_FallsBackWhenUnset(t *testing.T) {
	t.Parallel()

	song := db.Song{Name: "Breach (Walk Alone)"}
	want := []string{"Breach (Walk Alone)", "Breach", "Walk Alone"}
	if got := utils.SongAnswers(song); !slices.Equal(got, want) {
		t.Errorf("SongAnswers = %q, want %q", got, want)
	}
}

func TestTitleAppearsIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		line  string
		title string
		want  bool
	}{
		// The Breach round showed this exact line as the clue for a song whose
		// accepted answer is now "Walk Alone".
		{"subtitle in a lyric", "You'll never walk alone", "Walk Alone", true},
		{"title in a lyric", "I can feel the tremor", "Tremor", true},
		{"punctuation does not hide it", "Don't look down!", "Don't Look Down", true},
		// Word boundaries: a naive Contains on "Not" hides most of a song.
		{"substring of a longer word does not count", "There is nothing left", "Not", false},
		{"cannot does not contain not", "I cannot stay", "Not", false},
		{"unrelated line", "The sun is going down", "Animals", false},
		{"empty title never fires", "any line at all", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := utils.TitleAppearsIn(tt.line, tt.title); got != tt.want {
				t.Errorf("TitleAppearsIn(%q, %q) = %v, want %v", tt.line, tt.title, got, tt.want)
			}
		})
	}
}
