package mgbot

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/disgoorg/snowflake/v2"
	"github.com/pelletier/go-toml/v2"
)

func LoadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config: %w", err)
	}
	defer file.Close()

	var cfg Config
	if err = toml.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

type Config struct {
	Log      LogConfig      `toml:"log"`
	Bot      BotConfig      `toml:"bot"`
	Lavalink LavalinkConfig `toml:"lavalink"`
	DB       DatabaseConfig `toml:"database"`
	Health   HealthConfig   `toml:"health"`
	// Internal is read by the bot, Dashboard by the dashboard binary. Both
	// live in the same file so there is one config format and one
	// LoadConfig; each process simply ignores the section it does not own.
	Internal  InternalConfig  `toml:"internal"`
	Dashboard DashboardConfig `toml:"dashboard"`
}

type BotConfig struct {
	DevGuilds []snowflake.ID `toml:"dev_guilds"`
	Token     string         `toml:"token"`
	// The tag has to match what the config files actually say. It read
	// "youtube_api_key" while config.toml, config.example.toml and the deployed
	// config.docker.toml all write "yt_api_key", so this field was empty in
	// production and the YouTube service ran on the service-account file alone.
	YoutubeAPIKey      string   `toml:"yt_api_key"`
	GoogleServiceFile  string   `toml:"google_service_file"`
	RedditClientID     string   `toml:"reddit_client_id"`
	RedditClientSecret string   `toml:"reddit_client_secret"`
	RedditBotUsername  string   `toml:"reddit_bot_username"`
	RedditBotPassword  string   `toml:"reddit_bot_password"`
	BeatportUsername   string   `toml:"beatport_username"`
	BeatportPassword   string   `toml:"beatport_password"`
	BeatportLabelID    string   `toml:"beatport_label_id"`
	BeatportArtistIDs  []string `toml:"beatport_artist_ids"`
	BeatportMaxTracks  int      `toml:"beatport_max_tracks"`
}

type LogConfig struct {
	Level      slog.Level `toml:"level"`
	Format     string     `toml:"format"`
	AddSource  bool       `toml:"add_source"`
	File       string     `toml:"file"`
	MaxSize    int        `toml:"max_size"`
	MaxAge     int        `toml:"max_age"`
	MaxBackups int        `toml:"max_backups"`
	TimeZone   string     `toml:"timezone"`
}

type LavalinkConfig struct {
	URL      string `toml:"url"`
	Password string `toml:"password"`
}

// HealthConfig controls the /health endpoint used by the container HEALTHCHECK
// and by external uptime monitoring. Address is a net/http listen address;
// it defaults to ":8081" when left empty.
type HealthConfig struct {
	Address string `toml:"address"`
}

type DatabaseConfig struct {
	Host     string `toml:"host"`
	User     string `toml:"user"`
	Password string `toml:"password"`
	Name     string `toml:"name"`
	Port     int    `toml:"port"`
}

func (d *DatabaseConfig) URI() string {
	uri := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", d.User, d.Password, d.Host, d.Port, d.Name)
	return uri
}

// InternalConfig controls the bot's internal API, which serves live guild data
// (roles, channels, member counts) to the dashboard from the disgo cache so the
// bot token never has to leave this process.
//
// This is a separate listener from the health server on purpose: /health is
// deliberately unauthenticated and is hit by the container HEALTHCHECK, and
// mixing authenticated routes onto that port makes it easy to expose them by
// accident the day someone publishes 8081 to the host.
type InternalConfig struct {
	// Address defaults to ":8082" when empty. Container-internal only; it
	// must never be published to the host.
	Address string `toml:"address"`
	// Secret is presented by the dashboard in the X-Internal-Token header.
	// The API refuses to start when it is empty rather than serving guild
	// data to anything that can reach the port.
	Secret string `toml:"secret"`
}

// DashboardConfig is read only by the dashboard binary (cmd/dashboard).
type DashboardConfig struct {
	// Address defaults to ":8080" when empty.
	Address string `toml:"address"`
	// PublicBaseURL is the externally reachable origin, used to decide
	// whether session cookies get the Secure flag.
	PublicBaseURL string `toml:"public_base_url"`

	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
	// RedirectURI has to match a redirect registered on the Discord
	// application byte for byte or the OAuth exchange fails.
	RedirectURI string `toml:"redirect_uri"`

	// SessionSecret keys the HMAC over the session cookie. At least 32 bytes.
	SessionSecret string `toml:"session_secret"`

	// BotAPIURL points at the bot's InternalConfig.Address, and BotAPISecret
	// must equal InternalConfig.Secret.
	BotAPIURL    string `toml:"bot_api_url"`
	BotAPISecret string `toml:"bot_api_secret"`

	// OwnerIDs may administer every guild the bot is in, regardless of their
	// Discord permissions there.
	OwnerIDs []snowflake.ID `toml:"owner_ids"`
}
