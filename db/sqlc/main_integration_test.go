//go:build integration

package db_test

// Integration harness. Build-tagged so `go test ./...` stays hermetic and fast;
// run these with `make test-integration`, or:
//
//	STMPD_TEST_DATABASE_URL=postgres://postgres:password@localhost:5432/stmpdbot_test?sslmode=disable \
//	  go test -tags integration -count=1 ./db/...
//
// The schema comes from the real migrations in db/migrations, so these exercise
// the queries against the schema that actually ships rather than a hand-written
// approximation.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	migratePgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

// testPool is shared by every test in the package; each one cleans up the rows
// it creates rather than the whole database, so they can run in parallel.
var testPool *pgxpool.Pool

const databaseURLEnv = "STMPD_TEST_DATABASE_URL"

func TestMain(m *testing.M) {
	databaseURL := os.Getenv(databaseURLEnv)
	if databaseURL == "" {
		fmt.Fprintf(os.Stderr,
			"skipping integration tests: %s is not set\n", databaseURLEnv)
		os.Exit(0)
	}

	if err := run(m, databaseURL); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(m *testing.M, databaseURL string) error {
	ctx := context.Background()

	// Migrate first, on a connection of its own. Deriving the migration's
	// *sql.DB from the pgxpool instead makes pool.Close() block forever waiting
	// for a connection database/sql has not handed back.
	if err := applyMigrations(databaseURL); err != nil {
		return err
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", databaseURLEnv, err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("pinging the test database: %w", err)
	}

	testPool = pool

	if code := m.Run(); code != 0 {
		os.Exit(code)
	}
	return nil
}

// applyMigrations brings the test database up to date using the same driver and
// migration directory the bot itself uses in SetupDB.
func applyMigrations(databaseURL string) error {
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("opening a migration connection: %w", err)
	}
	defer sqlDB.Close()

	driver, err := migratePgx.WithInstance(sqlDB, &migratePgx.Config{})
	if err != nil {
		return fmt.Errorf("building the migration driver: %w", err)
	}

	// Tests run with the package directory as the working directory.
	m, err := migrate.NewWithDatabaseInstance("file://../migrations", "postgres", driver)
	if err != nil {
		return fmt.Errorf("loading migrations: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running migrations: %w", err)
	}
	return nil
}

// queries returns a Queries bound to the shared pool.
func queries(t *testing.T) *db.Queries {
	t.Helper()

	if testPool == nil {
		t.Fatalf("no test database; set %s", databaseURLEnv)
	}
	return db.New(testPool)
}
