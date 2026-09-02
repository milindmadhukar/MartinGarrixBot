// Package script holds the plumbing shared by the one-off maintenance commands in
// scripts/.
//
// These are deliberately separate binaries rather than flags on the bot. A backfill
// rewrites release dates and deletes merged duplicates; that is not something that
// should be reachable from a process which runs unattended for weeks. Keeping them
// out of the bot also means they cannot import the notifier, so a maintenance pass
// announcing the back catalogue to Discord is structurally impossible rather than
// merely guarded against.
package script

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
)

// Env is everything a maintenance script needs: a database handle and whether it is
// allowed to write.
//
// Under -dry-run, Queries is bound to a transaction that is rolled back on cleanup
// instead of to the pool. Writes therefore execute for real -- constraints fire,
// no-op updates report zero rows, duplicate inserts are rejected -- and then vanish.
// A dry run that merely skips the writes cannot tell you any of that: it reported
// 1013 rows written where the real run wrote none, and inserts the database would
// have refused. The point of a dry run is to be believed.
type Env struct {
	Pool    *pgxpool.Pool
	Queries *db.Queries
	Config  *stmpdbot.Config
	DryRun  bool
}

// Setup parses the standard flags, connects to the database named by the config, and
// returns the environment. Config comes from the same TOML the bot reads so there is
// never a second copy of the database credentials to keep in step.
func Setup(name string) (*Env, context.Context, func()) {
	configPath := flag.String("config", "config.toml", "path to the bot's TOML config")
	dryRun := flag.Bool("dry-run", false, "report what would change without writing anything")
	timeout := flag.Duration("timeout", 30*time.Minute, "overall timeout for the run")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := stmpdbot.LoadConfig(*configPath)
	if err != nil {
		fatal("failed to load config", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)

	pool, err := pgxpool.New(ctx, cfg.DB.URI())
	if err != nil {
		cancel()
		fatal("failed to create connection pool", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		cancel()
		fatal("failed to reach the database", err)
	}

	slog.Info("Starting maintenance script",
		slog.String("script", name),
		slog.String("database", fmt.Sprintf("%s:%d/%s", cfg.DB.Host, cfg.DB.Port, cfg.DB.Name)),
		slog.Bool("dry_run", *dryRun))

	if *dryRun {
		slog.Warn("DRY RUN: no changes will be written")
	}

	env := &Env{Pool: pool, Queries: db.New(pool), Config: cfg, DryRun: *dryRun}

	release := func() {
		pool.Close()
		cancel()
	}

	if *dryRun {
		tx, err := pool.Begin(ctx)
		if err != nil {
			release()
			fatal("failed to open the dry-run transaction", err)
		}
		env.Queries = db.New(tx)
		release = func() {
			// Rollback is the whole mechanism, so a failure to roll back is not
			// something to swallow: it would mean a "dry" run had committed.
			if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				slog.Error("DRY RUN FAILED TO ROLL BACK - inspect the database", slog.Any("err", err))
			} else {
				slog.Info("Dry run rolled back; nothing was written")
			}
			pool.Close()
			cancel()
		}
	}

	return env, ctx, release
}

func fatal(msg string, err error) {
	slog.Error(msg, slog.Any("err", err))
	os.Exit(1)
}

// Fatal aborts the script with a message. Scripts are run by hand and their exit
// code is the signal, so there is no point continuing past a failure.
func Fatal(msg string, err error) { fatal(msg, err) }
