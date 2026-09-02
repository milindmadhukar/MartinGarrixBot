package commands

// In-package: parseDuration is unexported.

import (
	"strings"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"seconds", "30s", 30 * time.Second},
		{"minutes", "15m", 15 * time.Minute},
		{"hours", "2h", 2 * time.Hour},
		{"days", "7d", 7 * 24 * time.Hour},
		{"weeks", "2w", 2 * 7 * 24 * time.Hour},
		{"one of a unit", "1h", time.Hour},
		{"a large value", "999d", 999 * 24 * time.Hour},
		{"uppercase is accepted", "2H", 2 * time.Hour},
		{"surrounding whitespace is trimmed", "  3d  ", 3 * 24 * time.Hour},
		{"multiple digits", "120m", 120 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseDuration(tt.in)
			if err != nil {
				t.Fatalf("parseDuration(%q) returned an unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseDuration_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		in              string
		wantErrContains string
	}{
		{"empty", "", "invalid duration format"},
		{"a unit with no value", "h", "invalid duration format"},
		{"a single digit with no unit", "5", "invalid duration format"},
		{"whitespace only", "   ", "invalid duration format"},
		{"an unsupported unit", "5y", "invalid duration unit"},
		{"a unit that is not a letter", "5!", "invalid duration unit"},
		{"a non-numeric value", "abcd", "invalid duration value"},
		{"a decimal value", "1.5h", "invalid duration value"},
		{"zero is rejected", "0h", "duration must be positive"},
		{"a negative value is rejected", "-5h", "duration must be positive"},
		// The value is parsed before the unit is validated, so a reversed input
		// is reported as a bad value rather than a bad unit.
		{"the unit comes first", "h5", "invalid duration value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseDuration(tt.in)
			if err == nil {
				t.Fatalf("parseDuration(%q) returned no error, want one containing %q",
					tt.in, tt.wantErrContains)
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Errorf("parseDuration(%q) error = %q, want it to contain %q",
					tt.in, err, tt.wantErrContains)
			}
		})
	}
}

// BUG: the unit is taken as the final byte rather than the final rune, so a
// multi-byte suffix is split mid-character. The value half then fails to parse,
// which at least means it is rejected rather than misread. Recorded so the
// behaviour is visible if the parser is ever rewritten.
func TestParseDuration_MultiByteSuffixIsRejected(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"5ह", "5日", "5€"} {
		if _, err := parseDuration(in); err == nil {
			t.Errorf("parseDuration(%q) returned no error; a multi-byte suffix "+
				"should still be rejected", in)
		}
	}
}
