package dashboard

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

func testOptions(t *testing.T) *Options {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("loading location: %v", err)
	}
	return &Options{TimeZone: "Asia/Kolkata", Location: loc}
}

// TestTemplatesParse is the guard against the failure mode this design invites:
// a template typo is invisible until someone loads the page, because parsing
// happens at startup and executing happens per request.
func TestTemplatesParse(t *testing.T) {
	if _, err := newRenderer(testOptions(t), false); err != nil {
		t.Fatalf("templates failed to parse: %v", err)
	}
}

// TestPanelsExecute runs every metric panel through its template with empty
// data. Panels are reached only over htmx, so a nil-deref in one of them would
// otherwise show up as a silently blank card in production.
func TestPanelsExecute(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	overview := r.pages["overview"]
	for _, panel := range panelNames {
		t.Run(panel, func(t *testing.T) {
			p := &pageData{GuildID: "1", Data: map[string]any{
				"WindowDays": 30,
				"Panel":      panel,
				// Every panel must survive an empty result set: a brand new
				// guild has no rows in any of these tables.
				"Summary":     db.DashGuildOverviewRow{},
				"Grid":        [][]heatmapCell{},
				"Sort":        "xp",
				"Max":         int64(1),
				"MaxType":     int64(1),
				"Peak":        int64(0),
				"Series":      nil,
				"Channels":    nil,
				"Members":     nil,
				"ByType":      nil,
				"Moderators":  nil,
				"Punishments": nil,
			}}

			var buf bytes.Buffer
			if err := overview.ExecuteTemplate(&buf, "panel-"+panel, p); err != nil {
				t.Fatalf("panel %q failed to execute: %v", panel, err)
			}
			if buf.Len() == 0 {
				t.Fatalf("panel %q rendered nothing", panel)
			}
		})
	}
}

// TestPanelErrorStateRenders checks the degraded path, which is the one that
// only ever runs when something else has already gone wrong.
func TestPanelErrorStateRenders(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	for _, panel := range panelNames {
		p := &pageData{GuildID: "1", Data: map[string]any{
			"WindowDays": 30,
			"Error":      "This panel took too long to load.",
			"Summary":    db.DashGuildOverviewRow{},
			"Grid":       [][]heatmapCell{},
			"Sort":       "xp",
		}}

		var buf bytes.Buffer
		if err := r.pages["overview"].ExecuteTemplate(&buf, "panel-"+panel, p); err != nil {
			t.Fatalf("panel %q error state failed: %v", panel, err)
		}
		if !strings.Contains(buf.String(), "took too long") {
			t.Errorf("panel %q did not render its error message", panel)
		}
	}
}

// TestFmtTimeUsesConfiguredZone pins the timezone trap. pgx hands naive
// timestamp columns back with a UTC location even though the value is a UTC
// instant meant to be displayed locally, so formatting one directly would show
// the wrong wall clock.
func TestFmtTimeUsesConfiguredZone(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	fmtTime, ok := r.funcs()["fmtTime"].(func(pgtype.Timestamp) string)
	if !ok {
		t.Fatal("fmtTime has the wrong signature")
	}

	// 2026-01-02 00:30 UTC is 06:00 the same day in Asia/Kolkata.
	ts := pgtype.Timestamp{
		Time:  time.Date(2026, 1, 2, 0, 30, 0, 0, time.UTC),
		Valid: true,
	}
	if got, want := fmtTime(ts), "2026-01-02 06:00"; got != want {
		t.Errorf("fmtTime = %q, want %q", got, want)
	}

	if got := fmtTime(pgtype.Timestamp{}); got != "—" {
		t.Errorf("fmtTime of NULL = %q, want an em dash", got)
	}
}

func TestComma(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{int64(0), "0"},
		{int64(999), "999"},
		{int64(1000), "1,000"},
		{int64(1234567), "1,234,567"},
		{int64(-4321), "-4,321"},
		{int(42), "42"},
		{int32(100000), "100,000"},
		{pgtype.Int4{Int32: 2500, Valid: true}, "2,500"},
		{pgtype.Int4{}, "0"},
		{pgtype.Int8{Int64: 987654, Valid: true}, "987,654"},
	}
	for _, tc := range cases {
		if got := commaAny(tc.in); got != tc.want {
			t.Errorf("commaAny(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHeatmapGridIsDense(t *testing.T) {
	// The query returns only hours that saw traffic. The grid has to be
	// complete regardless, or every column after a missing cell shifts left.
	rows := []db.DashActivityHeatmapRow{
		{Dow: 1, Hour: 9, Messages: 50},
		{Dow: 6, Hour: 23, Messages: 100},
	}

	grid, peak := heatmapGrid(rows)
	if peak != 100 {
		t.Errorf("peak = %d, want 100", peak)
	}
	if len(grid) != 7 {
		t.Fatalf("grid has %d days, want 7", len(grid))
	}
	for dow, row := range grid {
		if len(row) != 24 {
			t.Fatalf("day %d has %d hours, want 24", dow, len(row))
		}
	}
	if got := grid[1][9].Messages; got != 50 {
		t.Errorf("grid[1][9] = %d, want 50", got)
	}
	if got := grid[1][9].Intensity; got != 50 {
		t.Errorf("grid[1][9] intensity = %d, want 50", got)
	}
	if got := grid[6][23].Intensity; got != 100 {
		t.Errorf("busiest cell intensity = %d, want 100", got)
	}
	if got := grid[0][0].Messages; got != 0 {
		t.Errorf("untouched cell = %d, want 0", got)
	}
}

func TestHeatmapGridHandlesNoData(t *testing.T) {
	// A guild with no messages must not divide by zero.
	grid, peak := heatmapGrid(nil)
	if peak != 0 {
		t.Errorf("peak = %d, want 0", peak)
	}
	if len(grid) != 7 || len(grid[0]) != 24 {
		t.Fatal("grid should still be dense with no input")
	}
	if grid[3][12].Intensity != 0 {
		t.Error("intensity should be 0 when there is no peak")
	}
}

// TestPanelErrorSuppressesData guards the bug golangci-lint caught: the growth
// and moderation panels each run two queries, and the second used to overwrite
// the first's error before it was ever checked. A failed first query then
// rendered as an empty chart with no error shown -- silently wrong, which is
// worse than visibly broken. Both panels now bail before the second query, so a
// panel carrying an error must not also be carrying half-populated data.
func TestPanelErrorSuppressesData(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	for _, panel := range []string{"growth", "moderation"} {
		t.Run(panel, func(t *testing.T) {
			// Exactly what the handler now produces when the first query
			// fails: an error and nothing else.
			p := &pageData{GuildID: "1", Data: map[string]any{
				"WindowDays": 30,
				"Error":      "This panel could not be loaded.",
			}}

			var buf bytes.Buffer
			if err := r.pages["overview"].ExecuteTemplate(&buf, "panel-"+panel, p); err != nil {
				t.Fatalf("panel %q with only an error failed to render: %v", panel, err)
			}

			out := buf.String()
			if !strings.Contains(out, "could not be loaded") {
				t.Errorf("panel %q did not show its error", panel)
			}
			// No bars drawn next to the error message.
			if strings.Contains(out, "style=\"height:") || strings.Contains(out, "style=\"width:") {
				t.Errorf("panel %q drew a chart alongside its error", panel)
			}
		})
	}
}
