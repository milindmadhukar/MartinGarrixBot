package db_test

// External package on purpose: sqlc regenerates *.sql.go, db.go, models.go and
// querier.go, so this file is obviously not one of its outputs and survives
// `make sqlc`.

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

func TestErrorCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"no error", nil, ""},
		{"an unrelated error", errors.New("boom"), ""},
		{"no rows is not a pg error code", pgx.ErrNoRows, ""},
		{
			name: "a unique violation",
			err:  &pgconn.PgError{Code: db.UniqueViolation},
			want: db.UniqueViolation,
		},
		{
			name: "a foreign key violation",
			err:  &pgconn.PgError{Code: db.ForeignKeyViolation},
			want: db.ForeignKeyViolation,
		},
		{
			// The handlers wrap driver errors before checking them, so unwrapping
			// has to work or every conflict would look like an unknown failure.
			name: "a wrapped pg error is still found",
			err:  fmt.Errorf("inserting song: %w", &pgconn.PgError{Code: db.UniqueViolation}),
			want: db.UniqueViolation,
		},
		{
			name: "a doubly wrapped pg error is still found",
			err: fmt.Errorf("outer: %w",
				fmt.Errorf("inner: %w", &pgconn.PgError{Code: db.ForeignKeyViolation})),
			want: db.ForeignKeyViolation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := db.ErrorCode(tt.err); got != tt.want {
				t.Errorf("ErrorCode(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// The constants must stay equal to the Postgres SQLSTATE values they name,
// since that is what the driver reports.
func TestViolationCodes(t *testing.T) {
	t.Parallel()

	if db.UniqueViolation != "23505" {
		t.Errorf("UniqueViolation = %q, want the SQLSTATE 23505", db.UniqueViolation)
	}
	if db.ForeignKeyViolation != "23503" {
		t.Errorf("ForeignKeyViolation = %q, want the SQLSTATE 23503", db.ForeignKeyViolation)
	}
}

func TestErrRecordNotFound(t *testing.T) {
	t.Parallel()

	// Callers compare query errors against this, so it has to stay the sentinel
	// pgx actually returns.
	if !errors.Is(db.ErrRecordNotFound, pgx.ErrNoRows) {
		t.Error("ErrRecordNotFound no longer matches pgx.ErrNoRows")
	}
	if !errors.Is(fmt.Errorf("looking up song: %w", pgx.ErrNoRows), db.ErrRecordNotFound) {
		t.Error("a wrapped pgx.ErrNoRows is not matched by ErrRecordNotFound")
	}
}
