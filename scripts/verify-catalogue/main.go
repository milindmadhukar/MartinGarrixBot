// Command verify-catalogue checks the songs table against the rules the rest of the
// bot assumes, and reports every row that breaks one.
//
// It exists because the alternative was someone scrolling the table by hand and
// noticing that "Aurora" appeared twice. Each defect found that way turned out to be
// a whole class -- one wrongly flagged collection meant twenty-seven songs were
// missing from search -- so the useful unit of work is the invariant, not the row.
//
// The invariants themselves live in utils/catalogue, because the dashboard's problems
// page reports the same ones and two implementations would drift. What stays here is
// how a terminal renders them.
//
// It writes nothing. Every check names the pass that repairs it.
package main

import (
	"fmt"
	"log/slog"

	"github.com/milindmadhukar/STMPDBot/scripts/internal/script"
	"github.com/milindmadhukar/STMPDBot/utils/catalogue"
)

// samplesPerCheck bounds the output. A check that fires on 300 rows is a broken rule,
// not 300 broken rows, and the first few are enough to recognise it.
const samplesPerCheck = 8

func main() {
	env, ctx, cleanup := script.Setup("verify-catalogue")
	defer cleanup()

	rows, err := env.Queries.GetSongsForAudit(ctx)
	if err != nil {
		script.Fatal("failed to load the catalogue", err)
	}

	findings := catalogue.Audit(rows)
	byCheck := catalogue.GroupByCheck(findings)

	failed := 0
	for _, c := range catalogue.Checks() {
		group := byCheck[c.ID]
		if len(group) == 0 {
			continue
		}
		failed++
		slog.Warn("invariant violated",
			slog.String("check", c.Title),
			slog.Int("rows", len(group)),
			slog.String("fix", c.Remedy))

		shown := min(len(group), samplesPerCheck)
		for _, f := range group[:shown] {
			slog.Info(fmt.Sprintf("  #%d %s", f.SongID, f.Detail))
		}
		if len(group) > shown {
			slog.Info(fmt.Sprintf("  ... and %d more", len(group)-shown))
		}
	}

	slog.Info("Audit complete",
		slog.Int("songs", len(rows)),
		slog.Int("checks_failed", failed),
		slog.Int("rows_flagged", len(findings)))
	if len(findings) == 0 {
		slog.Info("The catalogue satisfies every invariant")
	}
}
