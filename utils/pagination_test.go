package utils_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

func text(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}

func timestamp(tm time.Time) pgtype.Timestamp {
	return pgtype.Timestamp{Time: tm, Valid: true}
}

func boolean(b bool) pgtype.Bool {
	return pgtype.Bool{Bool: b, Valid: true}
}

func TestCalculateTotalPages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		totalItems   int
		itemsPerPage int
		want         int
	}{
		{"no items", 0, 5, 0},
		{"fewer items than one page", 3, 5, 1},
		{"exactly one page", 5, 5, 1},
		{"one over a page rounds up", 6, 5, 2},
		{"exactly two pages", 10, 5, 2},
		{"partial final page", 11, 5, 3},
		{"single item per page", 7, 1, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.CalculateTotalPages(tt.totalItems, tt.itemsPerPage)
			if got != tt.want {
				t.Errorf("CalculateTotalPages(%d, %d) = %d, want %d",
					tt.totalItems, tt.itemsPerPage, got, tt.want)
			}
		})
	}
}

// A page size of zero used to compute int(math.Ceil(+Inf)), which the Go spec
// leaves undefined.
func TestCalculateTotalPages_NonPositivePageSize(t *testing.T) {
	t.Parallel()

	for _, perPage := range []int{0, -1, -10} {
		if got := utils.CalculateTotalPages(100, perPage); got != 0 {
			t.Errorf("CalculateTotalPages(100, %d) = %d, want 0", perPage, got)
		}
	}
}

// Cross-check the integer form against the arithmetic it replaced, for every
// combination that matters.
func TestCalculateTotalPages_MatchesCeilingDivision(t *testing.T) {
	t.Parallel()

	for total := 0; total <= 200; total++ {
		for perPage := 1; perPage <= 20; perPage++ {
			want := (total + perPage - 1) / perPage
			if got := utils.CalculateTotalPages(total, perPage); got != want {
				t.Fatalf("CalculateTotalPages(%d, %d) = %d, want %d",
					total, perPage, got, want)
			}
		}
	}
}

func TestFormatModlogEntry(t *testing.T) {
	t.Parallel()

	when := time.Date(2026, 3, 14, 15, 9, 26, 0, time.UTC)

	t.Run("a complete entry", func(t *testing.T) {
		t.Parallel()

		got := utils.FormatModlogEntry(db.Modlog{
			ID:          42,
			LogType:     "ban",
			ModeratorID: 111222333,
			Reason:      text("spamming"),
			Time:        timestamp(when),
		}, 3)

		for _, want := range []string{
			"**3.** ban | Case #42",
			"• Moderator: <@111222333>",
			"• Reason: spamming",
			fmt.Sprintf("• Time: <t:%d:F>", when.Unix()),
		} {
			if !strings.Contains(got, want) {
				t.Errorf("entry is missing %q\ngot:\n%s", want, got)
			}
		}
	})

	t.Run("a missing reason falls back", func(t *testing.T) {
		t.Parallel()

		got := utils.FormatModlogEntry(db.Modlog{ID: 1, LogType: "kick"}, 1)
		if !strings.Contains(got, "• Reason: No reason provided") {
			t.Errorf("expected the fallback reason\ngot:\n%s", got)
		}
	})

	t.Run("a missing timestamp falls back", func(t *testing.T) {
		t.Parallel()

		got := utils.FormatModlogEntry(db.Modlog{ID: 1, LogType: "kick"}, 1)
		if !strings.Contains(got, "• Time: Unknown") {
			t.Errorf("expected the fallback timestamp\ngot:\n%s", got)
		}
	})

	t.Run("an active temporary action shows when it expires", func(t *testing.T) {
		t.Parallel()

		expires := when.Add(24 * time.Hour)
		got := utils.FormatModlogEntry(db.Modlog{
			ID:        7,
			LogType:   "tempban",
			Time:      timestamp(when),
			ExpiresAt: timestamp(expires),
			Active:    boolean(true),
		}, 1)

		want := fmt.Sprintf("• Expires: <t:%d:R>", expires.Unix())
		if !strings.Contains(got, want) {
			t.Errorf("entry is missing %q\ngot:\n%s", want, got)
		}
	})

	t.Run("an inactive temporary action shows as expired", func(t *testing.T) {
		t.Parallel()

		got := utils.FormatModlogEntry(db.Modlog{
			ID:        7,
			LogType:   "tempban",
			Time:      timestamp(when),
			ExpiresAt: timestamp(when.Add(24 * time.Hour)),
			Active:    boolean(false),
		}, 1)

		if !strings.Contains(got, "• Status: Expired/Deactivated") {
			t.Errorf("expected the expired status\ngot:\n%s", got)
		}
	})

	t.Run("a permanent action shows no expiry line", func(t *testing.T) {
		t.Parallel()

		got := utils.FormatModlogEntry(db.Modlog{ID: 7, LogType: "ban", Time: timestamp(when)}, 1)
		if strings.Contains(got, "Expires") || strings.Contains(got, "Status:") {
			t.Errorf("did not expect an expiry line\ngot:\n%s", got)
		}
	})
}

func TestCreateModlogEmbed(t *testing.T) {
	t.Parallel()

	t.Run("no logs", func(t *testing.T) {
		t.Parallel()

		embed := utils.CreateModlogEmbed(nil, 555, 1, 1)
		if embed.Description != "No moderation logs found for this user." {
			t.Errorf("description = %q, want the empty-state message", embed.Description)
		}
		if !strings.Contains(embed.Title, "555") {
			t.Errorf("title = %q, want it to mention the user", embed.Title)
		}
	})

	t.Run("numbering continues across pages", func(t *testing.T) {
		t.Parallel()

		logs := []db.Modlog{
			{ID: 10, LogType: "ban"},
			{ID: 11, LogType: "kick"},
		}

		// Page 2 with 5 per page starts at entry 6.
		embed := utils.CreateModlogEmbed(logs, 555, 2, 3)
		if !strings.Contains(embed.Description, "**6.** ban") {
			t.Errorf("expected the first entry on page 2 to be numbered 6\ngot:\n%s",
				embed.Description)
		}
		if !strings.Contains(embed.Description, "**7.** kick") {
			t.Errorf("expected the second entry on page 2 to be numbered 7\ngot:\n%s",
				embed.Description)
		}
		if embed.Footer == nil || embed.Footer.Text != "Page 2 of 3" {
			t.Errorf("footer = %+v, want \"Page 2 of 3\"", embed.Footer)
		}
	})
}

func TestCreatePaginationButtons(t *testing.T) {
	t.Parallel()

	t.Run("a single page needs no navigation", func(t *testing.T) {
		t.Parallel()

		for _, totalPages := range []int{0, 1} {
			got := utils.CreatePaginationButtons(1, totalPages, "modlogs:123")
			if len(got) != 0 {
				t.Errorf("CreatePaginationButtons(1, %d, ...) returned %d rows, want none",
					totalPages, len(got))
			}
		}
	})

	t.Run("the custom ID format is the contract a component handler must parse",
		func(t *testing.T) {
			t.Parallel()

			// The custom ID is a router path: commands.go registers
			// "/modlogs/{userID}/{action}/{page}" against these buttons, so the
			// separator has to be "/" for the handler to match. Colons made every
			// click fail with "This interaction failed".
			buttons := actionRowButtons(t, utils.CreatePaginationButtons(2, 5, "/modlogs/123"))

			wantIDs := []string{
				"/modlogs/123/first/2",
				"/modlogs/123/prev/2",
				"/modlogs/123/current/2",
				"/modlogs/123/next/2",
				"/modlogs/123/last/2",
			}
			if len(buttons) != len(wantIDs) {
				t.Fatalf("got %d buttons, want %d", len(buttons), len(wantIDs))
			}
			for i, want := range wantIDs {
				if buttons[i].CustomID != want {
					t.Errorf("button %d custom ID = %q, want %q", i, buttons[i].CustomID, want)
				}
			}
		})

	t.Run("boundary buttons are disabled", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name        string
			currentPage int
			totalPages  int
			wantFirst   bool // whether "first"/"prev" are disabled
			wantLast    bool // whether "next"/"last" are disabled
		}{
			{"on the first page", 1, 5, true, false},
			{"in the middle", 3, 5, false, false},
			{"on the last page", 5, 5, false, true},
			{"two pages, first", 1, 2, true, false},
			{"two pages, last", 2, 2, false, true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				buttons := actionRowButtons(t, utils.CreatePaginationButtons(
					tt.currentPage, tt.totalPages, "modlogs:1"))
				if len(buttons) != 5 {
					t.Fatalf("got %d buttons, want 5", len(buttons))
				}

				if buttons[0].Disabled != tt.wantFirst || buttons[1].Disabled != tt.wantFirst {
					t.Errorf("first/prev disabled = %v/%v, want %v",
						buttons[0].Disabled, buttons[1].Disabled, tt.wantFirst)
				}
				if !buttons[2].Disabled {
					t.Error("the page indicator should always be disabled")
				}
				if buttons[3].Disabled != tt.wantLast || buttons[4].Disabled != tt.wantLast {
					t.Errorf("next/last disabled = %v/%v, want %v",
						buttons[3].Disabled, buttons[4].Disabled, tt.wantLast)
				}
			})
		}
	})
}

// actionRowButtons flattens the single action row into its buttons.
func actionRowButtons(t *testing.T, rows []discord.LayoutComponent) []discord.ButtonComponent {
	t.Helper()

	if len(rows) != 1 {
		t.Fatalf("got %d container components, want 1 action row", len(rows))
	}

	row, ok := rows[0].(discord.ActionRowComponent)
	if !ok {
		t.Fatalf("component is %T, want discord.ActionRowComponent", rows[0])
	}

	buttons := make([]discord.ButtonComponent, 0, len(row.Components))
	for i, component := range row.Components {
		button, ok := component.(discord.ButtonComponent)
		if !ok {
			t.Fatalf("component %d is %T, want discord.ButtonComponent", i, component)
		}
		buttons = append(buttons, button)
	}
	return buttons
}
