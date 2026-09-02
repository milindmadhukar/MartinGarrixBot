# STMPD Bot

A multipurpose Discord bot for the STMPD RCRDS community, written in Go with
[disgo](https://github.com/disgoorg/disgo).

![STMPD Bot](https://cdn.discordapp.com/avatars/799613778052382720/62742119975050bb40ab9b3ef9c0b322.png?size=256 "STMPD Bot")

## Features

Music and catalogue search, release and news notifications, a 24/7 radio,
activity levels, a STMPD Coins economy, moderation with audit logging, and a web
dashboard for per-guild configuration. See [FEATURES.md](FEATURES.md) for the
full list.

## Running it

Requires Go 1.25+, PostgreSQL, and a [Lavalink](https://lavalink.dev) node for
audio.

```sh
cp config.example.toml config.toml   # then fill in the required fields
go run . --sync-commands
```

Database migrations run automatically at startup.

### Flags

Bot (`.`):

- `-config` — path to the config file (default `config.toml`)
- `-sync-commands` — register slash commands with Discord
- `-clear-commands` — remove registered slash commands

Dashboard (`./cmd/dashboard`):

- `-config` — path to the config file (default `config.toml`)
- `-dev` — re-parse templates from disk per request

## Dashboard

The dashboard is a separate binary sharing the same config file. It gates access
behind Discord OAuth and is served at `bot.milind.dev` in production.

```sh
make tailwind     # rebuild dashboard/static/app.css (committed, go:embed-ed)
make dashboard    # run locally with -dev
```

## Configuration

TOML, documented inline in [config.example.toml](config.example.toml). It covers
logging, the bot token and API keys, the database, Lavalink, notification
sources (Beatport, Reddit, YouTube), and the dashboard's OAuth credentials.

## Development

```sh
make sqlc              # regenerate db/sqlc from db/query (Docker)
make make_migration    # scaffold a migration pair in db/migrations
make migrate_up        # apply migrations locally
make lint              # golangci-lint
go test ./...          # unit tests
make test-integration  # integration tests; needs STMPD_TEST_DATABASE_URL
```

Maintenance and backfill jobs live in [scripts/](scripts/README.md) as separate
binaries rather than flags on the bot.

## Deployment

Pushes to `main` publish two images to GHCR, which the host picks up
automatically:

- `ghcr.io/milindmadhukar/stmpdbot:main`
- `ghcr.io/milindmadhukar/stmpdbot-dashboard:main`

`docker-compose.yml` runs the bot, the dashboard, and Lavalink behind Traefik.

## License

[Apache 2.0](LICENSE)
