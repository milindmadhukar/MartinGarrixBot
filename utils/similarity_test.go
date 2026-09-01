package utils_test

// The two thresholds pinned here are load-bearing:
//
//	0.85 decides whether an incoming Beatport track is merged into an existing
//	     catalogue row or inserted as a new song (findSimilarExistingSong).
//	0.6  decides whether a quiz answer is accepted and coins are paid out.
//
// Neither had any test coverage, and neither is configurable, so a change to
// SimilarityScore silently changes catalogue and payout behaviour.

import (
	"math"
	"testing"

	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// dedupeThreshold mirrors similarityThreshold in mgbot/handlers/beatport.go.
const dedupeThreshold = 0.85

// quizThreshold mirrors the literal passed by mgbot/commands/quiz.go.
const quizThreshold = 0.6

func TestSimilarityScore_ExactAndEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b string
		want float64
	}{
		{"identical", "Animals", "Animals", 1.0},
		{"case insensitive", "Animals", "animals", 1.0},
		{"spacing and punctuation ignored", "Scared To Be Lonely", "scared to be lonely", 1.0},
		{"punctuation only differences", "Ocean!", "Ocean", 1.0},
		{"both empty", "", "", 1.0},
		{"both reduce to empty", "!!!", "???", 1.0},
		{"empty against non-empty", "", "Animals", 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := utils.SimilarityScore(tt.a, tt.b); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("SimilarityScore(%q, %q) = %.4f, want %.4f", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSimilarityScore_Properties(t *testing.T) {
	t.Parallel()

	// A corpus of real Garrix / STMPD catalogue strings plus near-misses.
	corpus := []string{
		"", "Animals", "animals", "Animal", "Anmials", "Ocean", "Home", "Poison",
		"Position", "Scared To Be Lonely", "In The Name Of Love", "High On Life",
		"Martin Garrix - Animals", "Martin Garrix - Animals (Original Mix)",
		"Martin Garrix, Bono, The Edge - We Are The People",
		"Julian Jordan - Kangaroo", "Dyro - Paradox", "Høme", "café",
		"!!!", "🎧", "a", "ab",
	}

	for _, a := range corpus {
		for _, b := range corpus {
			got := utils.SimilarityScore(a, b)

			if got < 0 || got > 1 {
				t.Errorf("SimilarityScore(%q, %q) = %.4f, outside [0,1]", a, b, got)
			}
			if rev := utils.SimilarityScore(b, a); math.Abs(got-rev) > 1e-9 {
				t.Errorf("SimilarityScore is not symmetric: (%q,%q)=%.4f but (%q,%q)=%.4f",
					a, b, got, b, a, rev)
			}
		}

		if self := utils.SimilarityScore(a, a); math.Abs(self-1.0) > 1e-9 {
			t.Errorf("SimilarityScore(%q, %q) = %.4f, want 1.0 for identity", a, a, self)
		}
	}

	// Deliberately not asserted: the triangle inequality. Normalised edit
	// distance violates it, so it is not a valid invariant here.
}

func TestIsCloseMatch_QuizThreshold(t *testing.T) {
	t.Parallel()

	// target is the song name; input is what the member typed.
	tests := []struct {
		name   string
		target string
		input  string
		want   bool
	}{
		{"exact", "Animals", "Animals", true},
		{"different case", "Animals", "animals", true},
		{"missing trailing letter", "Animals", "Animal", true},
		{"transposed letters", "Animals", "Anmials", true},
		{"plural form", "Ocean", "Oceans", true},
		{"one letter off on a short title", "Home", "Dome", true},
		{"minor misspelling in a long title", "Scared To Be Lonely", "scared to be lonly", true},
		{"partial title still accepted", "In The Name Of Love", "name of love", true},
		{"a different song is rejected", "Animals", "Poison", false},
		{"an unrelated short title is rejected", "Ocean", "Home", false},
		{"empty answer is rejected", "Animals", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.IsCloseMatch(tt.target, tt.input, quizThreshold)
			if got != tt.want {
				t.Errorf("IsCloseMatch(%q, %q, %v) = %v, want %v (score %.4f)",
					tt.target, tt.input, quizThreshold, got, tt.want,
					utils.SimilarityScore(tt.target, tt.input))
			}
		})
	}
}

func TestIsCloseMatch_DedupeThreshold(t *testing.T) {
	t.Parallel()

	// Both sides are the "Artists - Name" key that findSimilarExistingSong builds.
	tests := []struct {
		name     string
		existing string
		incoming string
		want     bool
	}{
		{
			name:     "identical release dedupes",
			existing: "Martin Garrix - Poison",
			incoming: "Martin Garrix - Poison",
			want:     true,
		},
		{
			name:     "differing artist does not dedupe",
			existing: "Dyro - Paradox",
			incoming: "Julian Jordan - Paradox",
			want:     false,
		},
		{
			name:     "an added collaborator does not dedupe",
			existing: "Julian Jordan - Kangaroo",
			incoming: "Julian Jordan, Martin Garrix - Kangaroo",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.IsCloseMatch(tt.existing, tt.incoming, dedupeThreshold)
			if got != tt.want {
				t.Errorf("IsCloseMatch(%q, %q, %v) = %v, want %v (score %.4f)",
					tt.existing, tt.incoming, dedupeThreshold, got, tt.want,
					utils.SimilarityScore(tt.existing, tt.incoming))
			}
		})
	}
}

// BUG: the 0.85 dedupe threshold does not fire for mix-suffix variants, which
// is the single most common difference between a Beatport title and the STMPD
// title for the same track ("(Original Mix)", "Extended Mix"). The suffix adds
// enough characters that the normalised score lands near 0.6, so these are
// inserted as new rows instead of being merged. Pinned rather than fixed:
// changing the threshold or the normalisation re-partitions rows already in
// the database. See TestDedupe_ShouldMatchMixVariants for the intended result.
func TestDedupe_MixVariantsDoNotMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		existing string
		incoming string
	}{
		{"Martin Garrix - Animals", "Martin Garrix - Animals (Original Mix)"},
		{"Martin Garrix - Animals", "Martin Garrix - Animals - Extended Mix"},
		{"Martin Garrix - Byte", "Martin Garrix - Byte (Original Mix)"},
	}

	for _, tt := range tests {
		if utils.IsCloseMatch(tt.existing, tt.incoming, dedupeThreshold) {
			t.Errorf("IsCloseMatch(%q, %q, %v) now returns true; the dedupe gap is "+
				"fixed, so unskip TestDedupe_ShouldMatchMixVariants and delete this test",
				tt.existing, tt.incoming, dedupeThreshold)
		}
	}
}

// BUG: at exactly 0.85, two genuinely different titles merge. "Poison" and
// "Position" differ by two edits over twenty characters, which is enough to
// clear the threshold, so a real release can be absorbed into an unrelated row.
func TestDedupe_PoisonMatchesPosition(t *testing.T) {
	t.Parallel()

	const a, b = "Martin Garrix - Poison", "Martin Garrix - Position"

	if !utils.IsCloseMatch(a, b, dedupeThreshold) {
		t.Errorf("IsCloseMatch(%q, %q, %v) now returns false; the false-positive "+
			"is fixed, so delete this test", a, b, dedupeThreshold)
	}
}

func TestDedupe_ShouldMatchMixVariants(t *testing.T) {
	t.Skip("BUG: mix-suffix variants score ~0.63 and so are never deduped. " +
		"Fixing this needs a normalisation step that strips mix suffixes before " +
		"comparison, plus a decision about the duplicate rows already stored.")

	if !utils.IsCloseMatch("Martin Garrix - Animals",
		"Martin Garrix - Animals (Original Mix)", dedupeThreshold) {
		t.Error("a mix variant of the same track should dedupe against the original")
	}
}
