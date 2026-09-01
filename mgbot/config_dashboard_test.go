package mgbot

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigDashboardSections pins how the two new sections decode.
//
// owner_ids is []snowflake.ID, and snowflake.ID implements only MarshalJSON --
// no encoding.TextUnmarshaler -- so go-toml decodes it from a bare TOML
// integer, not a quoted string. Getting that wrong means the deployed config
// fails to parse at startup, which is exactly the sort of thing worth pinning
// once rather than discovering on the box.
func TestLoadConfigDashboardSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	const contents = `
[database]
host = "localhost"
user = "postgres"
password = "pw"
name = "garrixbot"
port = 5432

[internal]
address = ":8082"
secret = "internal-secret"

[dashboard]
address = ":8080"
public_base_url = "https://bot.milind.dev"
client_id = "799613778052382720"
client_secret = "client-secret"
redirect_uri = "https://bot.milind.dev/auth/callback"
session_secret = "0123456789abcdef0123456789abcdef"
bot_api_url = "http://martingarrixbot:8082"
bot_api_secret = "internal-secret"
owner_ids = [522436981913944096, 421608483629301772]
`

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Internal.Address != ":8082" || cfg.Internal.Secret != "internal-secret" {
		t.Errorf("[internal] decoded as %+v", cfg.Internal)
	}
	if cfg.Dashboard.PublicBaseURL != "https://bot.milind.dev" {
		t.Errorf("public_base_url = %q", cfg.Dashboard.PublicBaseURL)
	}
	if cfg.Dashboard.BotAPISecret != cfg.Internal.Secret {
		t.Error("the two halves of the shared secret should match in this fixture")
	}

	if len(cfg.Dashboard.OwnerIDs) != 2 {
		t.Fatalf("owner_ids decoded to %d entries: %v", len(cfg.Dashboard.OwnerIDs), cfg.Dashboard.OwnerIDs)
	}
	if got := cfg.Dashboard.OwnerIDs[0].String(); got != "522436981913944096" {
		t.Errorf("owner_ids[0] = %s", got)
	}
	if got := cfg.Dashboard.OwnerIDs[1].String(); got != "421608483629301772" {
		t.Errorf("owner_ids[1] = %s", got)
	}
}

// TestLoadConfigWithoutDashboardSections: the bot must keep starting on a
// config that predates the dashboard entirely.
func TestLoadConfigWithoutDashboardSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(path, []byte("[bot]\ntoken = \"x\"\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Internal.Secret != "" {
		t.Error("an absent [internal] section should leave the secret empty, disabling the API")
	}
	if len(cfg.Dashboard.OwnerIDs) != 0 {
		t.Error("an absent [dashboard] section should leave owner_ids empty")
	}
}
