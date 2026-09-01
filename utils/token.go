package utils

import "time"

// TokenRefreshMargin is how long before expiry a token is treated as stale.
// Refreshing early avoids a request racing the expiry and coming back 401.
const TokenRefreshMargin = 5 * time.Minute

// NeedsRefresh reports whether an OAuth token has to be fetched again. An empty
// token always does, as does one inside margin of expiring.
//
// Shared by the Reddit and Beatport clients, which both cache a token on a
// long-lived process: authenticating only at startup leaves the bot silently
// unauthenticated once the token lapses.
func NeedsRefresh(token string, expiry, now time.Time, margin time.Duration) bool {
	if token == "" {
		return true
	}
	return !now.Before(expiry.Add(-margin))
}
