package dashboard

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/milindmadhukar/STMPDBot/stmpdbot"
)

// ErrBotAPIUnavailable marks every failure to reach the bot's internal API.
// Handlers check for it to decide between the degradation banner and a real
// error page.
var ErrBotAPIUnavailable = errors.New("bot api unavailable")

// Defaults applied when the corresponding TOML key is empty.
const (
	defaultAddress       = ":8080"
	defaultSessionTTL    = 7 * 24 * time.Hour
	defaultGuildCacheTTL = 10 * time.Minute
	defaultBotCacheTTL   = 30 * time.Second
	// defaultWindowDays is how far back the metric panels look by default.
	defaultWindowDays = 30
	// minSessionSecretLen is the shortest secret that makes the HMAC worth
	// anything.
	minSessionSecretLen = 32
)

// Options is the validated, defaulted form of stmpdbot.DashboardConfig.
type Options struct {
	Address       string
	PublicBaseURL string
	ClientID      string
	ClientSecret  string
	RedirectURI   string
	SessionSecret string
	BotAPIURL     string
	BotAPISecret  string
	SuperAdminIDs map[string]bool

	// BackgroundsDir is where rank card background images live, shared with
	// the bot via storage.backgrounds_dir. Defaults to "assets/backgrounds".
	BackgroundsDir string

	SessionTTL    time.Duration
	GuildCacheTTL time.Duration
	BotCacheTTL   time.Duration

	// Secure controls the Secure flag and the __Host- cookie prefix. Derived
	// from PublicBaseURL rather than configured separately, so a config that
	// says https cannot accidentally ship insecure cookies.
	Secure bool

	// TimeZone names the zone metric buckets are cut in. Taken from the bot's
	// [log] section so charts and log timestamps agree.
	TimeZone string
	Location *time.Location
}

// NewOptions validates the dashboard configuration, reporting every problem at
// once rather than one per run.
func NewOptions(cfg *stmpdbot.Config) (*Options, error) {
	d := cfg.Dashboard

	var problems []string
	require := func(value, name string) {
		if strings.TrimSpace(value) == "" {
			problems = append(problems, "dashboard."+name+" is required")
		}
	}
	require(d.ClientID, "client_id")
	require(d.ClientSecret, "client_secret")
	require(d.RedirectURI, "redirect_uri")
	require(d.SessionSecret, "session_secret")
	require(d.PublicBaseURL, "public_base_url")

	if n := len(d.SessionSecret); n > 0 && n < minSessionSecretLen {
		problems = append(problems, fmt.Sprintf(
			"dashboard.session_secret must be at least %d characters (got %d); generate one with `openssl rand -hex 32`",
			minSessionSecretLen, n))
	}

	// The two halves of the shared secret live in different TOML sections and
	// are trivially easy to set on only one side. Catching it here beats
	// debugging a dashboard whose dropdowns are silently empty.
	if d.BotAPIURL != "" && d.BotAPISecret == "" {
		problems = append(problems, "dashboard.bot_api_secret is required when bot_api_url is set")
	}
	if d.BotAPISecret != "" && cfg.Internal.Secret != "" && d.BotAPISecret != cfg.Internal.Secret {
		problems = append(problems, "dashboard.bot_api_secret does not match internal.secret")
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}

	superAdmins := make(map[string]bool, len(d.SuperAdminIDs))
	for _, id := range d.SuperAdminIDs {
		superAdmins[id.String()] = true
	}

	tz := cfg.Log.TimeZone
	if tz == "" {
		tz = "Asia/Kolkata"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		// Matches SetupLogger's posture: a bad zone name degrades to UTC with
		// a clear signal rather than taking the process down.
		tz, loc = "UTC", time.UTC
	}

	opts := &Options{
		Address:        or(d.Address, defaultAddress),
		PublicBaseURL:  strings.TrimRight(d.PublicBaseURL, "/"),
		ClientID:       d.ClientID,
		ClientSecret:   d.ClientSecret,
		RedirectURI:    d.RedirectURI,
		SessionSecret:  d.SessionSecret,
		BotAPIURL:      d.BotAPIURL,
		BotAPISecret:   d.BotAPISecret,
		SuperAdminIDs:  superAdmins,
		BackgroundsDir: or(cfg.Storage.BackgroundsDir, "assets/backgrounds"),
		SessionTTL:     defaultSessionTTL,
		GuildCacheTTL:  defaultGuildCacheTTL,
		BotCacheTTL:    defaultBotCacheTTL,
		TimeZone:       tz,
		Location:       loc,
	}
	opts.Secure = strings.HasPrefix(opts.PublicBaseURL, "https://")
	return opts, nil
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
