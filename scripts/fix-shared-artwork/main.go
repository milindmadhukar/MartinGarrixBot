// Command fix-shared-artwork removes cover art that belongs to a different song.
//
// Beatport exposes no per-track image, only a per-release one, so the importer that
// took it unconditionally gave every track on a compilation that compilation's cover.
// In production one image sat on twelve unrelated songs and 276 rows wore artwork
// belonging to something else: a listener asking for "Dragon" got a card showing the
// Tomorrowland 2016 sleeve.
//
// A cover is cleared when the rows wearing it credit nobody in common, because then it
// cannot be any of their artwork. That is the whole rule, and it is shared with the
// audit check that reports the same rows, so a run of this pass and the report that
// sent you to it cannot disagree.
//
// Sharing on its own is not a defect, which is why the count is not the test. A song
// and its renditions share a cover; so do the tracks of one act's own EP -- "Front 2
// Back" is Bart B More on "The Street EP", and the EP sleeve is exactly what Apple and
// Spotify show for it. Only a cover spanning unrelated acts is necessarily wrong.
//
// This pass only clears. Refilling is backfill-artwork's job, and running it afterwards
// is the point of clearing: a NULL cover is a row the Apple enrichment will resolve,
// while a wrong one is a row nothing will ever revisit.
//
// Writes nothing that a human has locked. Idempotent, and never announces.
package main

import (
	"log/slog"

	"github.com/milindmadhukar/STMPDBot/scripts/internal/script"
	"github.com/milindmadhukar/STMPDBot/utils/catalogue"
)

func main() {
	env, ctx, cleanup := script.Setup("fix-shared-artwork")
	defer cleanup()

	rows, err := env.Queries.GetSongsWithSharedArtwork(ctx)
	if err != nil {
		script.Fatal("failed to load songs", err)
	}
	slog.Info("Rows sharing a cover with another song", slog.Int("count", len(rows)))

	// Rows come back ordered by thumbnail_url, so one pass groups them by cover.
	byCover := map[string][]int{}
	credits := map[string][]string{}
	for i, row := range rows {
		url := row.ThumbnailUrl.String
		byCover[url] = append(byCover[url], i)
		credits[url] = append(credits[url], row.Artists)
	}

	var kept, cleared, locked int
	prog := script.NewProgress("fix shared artwork", len(rows))

	for _, row := range rows {
		prog.Step()

		// A cover on several acts who credit nobody in common cannot be any of their
		// artwork. That is the whole test, and it is deliberately the only one.
		//
		// The tempting extra rule -- "the release is not named after this track, so the
		// cover is not this track's" -- is wrong. A track on its own act's EP wears the
		// EP's cover legitimately: "Front 2 Back" is Bart B More on "The Street EP", and
		// the EP sleeve is exactly what Apple and Spotify show for it. Applying that
		// rule here would have stripped 115 rows of correct artwork.
		//
		// It is also what catches Beatport's placeholder images, which land on unrelated
		// singles whose release name is the track name, so no release-name rule could
		// see them.
		//
		// The same rule the audit reports, so this pass and the check that reports it
		// cannot disagree about which rows are wrong.
		if catalogue.UnrelatedActs(credits[row.ThumbnailUrl.String]) < 2 {
			kept++
			continue
		}

		n, err := env.Queries.ClearSongArtwork(ctx, row.ID)
		if err != nil {
			script.Fatal("failed to clear artwork", err)
		}
		if n == 0 {
			// The only way to match no rows is a lock: the row was selected because it
			// has a cover, and nothing else has run since.
			locked++
			slog.Info("left a hand-set cover alone",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name))
			continue
		}

		cleared++
		slog.Info("cleared a cover shared by unrelated acts",
			slog.Int64("song_id", row.ID),
			slog.String("name", row.Name),
			slog.String("artists", row.Artists),
			slog.String("release", row.ReleaseName.String),
			slog.String("was", row.ThumbnailUrl.String))
	}

	prog.Done()

	slog.Info("Shared artwork pass complete",
		slog.Int("examined", len(rows)),
		slog.Int("kept_as_their_own", kept),
		slog.Int("cleared", cleared),
		slog.Int("left_locked", locked))
	if cleared > 0 && !env.DryRun {
		slog.Info("Run backfill-artwork next to resolve replacements from Apple")
	}
}
