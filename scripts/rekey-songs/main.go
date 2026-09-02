// Command rekey-songs recomputes songs.match_key and songs.base_key for every row.
//
// The keys are derived in Go by utils/matchkey.go, so they cannot be filled in by a
// migration. Run this once after migration 000011, and again after any change to the
// normalization rules -- backfill-stmpd and link-remix-parents both depend on the
// keys being present and current.
//
// Idempotent: rows whose keys already match are left alone.
package main

import (
	"log/slog"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/scripts/internal/script"
	"github.com/milindmadhukar/STMPDBot/utils"
)

func main() {
	env, ctx, cleanup := script.Setup("rekey-songs")
	defer cleanup()

	rows, err := env.Queries.GetSongsForKeying(ctx)
	if err != nil {
		script.Fatal("failed to load songs", err)
	}

	var changed, unchanged, flagged, unflagged, renamed, defeatured int
	prog := script.NewProgress("rekey songs", len(rows))
	for _, row := range rows {
		prog.Step()
		// Rows the catalogue has no slug for keep whatever shape they arrived in, so
		// some carry the rendition inside the name -- "Sicko Drop (Claudinho Brasil
		// Remix)" -- while their catalogue-backed siblings carry it in mix_name. Move
		// it, so every row records a rendition the same way and the two can be told
		// apart by the same rule.
		name, mix := row.Name, row.MixName.String
		// Move the rendition out of the name when the name is the only place it lives,
		// or when mix_name already says exactly the same thing. Where the two differ
		// the name is the more specific of the two -- "Higher Ground (DubVision
		// Remix)" against a mix_name of just "Remixes" -- and stripping it would
		// throw away which remix it is.
		base, variant := utils.SplitTitleRendition(name)
		redundant := variant != "" && utils.NormalizeToken(variant) == utils.NormalizeToken(mix)
		if variant != "" && (mix == "" || redundant) {
			if mix == "" {
				mix = variant
			}
			name = base
			if _, err := env.Queries.SetSongTitle(ctx, db.SetSongTitleParams{
				ID: row.ID, Name: name, MixName: utils.Text(mix),
			}); err != nil {
				slog.Warn("could not move a rendition out of the name",
					slog.Int64("song_id", row.ID), slog.String("name", row.Name),
					slog.Any("err", err))
				name, mix = row.Name, row.MixName.String
			} else {
				renamed++
				slog.Info("moved the rendition out of the name",
					slog.Int64("song_id", row.ID), slog.String("was", row.Name),
					slog.String("now", name), slog.String("mix", mix))
			}
		}

		// A feature clause in the name is redundant when the artists column already
		// credits those people -- "All I Need Is You feat. Myke Tyler" by "Megisto,
		// Myke Tyler" says it twice. Drop it from the name so the same song is not
		// written two ways depending on which source supplied the row.
		if stripped, featured := utils.SplitTitleFeature(name); featured != "" &&
			utils.ArtistsSubsume(row.Artists, featured) {
			if _, err := env.Queries.SetSongTitle(ctx, db.SetSongTitleParams{
				ID: row.ID, Name: stripped, MixName: utils.Text(mix),
			}); err != nil {
				slog.Warn("could not drop a redundant credit from the name",
					slog.Int64("song_id", row.ID), slog.String("name", name), slog.Any("err", err))
			} else {
				defeatured++
				slog.Info("dropped a credit already in the artists",
					slog.Int64("song_id", row.ID), slog.String("was", name), slog.String("now", stripped))
				name = stripped
			}
		}

		matchKey := utils.MatchKey(name, "", mix, row.Artists)
		baseKey := utils.BaseKey(name, row.Artists)

		// Nothing branches on DryRun any more: the whole run is inside a transaction
		// that is rolled back, so the dry run exercises exactly the code the real one
		// does. The branch that used to live here returned early and skipped the
		// check below entirely, so a dry run reported no collections however many
		// there were.
		//
		// Collections are re-evaluated here too. The migration that first populated
		// the flag could only look at the title, so a DJ set called "Tomorrowland
		// 2016: The Elixir Of Life" was filed as a song -- its mix name and its
		// 29-minute running time are what give it away.
		isRelease := utils.IsCollection(row.Name, row.MixName.String, row.LengthMs.Int32) ||
			utils.AppleURLNamesThisRelease(row.Name, row.AppleMusicUrl.String) ||
			utils.StmpdSlugNamesRelease(row.Name, row.StmpdSlug.String)

		// The flag is recomputed, not merely raised. It used to be one-way, which
		// meant a row the first title-only migration got wrong stayed wrong forever:
		// "Hero" was marked a release and so was filtered out of search, out of the
		// radio, and out of contention as the parent of its own remixes -- leaving
		// the Space Ducks remix standing in for the song. Deciding both directions
		// here makes the flag a function of the row rather than of its history.
		if row.IsCollection != isRelease {
			n, err := env.Queries.SetSongCollection(ctx, db.SetSongCollectionParams{
				ID: row.ID, IsCollection: isRelease,
			})
			if err != nil {
				script.Fatal("failed to set the collection flag", err)
			}
			if n > 0 {
				if isRelease {
					flagged++
					slog.Info("flagged as a release, not a track",
						slog.Int64("song_id", row.ID), slog.String("name", row.Name),
						slog.String("mix", row.MixName.String))
				} else {
					unflagged++
					slog.Info("restored as a track, not a release",
						slog.Int64("song_id", row.ID), slog.String("name", row.Name),
						slog.String("mix", row.MixName.String))
				}
			}
		}

		n, err := env.Queries.SetSongKeys(ctx, db.SetSongKeysParams{
			ID:       row.ID,
			MatchKey: utils.Text(matchKey),
			BaseKey:  utils.Text(baseKey),
		})
		if err != nil {
			script.Fatal("failed to write song keys", err)
		}
		if n > 0 {
			changed++
		} else {
			unchanged++
		}
	}

	prog.Done()

	slog.Info("Rekey complete",
		slog.Int("total", len(rows)),
		slog.Int("written", changed),
		slog.Int("already_current", unchanged),
		slog.Int("newly_flagged_as_collections", flagged),
		slog.Int("restored_as_tracks", unflagged),
		slog.Int("renditions_moved_out_of_name", renamed),
		slog.Int("redundant_credits_dropped", defeatured))
}
