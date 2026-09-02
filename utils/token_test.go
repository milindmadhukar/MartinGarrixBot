package utils_test

import (
	"testing"
	"time"

	"github.com/milindmadhukar/STMPDBot/utils"
)

func TestNeedsRefresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	const margin = 5 * time.Minute

	tests := []struct {
		name   string
		token  string
		expiry time.Time
		want   bool
	}{
		{
			name:   "an empty token always needs refreshing",
			token:  "",
			expiry: now.Add(time.Hour),
			want:   true,
		},
		{
			name:   "an empty token with no expiry at all",
			token:  "",
			expiry: time.Time{},
			want:   true,
		},
		{
			name:   "a token well inside its lifetime is kept",
			token:  "abc",
			expiry: now.Add(time.Hour),
			want:   false,
		},
		{
			name:   "a token just outside the margin is kept",
			token:  "abc",
			expiry: now.Add(margin + time.Second),
			want:   false,
		},
		{
			// Refreshing exactly on the margin rather than one instant later
			// keeps the boundary from depending on clock resolution.
			name:   "a token exactly on the margin is refreshed",
			token:  "abc",
			expiry: now.Add(margin),
			want:   true,
		},
		{
			name:   "a token inside the margin is refreshed",
			token:  "abc",
			expiry: now.Add(time.Minute),
			want:   true,
		},
		{
			name:   "a token expiring right now is refreshed",
			token:  "abc",
			expiry: now,
			want:   true,
		},
		{
			name:   "an expired token is refreshed",
			token:  "abc",
			expiry: now.Add(-time.Hour),
			want:   true,
		},
		{
			// The zero time is what an unset expiry looks like on a struct that
			// was never populated; it must not read as valid forever.
			name:   "a zero expiry is refreshed",
			token:  "abc",
			expiry: time.Time{},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := utils.NeedsRefresh(tt.token, tt.expiry, now, margin)
			if got != tt.want {
				t.Errorf("NeedsRefresh(%q, %s, %s, %s) = %v, want %v",
					tt.token, tt.expiry, now, margin, got, tt.want)
			}
		})
	}
}

// A long-lived process must re-authenticate as tokens lapse; Reddit's last 24
// hours, so a bot that only authenticated at startup would go quiet after a day.
func TestNeedsRefresh_OverATokenLifetime(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	expiry := start.Add(24 * time.Hour)
	const margin = 5 * time.Minute

	// Checked hourly, it should hold until the final margin.
	for hour := range 24 {
		now := start.Add(time.Duration(hour) * time.Hour)
		if utils.NeedsRefresh("token", expiry, now, margin) {
			t.Fatalf("token was refreshed early, %d hours in", hour)
		}
	}

	if !utils.NeedsRefresh("token", expiry, expiry.Add(-time.Minute), margin) {
		t.Error("token was not refreshed inside the margin")
	}
	if !utils.NeedsRefresh("token", expiry, expiry.Add(time.Second), margin) {
		t.Error("an expired token was not refreshed")
	}
}

func TestTokenRefreshMargin(t *testing.T) {
	t.Parallel()

	if utils.TokenRefreshMargin != 5*time.Minute {
		t.Errorf("TokenRefreshMargin = %s, want 5m; both the Reddit and Beatport "+
			"clients depend on this", utils.TokenRefreshMargin)
	}
}
