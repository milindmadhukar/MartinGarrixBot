package utils_test

import (
	"testing"

	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/STMPDBot/utils"
)

func TestCutString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		str    string
		maxLen int
		want   string
	}{
		{"under the limit is untouched", "hello", 10, "hello"},
		{"exactly at the limit is untouched", "hello", 5, "hello"},
		{"over the limit is truncated with an ellipsis", "hello world", 5, "hell…"},
		{"empty string", "", 10, ""},
		{"limit of one leaves room only for the ellipsis", "hello", 1, "…"},
		// Truncation counts runes, not bytes, so a multi-byte string is not
		// cut mid-character.
		{"counts runes not bytes", "héllo wörld", 5, "héll…"},
		{"emoji are single units", "🎧🎧🎧🎧🎧🎧", 3, "🎧🎧…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := utils.CutString(tt.str, tt.maxLen); got != tt.want {
				t.Errorf("CutString(%q, %d) = %q, want %q", tt.str, tt.maxLen, got, tt.want)
			}
		})
	}
}

// A non-positive limit leaves no room for the text nor the ellipsis. This used
// to slice runes[0:-1] and panic.
func TestCutString_NonPositiveLimit(t *testing.T) {
	t.Parallel()

	for _, maxLen := range []int{0, -1, -100} {
		if got := utils.CutString("hello", maxLen); got != "" {
			t.Errorf("CutString(%q, %d) = %q, want %q", "hello", maxLen, got, "")
		}
	}
}

func TestExtractEmojiParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		in           string
		wantName     string
		wantID       snowflake.ID
		wantAnimated bool
	}{
		{
			name:     "static custom emoji",
			in:       "<:spotify:1328049287640125562>",
			wantName: "spotify",
			wantID:   snowflake.ID(1328049287640125562),
		},
		{
			name:         "animated custom emoji",
			in:           "<a:tick:810462879374770186>",
			wantName:     "tick",
			wantID:       snowflake.ID(810462879374770186),
			wantAnimated: true,
		},
		{
			name: "plain unicode emoji has no parts",
			in:   "🎧",
		},
		{
			name: "empty string",
			in:   "",
		},
		{
			name: "too few segments",
			in:   "<:spotify>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			name, id, animated := utils.ExtractEmojiParts(tt.in)
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if id != tt.wantID {
				t.Errorf("id = %d, want %d", id, tt.wantID)
			}
			if animated != tt.wantAnimated {
				t.Errorf("animated = %v, want %v", animated, tt.wantAnimated)
			}
		})
	}
}

// A malformed ID used to reach snowflake.MustParse and panic. createButton runs
// this on every song message, so a panic here would take down the handler.
func TestExtractEmojiParts_MalformedIDDoesNotPanic(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		"<:broken:notanumber>",
		"<a:broken:>",
		"<:broken:12notanumber34>",
		"::",
	} {
		name, id, animated := utils.ExtractEmojiParts(in)
		if name != "" || id != 0 || animated {
			t.Errorf("ExtractEmojiParts(%q) = (%q, %d, %v), want zero values",
				in, name, id, animated)
		}
	}
}

func TestHumanize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		xp   int32
		want string
	}{
		{"zero", 0, "0"},
		{"below one thousand is printed plainly", 999, "999"},
		{"exactly one thousand", 1000, "1.00K"},
		{"rounds to two decimals", 1234, "1.23K"},
		{"ten thousand", 10000, "10.00K"},
		{"one million is still reported in thousands", 1_000_000, "1000.00K"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := utils.Humanize(tt.xp); got != tt.want {
				t.Errorf("Humanize(%d) = %q, want %q", tt.xp, got, tt.want)
			}
		})
	}
}

// BUG: the threshold is `xp < 1000`, so negative values take the plain-integer
// branch and are never abbreviated. No caller passes a negative today (XP and
// the level thresholds are both non-negative), so this is pinned, not fixed.
func TestHumanize_NegativeValuesAreNotAbbreviated(t *testing.T) {
	t.Parallel()

	if got := utils.Humanize(-5000); got != "-5000" {
		t.Errorf("Humanize(-5000) = %q, want %q (current behaviour)", got, "-5000")
	}
}
