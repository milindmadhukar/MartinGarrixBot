package handlers

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
	"github.com/milindmadhukar/STMPDBot/utils/catalogue"
)

// SelfHealCatalogue repairs the derived state of every song that has drifted from what
// the current rules produce.
//
// finaliseNewSong already does this for a row the fetchers have just inserted, but only
// for that row and only at that moment. Two things get past it. A row that already
// existed is never revisited -- the fetchers skip it -- so anything wrong with it stays
// wrong. And when the normalisation rules themselves change, every row in the table
// becomes stale at once while no individual row has been touched: that is what left 233
// rows carrying keys from an older matcher, which in turn hid ninety-nine duplicate
// pairs from every pass that looks for them.
//
// The fix for both used to be "remember to run rekey-songs and link-remix-parents",
// which is the wrong shape for a bot that runs continuously. This is those two passes
// as a ticker.
//
// What it will NOT do is anything that destroys a row. Merging duplicates deletes
// rows and needs judgement about which credit and which date survive -- dedupe-songs
// stays a deliberate, reviewed pass, and the dashboard offers the same thing one row at
// a time. Everything here is a pure recomputation from columns the row already holds,
// so the worst case for a bug in it is that the derived value is rewritten to the same
// wrong thing next cycle rather than that a recording disappears.
//
// It also honours locked_fields, because every query it calls does.
func SelfHealCatalogue(b *stmpdbot.STMPDBot, ticker *time.Ticker) {
	for ; true; <-ticker.C {
		ctx := context.Background()

		rows, err := b.Queries.GetSongsForAudit(ctx)
		if err != nil {
			slog.Error("Self-heal could not load the catalogue", slog.Any("err", err))
			continue
		}

		// The audit is the same one verify-catalogue and the dashboard report, so this
		// repairs exactly what they complain about -- and can never drift into fixing
		// something they do not consider broken.
		findings := catalogue.Audit(rows)
		byCheck := catalogue.GroupByCheck(findings)

		var repaired int
		repaired += healDerived(ctx, b, rows, byCheck)
		repaired += healParents(ctx, b, rows, byCheck)

		if repaired > 0 {
			slog.Info("Self-healed catalogue rows", slog.Int("repaired", repaired))
		}
	}
}

// healDerived rewrites the keys, the search haystack, the answerable title and the
// collection flag on rows whose stored values disagree with what the rules now produce.
func healDerived(ctx context.Context, b *stmpdbot.STMPDBot, rows []db.GetSongsForAuditRow,
	byCheck map[string][]catalogue.Finding) int {
	stale := map[int64]bool{}
	for _, check := range []string{"stale-keys", "stale-search-text", "stale-normalized-name", "collection-flag"} {
		for _, f := range byCheck[check] {
			stale[f.SongID] = true
		}
	}
	if len(stale) == 0 {
		return 0
	}

	var n int
	for _, r := range rows {
		if !stale[r.ID] {
			continue
		}

		if _, err := b.Queries.SetSongKeys(ctx, db.SetSongKeysParams{
			ID:       r.ID,
			MatchKey: utils.Text(utils.MatchKey(r.Name, "", r.MixName.String, r.Artists)),
			BaseKey:  utils.Text(utils.BaseKey(r.Name, r.Artists)),
			// release_name is part of the haystack rekey-songs builds, so it has to be
			// part of this one too -- writing it without would quietly drop the ability
			// to find a track by the EP it came on, on every row this pass touches.
			SearchText: utils.Text(utils.SearchText(r.Artists, r.Name, r.MixName.String, r.ReleaseName.String)),
		}); err != nil {
			slog.Error("Self-heal failed to rekey a song",
				slog.Int64("song_id", r.ID), slog.Any("err", err))
			continue
		}

		if _, err := b.Queries.SetSongNormalizedName(ctx, db.SetSongNormalizedNameParams{
			ID: r.ID, NormalizedName: utils.Text(utils.NormalizedTitle(r.Name)),
		}); err != nil {
			slog.Error("Self-heal failed to normalise a name",
				slog.Int64("song_id", r.ID), slog.Any("err", err))
		}

		// Recomputed in both directions, not merely raised: a row the rules once got
		// wrong must be able to stop being a collection as well as start being one.
		want := utils.IsCollection(r.Name, r.MixName.String, r.LengthMs.Int32) ||
			utils.AppleURLNamesThisRelease(r.Name, r.AppleMusicUrl.String) ||
			utils.StmpdSlugNamesRelease(r.Name, r.StmpdSlug.String)
		if want != r.IsCollection {
			if _, err := b.Queries.SetSongCollection(ctx, db.SetSongCollectionParams{
				ID: r.ID, IsCollection: want,
			}); err != nil {
				slog.Error("Self-heal failed to set a collection flag",
					slog.Int64("song_id", r.ID), slog.Any("err", err))
			}
		}
		n++
	}
	return n
}

// healParents files renditions that are sitting loose while the song they derive from
// is right there in the table.
//
// Only the unfiled case is repaired. The audit's other tree complaints -- a row that is
// its own parent, a tree two levels deep, a rendition filed under a release -- are
// rarer, and each one is a wrong pointer rather than a missing one, so re-pointing it
// automatically would overwrite a decision somebody may have made on purpose. Those
// stay for link-remix-parents and the dashboard's promote button, which is also why
// SetSongParent's lock guard matters: a hand-filed rendition is skipped here.
func healParents(ctx context.Context, b *stmpdbot.STMPDBot, rows []db.GetSongsForAuditRow,
	byCheck map[string][]catalogue.Finding) int {
	unfiled := map[int64]bool{}
	for _, f := range byCheck["unfiled-rendition"] {
		unfiled[f.SongID] = true
	}
	if len(unfiled) == 0 {
		return 0
	}

	// The canonical row for a base key, chosen the same way link-remix-parents chooses
	// it, so the two passes cannot disagree about which row is the song.
	canonical := map[string]db.GetSongsForAuditRow{}
	for _, r := range rows {
		if r.ParentSongID.Valid || r.IsCollection {
			continue
		}
		if _, variant := utils.SplitVariant(r.Name, "", r.MixName.String); variant != "" {
			continue
		}
		key := utils.BaseKey(r.Name, r.Artists)
		if best, seen := canonical[key]; !seen || catalogue.BetterCanonical(candidateOf(r), candidateOf(best)) {
			canonical[key] = r
		}
	}

	var n int
	for _, r := range rows {
		if !unfiled[r.ID] {
			continue
		}
		parent, ok := canonical[utils.BaseKey(r.Name, r.Artists)]
		if !ok || parent.ID == r.ID {
			continue
		}
		if !utils.ArtistsSubsume(r.Artists, parent.Artists) {
			continue
		}
		if _, err := b.Queries.SetSongParent(ctx, db.SetSongParentParams{
			ID: r.ID, ParentSongID: pgtype.Int8{Int64: parent.ID, Valid: true},
		}); err != nil {
			slog.Error("Self-heal failed to file a rendition",
				slog.Int64("song_id", r.ID), slog.Any("err", err))
			continue
		}
		slog.Info("Self-healed a loose rendition",
			slog.Int64("song_id", r.ID), slog.String("name", r.Name),
			slog.Int64("parent_id", parent.ID))
		n++
	}
	return n
}

// candidateOf reduces an audit row to what decides whether it is the row a listener
// means, so this pass and link-remix-parents elect the same canonical.
func candidateOf(r db.GetSongsForAuditRow) catalogue.Candidate {
	_, variant := utils.SplitVariant(r.Name, "", r.MixName.String)
	return catalogue.Candidate{
		ID:             r.ID,
		IsCollection:   r.IsCollection,
		NamesRendition: variant != "",
		HasSlug:        r.StmpdSlug.Valid,
		HasLyrics:      r.HasLyrics,
		HasLinks:       r.SpotifyUrl.Valid || r.YoutubeUrl.Valid || r.AppleMusicUrl.Valid,
		ArtistCount:    len(utils.SplitArtists(r.Artists)),
		ReleaseDate:    r.ReleaseDate.String,
	}
}
