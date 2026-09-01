package utils

// In-package: preprocessString, levenshteinDistance, min and max are unexported.
// Note that utils declares its own min/max, shadowing the builtins, so anything
// needing the builtin form belongs in utils_test instead.

import (
	"math"
	"testing"
)

func TestPreprocessString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"lowercases", "ANIMALS", "animals"},
		{"strips spaces", "Martin Garrix", "martingarrix"},
		{"strips punctuation", "Martin Garrix - Animals (Original Mix)", "martingarrixanimalsoriginalmix"},
		{"keeps digits", "  Hello, World! 123 ", "helloworld123"},
		{"only punctuation collapses to empty", "!@#$%^&*()", ""},
		{"keeps non-ASCII letters", "Høme", "høme"},
		{"strips emoji", "Animals 🎧", "animals"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := preprocessString(tt.in); got != tt.want {
				t.Errorf("preprocessString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLevenshteinDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		a, b   string
		want   int
		reason string
	}{
		{name: "both empty", a: "", b: "", want: 0},
		{name: "empty against non-empty", a: "", b: "abc", want: 3},
		{name: "non-empty against empty", a: "abc", b: "", want: 3},
		{name: "identical", a: "animals", b: "animals", want: 0},
		{name: "classic kitten/sitting", a: "kitten", b: "sitting", want: 3},
		{name: "single substitution", a: "home", b: "dome", want: 1},
		{name: "single deletion", a: "animals", b: "animal", want: 1},
		{name: "transposition costs two", a: "animals", b: "anmials", want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := levenshteinDistance(tt.a, tt.b); got != tt.want {
				t.Errorf("levenshteinDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// BUG: levenshteinDistance indexes bytes, not runes. A single accented
// character differs in two bytes, so it is charged two edits instead of one,
// and len() reports the byte length as the normalisation denominator. Titles
// with accents therefore score lower than they should. Pinned here so the
// behaviour is visible; see TestSimilarityScore_RuneAware for the fix.
func TestLevenshteinDistance_IsByteBasedNotRuneBased(t *testing.T) {
	t.Parallel()

	if got := levenshteinDistance("cafe", "café"); got != 2 {
		t.Errorf("levenshteinDistance(cafe, café) = %d, want 2 (byte-based); "+
			"a rune-aware implementation would return 1", got)
	}
	if got := levenshteinDistance("home", "høme"); got != 2 {
		t.Errorf("levenshteinDistance(home, høme) = %d, want 2 (byte-based); "+
			"a rune-aware implementation would return 1", got)
	}
}

func TestSimilarityScore_RuneAware(t *testing.T) {
	t.Skip("BUG: levenshteinDistance is byte-indexed; converting to []rune once " +
		"would fix this, but it changes dedupe outcomes for rows already in the " +
		"database, so it needs a deliberate decision plus a backfill.")

	// One accented character should cost exactly one edit out of four runes.
	if got := SimilarityScore("café", "cafe"); math.Abs(got-0.75) > 1e-9 {
		t.Errorf("SimilarityScore(café, cafe) = %.4f, want 0.75", got)
	}
}

func TestMin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []int
		want int
	}{
		{"single element", []int{7}, 7},
		{"ascending", []int{1, 2, 3}, 1},
		{"descending", []int{3, 2, 1}, 1},
		{"duplicates", []int{2, 2, 2}, 2},
		{"negatives", []int{-1, 5, -9}, -9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := min(tt.in...); got != tt.want {
				t.Errorf("min(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// min indexes nums[0] before checking the length, so no arguments panics. No
// caller does this today (levenshteinDistance always passes three), but the
// behaviour should be recorded rather than discovered.
func TestMin_PanicsOnNoArguments(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("min() with no arguments did not panic; if it was made safe, update this test")
		}
	}()
	_ = min()
}

func TestMax(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b int
		want int
	}{
		{"a greater", 5, 3, 5},
		{"b greater", 3, 5, 5},
		{"equal", 4, 4, 4},
		{"negatives", -5, -3, -3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := max(tt.a, tt.b); got != tt.want {
				t.Errorf("max(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
