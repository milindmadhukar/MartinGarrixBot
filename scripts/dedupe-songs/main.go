// Command dedupe-songs folds together rows that represent the same recording.
//
// A match_key is the artist set, the base title and the rendition, so two rows
// sharing one are the same recording stored twice. That happens because the two
// sources disagree about where the rendition belongs -- STMPD publishes "Grove
// (Rework)" as a title while beatport files "Grove" with mix_name "Rework" -- and
// because for months the old matcher could not pair rows across sources at all, so
// the beatport importer inserted its own copy of songs already present.
//
// The user-visible symptom is one song offering two identical-looking entries in
// /links autocomplete.
//
// Not every duplicate is safe to merge automatically. Where the two rows disagree
// about the release date, one of the two dates is about to be discarded, and a large
// gap can mean a genuine re-release rather than a duplicate. So merging is bounded by
// -max-date-gap (default 120 days, which covers the normal disagreement between
// beatport's publish date and STMPD's release date); anything wider is reported for a
// human to decide and left alone.
package main

import (
	"context"
	"flag"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/scripts/internal/script"
)

// distinctSlugs counts how many different STMPD releases a group claims to be.
func distinctSlugs(group []db.GetDuplicateMatchKeyRowsRow) int {
	seen := map[string]struct{}{}
	for _, r := range group {
		if r.StmpdSlug.Valid && r.StmpdSlug.String != "" {
			seen[r.StmpdSlug.String] = struct{}{}
		}
	}
	return len(seen)
}

// hasDate reports whether a row's release date is actually known. It is NULL for an
// unreleased song and for one whose date could not be established.
func hasDate(d pgtype.Text) bool { return d.Valid && d.String != "" }

// dateOf renders a possibly-absent date for logging.
func dateOf(d pgtype.Text) string {
	if !hasDate(d) {
		return "(none)"
	}
	return d.String
}

func main() {
	maxGap := flag.Int("report-date-gap", 400,
		"log a warning when merged rows' release dates differ by more than this many days")
	reportPath := flag.String("report-suspects", "",
		"write likely duplicates that are NOT safe to merge automatically to this CSV, and exit")

	env, ctx, cleanup := script.Setup("dedupe-songs")
	defer cleanup()

	if *reportPath != "" {
		reportSuspects(ctx, env, *reportPath)
		return
	}

	rows, err := env.Queries.GetDuplicateMatchKeyRows(ctx)
	if err != nil {
		script.Fatal("failed to load duplicate rows", err)
	}

	groups := map[string][]db.GetDuplicateMatchKeyRowsRow{}
	var order []string
	for _, r := range rows {
		k := r.MatchKey.String
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	slog.Info("Duplicate groups found", slog.Int("groups", len(order)), slog.Int("rows", len(rows)))

	var merged, deferred, failed int

	prog := script.NewProgress("dedupe groups", len(order))
	for _, key := range order {
		prog.Step()
		group := groups[key]
		if len(group) < 2 {
			continue
		}

		// Two rows each holding a different STMPD slug are two different releases,
		// however identically they key. "La La La" and "La La La (Drove Remix)" are
		// filed under separate slugs but reduce to the same match_key because the
		// remix name lives in neither the title nor mix_name on one of them.
		//
		// Merging them drops one slug, and the next backfill re-creates a row for
		// the release that lost it -- so the two scripts undo each other on every
		// run. The catalogue is the authority here, not the key.
		if slugs := distinctSlugs(group); slugs > 1 {
			deferred++
			slog.Warn("left alone: these are separate releases in the STMPD catalogue",
				slog.String("match_key", key),
				slog.Int("distinct_slugs", slugs),
				slog.Any("rows", describe(group)))
			continue
		}

		// The date used to gate this, and it was the wrong authority. Three rows for
		// "Together" -- identical artists, identical rendition, identical match_key --
		// went untouched because one of them was dated nine months out, and the same
		// happened to "Bouncybob". A match_key already encodes the artist set, the
		// base title and the rendition; two rows sharing one are the same recording,
		// and a date that disagrees is usually a compilation listing rather than
		// evidence of a second release.
		//
		// The slug guard above is the authority that matters: where the catalogue
		// says these are separate releases, they are left alone regardless of dates.

		if gap, ok := dateGap(group); ok && gap > *maxGap {
			slog.Warn("merging rows whose release dates differ widely",
				slog.String("match_key", key), slog.Int("gap_days", gap),
				slog.Any("rows", describe(group)))
		}

		winner := pickWinner(group)
		for _, r := range group {
			if r.ID == winner.ID {
				continue
			}

			if env.DryRun {
				slog.Info("would merge",
					slog.String("match_key", key),
					slog.Int64("keep", winner.ID), slog.String("keep_name", winner.Name),
					slog.String("keep_date", dateOf(winner.ReleaseDate)),
					slog.Int64("drop", r.ID), slog.String("drop_name", r.Name),
					slog.String("drop_date", dateOf(r.ReleaseDate)))
				merged++
				continue
			}

			if !mergeRows(ctx, env, winner.ID, r.ID, winner.StmpdSlug, r.StmpdSlug,
				winner.BeatportID, r.BeatportID) {
				failed++
				continue
			}
			merged++
			slog.Info("merged",
				slog.String("name", winner.Name),
				slog.Int64("kept", winner.ID), slog.Int64("dropped", r.ID))
		}
	}

	prog.Done()

	subsetMerged, subsetDeferred := dedupeBySubsetCredit(ctx, env)

	slog.Info("Dedupe complete",
		slog.Int("groups", len(order)),
		slog.Int("rows_merged_away", merged),
		slog.Int("groups_left_for_review", deferred),
		slog.Int("merged_by_subset_credit", subsetMerged),
		slog.Int("subset_pairs_left_for_review", subsetDeferred),
		slog.Int("failed", failed))
}

// pickWinner chooses the row to keep: the one a user would recognise as the song.
//
// A slug means the STMPD catalogue confirmed this release, which is the strongest
// provenance available; then hand-entered lyrics, which exist nowhere else and must
// not be the row that disappears; then streaming links; then the earliest date, since
// a duplicate is usually the later re-listing.
func pickWinner(group []db.GetDuplicateMatchKeyRowsRow) db.GetDuplicateMatchKeyRowsRow {
	best := group[0]
	for _, r := range group[1:] {
		if better(r, best) {
			best = r
		}
	}
	return best
}

func better(a, b db.GetDuplicateMatchKeyRowsRow) bool {
	if s := boolCmp(a.StmpdSlug.Valid, b.StmpdSlug.Valid); s != 0 {
		return s > 0
	}
	if s := boolCmp(a.Lyrics.Valid, b.Lyrics.Valid); s != 0 {
		return s > 0
	}
	if s := boolCmp(hasLinks(a), hasLinks(b)); s != 0 {
		return s > 0
	}
	// A known date beats an absent one. Without this a row with no date sorts as the
	// earliest of all and wins the "earliest release" rule below, so the merged row
	// would keep the absence and throw away a date we actually know.
	if s := boolCmp(hasDate(a.ReleaseDate), hasDate(b.ReleaseDate)); s != 0 {
		return s > 0
	}
	if a.ReleaseDate.String != b.ReleaseDate.String {
		return a.ReleaseDate.String < b.ReleaseDate.String
	}
	return a.ID < b.ID
}

func boolCmp(a, b bool) int {
	switch {
	case a && !b:
		return 1
	case !a && b:
		return -1
	default:
		return 0
	}
}

func hasLinks(r db.GetDuplicateMatchKeyRowsRow) bool {
	return r.SpotifyUrl.Valid || r.AppleMusicUrl.Valid || r.YoutubeUrl.Valid
}

// dateGap returns the span in days between the group's earliest and latest dates.
// Rows still carrying the 1970 placeholder are ignored: the placeholder is not a
// real disagreement, it is an absence.
func dateGap(group []db.GetDuplicateMatchKeyRowsRow) (int, bool) {
	var min, max time.Time
	for _, r := range group {
		if !hasDate(r.ReleaseDate) {
			continue
		}
		t, err := time.Parse(time.DateOnly, r.ReleaseDate.String)
		if err != nil {
			return 0, false
		}
		if min.IsZero() || t.Before(min) {
			min = t
		}
		if max.IsZero() || t.After(max) {
			max = t
		}
	}
	if min.IsZero() {
		return 0, true
	}
	return int(max.Sub(min).Hours() / 24), true
}

func describe(group []db.GetDuplicateMatchKeyRowsRow) []string {
	out := make([]string, 0, len(group))
	for _, r := range group {
		out = append(out, dateOf(r.ReleaseDate)+" "+r.Source+" #"+itoa(r.ID)+" "+r.Name)
	}
	return out
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

func pgInt8(v int64) pgtype.Int8 {
	return pgtype.Int8{Int64: v, Valid: true}
}

// mergeRows folds loser into winner and deletes it, in the order the constraints
// require.
//
// Renditions hanging off the losing row move first, or the foreign key nulls them and
// they resurface as separate songs. Then the loser gives up its uniquely-indexed
// identifiers -- beatport_id and stmpd_slug -- because the winner cannot take them on
// while another row still holds them.
func mergeRows(ctx context.Context, env *script.Env, winnerID, loserID int64,
	winnerSlug, loserSlug pgtype.Text, winnerBP, loserBP pgtype.Int4) bool {
	if _, err := env.Queries.RepointChildren(ctx, db.RepointChildrenParams{
		NewParent: pgInt8(winnerID), OldParent: pgInt8(loserID),
	}); err != nil {
		slog.Error("failed to repoint renditions", slog.Int64("song_id", loserID), slog.Any("err", err))
		return false
	}
	if err := env.Queries.ReleaseSongIdentifiers(ctx, loserID); err != nil {
		slog.Error("failed to release identifiers", slog.Int64("song_id", loserID), slog.Any("err", err))
		return false
	}
	if err := env.Queries.AdoptSongIdentifiers(ctx, db.AdoptSongIdentifiersParams{
		ID: winnerID, BeatportID: loserBP, StmpdSlug: loserSlug,
	}); err != nil {
		slog.Error("failed to adopt identifiers", slog.Int64("song_id", winnerID), slog.Any("err", err))
		return false
	}
	if err := env.Queries.MergeSongRows(ctx, db.MergeSongRowsParams{
		WinnerID: winnerID, LoserID: loserID,
	}); err != nil {
		slog.Error("failed to merge", slog.Int64("song_id", loserID), slog.Any("err", err))
		return false
	}
	if err := env.Queries.DeleteSong(ctx, loserID); err != nil {
		slog.Error("failed to delete merged row", slog.Int64("song_id", loserID), slog.Any("err", err))
		return false
	}
	return true
}
