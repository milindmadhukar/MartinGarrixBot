// Command dashboard serves the web dashboard for MartinGarrixBot.
//
// It is a separate binary from the bot but lives in the same repository so both
// compile against one copy of the db package and can never drift on schema. It
// reads the same TOML config file, which is what keeps the database credentials
// and the internal-API shared secret defined exactly once.
//
// It deliberately does NOT run migrations. The bot owns the schema; two
// processes racing golang-migrate on the same schema_migrations table means one
// gets a dirty-version error, and since both images are redeployed
// independently there is no ordering guarantee about which would win.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/milindmadhukar/MartinGarrixBot/dashboard"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
)

func main() {
	configPath := flag.String("config", "config.toml", "path to config")
	dev := flag.Bool("dev", false, "re-parse templates from disk on every request")
	flag.Parse()

	cfg, err := mgbot.LoadConfig(*configPath)
	if err != nil {
		slog.Error("Failed to read config", slog.Any("err", err))
		os.Exit(1)
	}

	// Shares the bot's logger setup, which also sets time.Local from
	// [log].timezone -- so dashboard timestamps and bot log timestamps agree.
	mgbot.SetupLogger(cfg.Log)

	opts, err := dashboard.NewOptions(cfg)
	if err != nil {
		slog.Error("Invalid dashboard configuration", slog.Any("err", err))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := newPool(ctx, cfg.DB.URI())
	if err != nil {
		slog.Error("Failed to connect to database", slog.Any("err", err))
		os.Exit(1)
	}
	defer pool.Close()

	server, err := dashboard.NewServer(opts, pool, *dev)
	if err != nil {
		slog.Error("Failed to build dashboard", slog.Any("err", err))
		os.Exit(1)
	}

	slog.Info("Dashboard starting",
		slog.String("addr", opts.Address),
		slog.String("public_base_url", opts.PublicBaseURL),
		slog.Bool("secure_cookies", opts.Secure),
		slog.Bool("bot_api", opts.BotAPIURL != ""),
		slog.String("timezone", opts.TimeZone))

	if err := server.ListenAndServe(ctx); err != nil {
		slog.Error("Dashboard stopped", slog.Any("err", err))
		os.Exit(1)
	}
	slog.Info("Dashboard stopped")
}

// newPool mirrors the bot's SetupDB retry loop: the database container and the
// dashboard start together under compose, so the first few connections
// routinely lose the race.
func newPool(ctx context.Context, uri string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, uri)
	if err != nil {
		return nil, err
	}

	const attempts = 5
	for i := range attempts {
		if err = pool.Ping(ctx); err == nil {
			return pool, nil
		}
		if ctx.Err() != nil {
			pool.Close()
			return nil, ctx.Err()
		}
		slog.Warn("Database not ready, retrying",
			slog.Int("attempt", i+1), slog.Any("err", err))
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}

	pool.Close()
	return nil, err
}
