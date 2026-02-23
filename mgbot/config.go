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
}

type BotConfig struct {
	DevGuilds          []snowflake.ID `toml:"dev_guilds"`
	Token              string         `toml:"token"`
	YoutubeAPIKey      string         `toml:"youtube_api_key"`
	GoogleServiceFile  string         `toml:"google_service_file"`
	RedditClientID     string         `toml:"reddit_client_id"`
	RedditClientSecret string         `toml:"reddit_client_secret"`
	RedditBotUsername  string         `toml:"reddit_bot_username"`
	RedditBotPassword  string         `toml:"reddit_bot_password"`
	BeatportUsername   string         `toml:"beatport_username"`
	BeatportPassword   string         `toml:"beatport_password"`
	BeatportLabelID    string         `toml:"beatport_label_id"`
	BeatportArtistIDs  []string       `toml:"beatport_artist_ids"`
	BeatportMaxTracks  int            `toml:"beatport_max_tracks"`
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
