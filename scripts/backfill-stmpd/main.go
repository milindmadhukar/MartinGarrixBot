// Command backfill-stmpd walks the entire STMPD RCRDS catalogue and reconciles it
// against the songs table.
//
// It exists because the periodic fetcher can only ever see a recent window, and the
// damage it is repairing is historical: 497 beatport-sourced rows carry no streaming
// links at all, because for months the STMPD sync was locked out of every row the
// beatport sync had touched. Those rows show up in /links as a card with no buttons
// and are invisible to the radio, which only plays songs with a YouTube URL.
//
// What it changes:
//   - fills in streaming links, artwork and the STMPD slug on rows that already exist
//   - corrects release_date from a "<year>-01-01" placeholder to the exact date
//   - merges a row into its twin when a date correction collides with unique_release
//   - inserts catalogue releases the table has never held
//
// What it deliberately cannot do: announce anything. This binary does not import the
// notifier, and every row it inserts is stamped as already announced.
//
// Run rekey-songs first, and -dry-run before the real thing.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"log/slog"
	"os"
	"strconv"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/scripts/internal/script"
	"github.com/milindmadhukar/STMPDBot/utils"
)

type counters struct {
	byTier        map[utils.MatchTier]int
	matched       int
	written       int
	unchanged     int
	inserted      int
	merged        int
	dateCorrected int
	reassigned    int
	failed        int
	conflated     []conflict
}

// conflict is a row holding one release's identity and another rendition's metadata.
type conflict struct {
	Release string
	Version string
	SongID  int64
	Row     string
	RowMix  string
}

var conflatedPath = flag.String("report-conflated", "",
	"write rows whose slug belongs to a release with a different rendition to this CSV")

func main() {
	env, ctx, cleanup := script.Setup("backfill-stmpd")
	defer cleanup()

	before, err := env.Queries.CountLinklessSongs(ctx)
	if err != nil {
		script.Fatal("failed to count linkless songs", err)
	}
	slog.Info("Songs with no streaming links before the run", slog.Int64("count", before))

	rows, err := env.Queries.GetAllSongsForMatching(ctx)
	if err != nil {
		script.Fatal("failed to load songs", err)
	}
	index := utils.NewSongIndex(rows)
	slog.Info("Indexed existing songs", slog.Int("count", len(rows)))

	// Oldest first: an original must be in place before its remixes arrive, so that
	// the remixes match against it rather than each inserting a fresh row.
	releases, err := utils.NewSanityClient().FetchStmpdReleases(ctx, "")
	if err != nil {
		script.Fatal("failed to fetch the STMPD catalogue", err)
	}
	slog.Info("Fetched STMPD catalogue", slog.Int("releases", len(releases)))

	c := counters{byTier: map[utils.MatchTier]int{}}
	c.reassigned = releaseMisassignedSlugs(ctx, env, index, releases)

	prog := script.NewProgress("reconcile catalogue", len(releases))
	for _, release := range releases {
		processRelease(ctx, env, index, release, &c)
		prog.Step()
	}
	prog.Done()

	after, err := env.Queries.CountLinklessSongs(ctx)
	if err != nil {
		script.Fatal("failed to count linkless songs", err)
	}

	slog.Info("Backfill complete",
		slog.Int("releases_seen", len(releases)),
		slog.Int("matched_existing", c.matched),
		slog.Int("rows_written", c.written),
		slog.Int("already_current", c.unchanged),
		slog.Int("inserted", c.inserted),
		slog.Int("merged_duplicates", c.merged),
		slog.Int("dates_corrected", c.dateCorrected),
		slog.Int("slugs_reassigned", c.reassigned),
		slog.Int("conflated_rows", len(c.conflated)),
		slog.Int("failed", c.failed))

	// Which tier resolved each match is the honest measure of how much of this run
	// rests on inference rather than on a stable identifier.
	for _, tier := range []utils.MatchTier{
		utils.MatchStmpdSlug, utils.MatchBeatportID, utils.MatchBeatportRelease,
		utils.MatchStreamingURL, utils.MatchKeyExact, utils.MatchBaseKeyVariant,
		utils.MatchFuzzyTitle,
	} {
		if n := c.byTier[tier]; n > 0 {
			slog.Info("Matches by tier",
				slog.String("tier", string(tier)),
				slog.Int("count", n),
				slog.Bool("exact", tier.Exact()))
		}
	}
	slog.Info("Songs with no streaming links",
		slog.Int64("before", before), slog.Int64("after", after))

	if len(c.conflated) > 0 {
		slog.Warn("Rows carrying one release's identity and another rendition's metadata",
			slog.Int("count", len(c.conflated)),
			slog.String("note", "each needs a human decision; nothing was changed"))
		if *conflatedPath != "" {
			writeConflicts(*conflatedPath, c.conflated)
		} else {
			slog.Info("Pass -report-conflated=<file.csv> to write the full list")
		}
	}
}

func processRelease(ctx context.Context, env *script.Env, index *utils.SongIndex, release utils.SanityRelease, c *counters) {
	name := release.Name()

	matched, tier := index.Lookup(release.Query())

	// Anything still mis-assigned here had nowhere better to go: the database holds
	// one row for the song and it happens to be a named rendition, while the
	// catalogue's release is the plain one. Moving the slug would mean inventing a
	// row carrying nothing but STMPD's links and stripping the only real row of them.
	// That is a gap in the catalogue, not an error. Report it.
	if matched != nil && tier == utils.MatchStmpdSlug &&
		!utils.RenditionsAgree(releaseVariant(release), rowVariant(*matched)) {
		c.conflated = append(c.conflated, conflict{
			Release: release.Name(), Version: orNone(release.Version),
			SongID: matched.ID, Row: matched.Name, RowMix: orNone(rowVariant(*matched)),
		})
	}

	if matched == nil {
		insertRelease(ctx, env, index, release, c)
		return
	}

	c.matched++
	c.byTier[tier]++

	// Correcting a stored date is only safe on an identity we are certain of. A
	// fuzzy title match is a good enough reason to add links; it is not a good
	// enough reason to rewrite the date or merge two rows together.
	correctDate := tier.Exact() || tier == utils.MatchKeyExact

	params := updateParams(matched.ID, release, correctDate)

	// The inferred tiers are the ones worth reading before committing.
	level := slog.LevelDebug
	if !tier.Exact() {
		level = slog.LevelInfo
	}
	slog.Log(ctx, level, "applying release to existing song",
		slog.String("release", name), slog.String("tier", string(tier)),
		slog.Int64("song_id", matched.ID), slog.Bool("correct_date", correctDate))

	n, err := env.Queries.UpdateSongWithStmpdRelease(ctx, params)
	if err == nil {
		index.Claim(matched, release.Slug)
		if n > 0 {
			c.written++
			if correctDate {
				c.dateCorrected++
			}
		} else {
			c.unchanged++
		}
		return
	}

	if db.ErrorCode(err) != db.UniqueViolation {
		slog.Error("failed to apply release", slog.String("name", name), slog.Any("err", err))
		c.failed++
		return
	}

	// The date correction collided with unique_release, which means another row
	// already holds this exact (name, artists, release_date). The two rows are the
	// same song arriving from two sources, so fold them together rather than
	// leaving a duplicate and an uncorrected date behind.
	mergeTwin(ctx, env, release, matched, params, c)
}

func mergeTwin(ctx context.Context, env *script.Env, release utils.SanityRelease, matched *db.GetAllSongsForMatchingRow, params db.UpdateSongWithStmpdReleaseParams, c *counters) {
	name := release.Name()

	// Look the twin up by the MATCHED ROW's name and artists, not the release's.
	// unique_release covers (name, artists, release_date), and this update only
	// changes the date -- so the row it collides with is the one already holding
	// this row's own name and artists at the new date. The release's own spelling
	// can differ ("Understand Me" stored, "Understand Me (The Remixes)" published)
	// and looking it up that way finds nothing.
	twin, err := env.Queries.GetSong(ctx, db.GetSongParams{
		Name:        matched.Name,
		Artists:     matched.Artists,
		ReleaseDate: utils.Text(release.ReleaseDate),
	})
	if err != nil {
		slog.Error("date correction conflicted but no twin row was found",
			slog.String("release", name),
			slog.String("row_name", matched.Name),
			slog.String("row_artists", matched.Artists),
			slog.String("target_date", release.ReleaseDate),
			slog.Any("err", err))
		c.failed++
		return
	}
	if twin.ID == matched.ID {
		slog.Error("row conflicted with itself, which should not be possible",
			slog.String("name", name), slog.Int64("song_id", twin.ID))
		c.failed++
		return
	}

	// Keep the twin: it already holds the corrected date, so merging into it needs
	// no further date change and cannot collide again.
	slog.Info("merging duplicate rows",
		slog.String("name", name),
		slog.Int64("keep", twin.ID), slog.Int64("drop", matched.ID))

	if err := env.Queries.MergeSongRows(ctx, db.MergeSongRowsParams{
		WinnerID: twin.ID, LoserID: matched.ID,
	}); err != nil {
		slog.Error("failed to merge rows", slog.String("name", name), slog.Any("err", err))
		c.failed++
		return
	}
	if err := env.Queries.DeleteSong(ctx, matched.ID); err != nil {
		slog.Error("failed to delete merged row", slog.String("name", name), slog.Any("err", err))
		c.failed++
		return
	}
	c.merged++

	params.ID = twin.ID
	if _, err := env.Queries.UpdateSongWithStmpdRelease(ctx, params); err != nil {
		slog.Error("failed to apply release after merge", slog.String("name", name), slog.Any("err", err))
		c.failed++
		return
	}
	c.written++
}

func insertRelease(ctx context.Context, env *script.Env, index *utils.SongIndex, release utils.SanityRelease, c *counters) {
	name := release.Name()

	song, err := env.Queries.InsertRelease(ctx, insertParams(release))
	if err != nil {
		if db.ErrorCode(err) == db.UniqueViolation {
			slog.Debug("release already stored under a different identity", slog.String("name", name))
			c.unchanged++
			return
		}
		slog.Error("failed to insert release", slog.String("name", name), slog.Any("err", err))
		c.failed++
		return
	}

	// Stamp it as announced. This binary must never cause a Discord post, and a row
	// inserted here is historical catalogue, not news.
	if err := env.Queries.MarkSongAnnounced(ctx, song.ID); err != nil {
		script.Fatal("failed to stamp inserted row as announced", err)
	}
	if _, err := env.Queries.SetSongKeys(ctx, db.SetSongKeysParams{
		ID:         song.ID,
		MatchKey:   utils.Text(utils.MatchKey(song.Name, "", song.MixName.String, song.Artists)),
		BaseKey:    utils.Text(utils.BaseKey(song.Name, song.Artists)),
		SearchText: utils.Text(utils.SearchText(song.Artists, song.Name, song.MixName.String, song.ReleaseName.String)),
	}); err != nil {
		script.Fatal("failed to key inserted row", err)
	}

	c.inserted++
	index.Append(db.GetAllSongsForMatchingRow{
		ID: song.ID, Name: song.Name, Artists: song.Artists, Source: song.Source,
		StmpdSlug: song.StmpdSlug, SpotifyUrl: song.SpotifyUrl,
		BeatportReleaseID: song.BeatportReleaseID, MixName: song.MixName,
	})
}

func insertParams(r utils.SanityRelease) db.InsertReleaseParams {
	l := r.StreamingLinks
	return db.InsertReleaseParams{
		Name: r.Title, Artists: r.Artists, ReleaseDate: utils.Text(r.ReleaseDate),
		MixName:   utils.Text(r.Version),
		StmpdSlug: utils.Text(r.Slug), ThumbnailUrl: utils.Text(r.Artwork()),
		SpotifyUrl: utils.Text(utils.CleanLink(l.Spotify)), AppleMusicUrl: utils.Text(utils.CleanLink(l.AppleMusic)),
		YoutubeUrl: utils.Text(utils.NormalizeYoutubeURL(l.YouTube)), YoutubeMusicUrl: utils.Text(utils.CleanLink(l.YouTubeMusic)),
		DeezerUrl: utils.Text(utils.CleanLink(l.Deezer)), TidalUrl: utils.Text(utils.CleanLink(l.Tidal)),
		AmazonMusicUrl: utils.Text(utils.CleanLink(l.AmazonMusic)), BeatportUrl: utils.Text(utils.CleanLink(l.Beatport)),
		BeatportReleaseID: utils.BeatportReleaseID(l.Beatport),
	}
}

func updateParams(id int64, r utils.SanityRelease, correctDate bool) db.UpdateSongWithStmpdReleaseParams {
	l := r.StreamingLinks

	// A NULL release_date leaves the stored value alone, via COALESCE.
	releaseDate := utils.Text("")
	if correctDate {
		releaseDate = utils.Text(r.ReleaseDate)
	}

	return db.UpdateSongWithStmpdReleaseParams{
		ID: id, StmpdSlug: utils.Text(r.Slug), ReleaseDate: releaseDate,
		MixName:      utils.Text(r.Version),
		ThumbnailUrl: utils.Text(r.Artwork()),
		SpotifyUrl:   utils.Text(utils.CleanLink(l.Spotify)), AppleMusicUrl: utils.Text(utils.CleanLink(l.AppleMusic)),
		YoutubeUrl: utils.Text(utils.NormalizeYoutubeURL(l.YouTube)), YoutubeMusicUrl: utils.Text(utils.CleanLink(l.YouTubeMusic)),
		DeezerUrl: utils.Text(utils.CleanLink(l.Deezer)), TidalUrl: utils.Text(utils.CleanLink(l.Tidal)),
		AmazonMusicUrl: utils.Text(utils.CleanLink(l.AmazonMusic)), BeatportUrl: utils.Text(utils.CleanLink(l.Beatport)),
		BeatportReleaseID: utils.BeatportReleaseID(l.Beatport),
	}
}

func releaseVariant(r utils.SanityRelease) string {
	_, v := utils.SplitVariant(r.Title, r.Version, "")
	return v
}

func rowVariant(row db.GetAllSongsForMatchingRow) string {
	_, v := utils.SplitVariant(row.Name, "", row.MixName.String)
	return v
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func writeConflicts(path string, rows []conflict) {
	f, err := os.Create(path)
	if err != nil {
		slog.Error("failed to write the conflated-rows report", slog.Any("err", err))
		return
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"song_id", "stored_name", "stored_rendition", "release_it_is_filed_under", "release_rendition"})
	for _, r := range rows {
		_ = w.Write([]string{strconv.FormatInt(r.SongID, 10), r.Row, r.RowMix, r.Release, r.Version})
	}
	slog.Info("Wrote conflated-rows report", slog.String("path", path), slog.Int("rows", len(rows)))
}

// releaseMisassignedSlugs frees slugs sitting on rows that record a different
// rendition, but only where the release has somewhere better to go.
//
// This runs before the main pass because a mis-assignment is often a straight swap:
// the plain release holds the acoustic row's slug while the acoustic release holds the
// plain row's. Neither can move while the other is in the way, so both are released
// first and the main pass then puts each where it belongs.
//
// The "somewhere better" test is what stops this from inventing rows. A release whose
// only candidate is the row it is already on -- the common case, where the database
// simply has no plain version of the song -- keeps it.
func releaseMisassignedSlugs(ctx context.Context, env *script.Env, index *utils.SongIndex, releases []utils.SanityRelease) int {
	bySlug := map[string]*db.GetAllSongsForMatchingRow{}
	misassigned := map[int64]bool{}

	for _, rel := range releases {
		row, tier := index.Lookup(rel.Query())
		if row == nil || tier != utils.MatchStmpdSlug {
			continue
		}
		if utils.RenditionsAgree(releaseVariant(rel), rowVariant(*row)) {
			continue
		}
		bySlug[rel.Slug] = row
		misassigned[row.ID] = true
	}
	if len(misassigned) == 0 {
		return 0
	}

	freed := 0
	for _, rel := range releases {
		row, ok := bySlug[rel.Slug]
		if !ok {
			continue
		}

		// A candidate is any row for this song that agrees on the rendition and is
		// either unclaimed or itself mis-assigned and about to be freed.
		if !index.HasBetterRendition(rel.Query(), misassigned) {
			continue
		}

		slog.Info("freeing a mis-assigned slug so the release can find its own row",
			slog.String("release", rel.Name()),
			slog.String("release_rendition", orNone(releaseVariant(rel))),
			slog.Int64("was_on", row.ID), slog.String("row", row.Name),
			slog.String("row_rendition", orNone(rowVariant(*row))))

		if !env.DryRun {
			if _, err := env.Queries.ClearStmpdSlug(ctx, row.ID); err != nil {
				slog.Error("failed to free the slug", slog.Int64("song_id", row.ID), slog.Any("err", err))
				continue
			}
		}
		index.Detach(row)
		freed++
	}

	return freed
}
