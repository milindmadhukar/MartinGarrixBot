// Command verify-catalogue checks the songs table against the rules the rest of the
// bot assumes, and reports every row that breaks one.
//
// It exists because the alternative was someone scrolling the table by hand and
// noticing that "Aurora" appeared twice. Each defect found that way turned out to be
// a whole class -- one wrongly flagged collection meant twenty-seven songs were
// missing from search -- so the useful unit of work is the invariant, not the row.
//
// It writes nothing. Every check names the pass that repairs it.
package main

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/scripts/internal/script"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// samplesPerCheck bounds the output. A check that fires on 300 rows is a broken rule,
// not 300 broken rows, and the first few are enough to recognise it.
const samplesPerCheck = 8

type check struct {
	name   string
	remedy string
	rows   []string
	count  int
}

type report struct {
	order  []string
	checks map[string]*check
}

func newReport() *report {
	return &report{checks: map[string]*check{}}
}

// flag records one violation. The remedy travels with the check so the output says
// what to run, not merely what is wrong.
func (r *report) flag(name, remedy string, id int64, detail string) {
	c, ok := r.checks[name]
	if !ok {
		c = &check{name: name, remedy: remedy}
		r.checks[name] = c
		r.order = append(r.order, name)
	}
	c.count++
	if len(c.rows) < samplesPerCheck {
		c.rows = append(c.rows, fmt.Sprintf("#%d %s", id, detail))
	}
}

func main() {
	env, ctx, cleanup := script.Setup("verify-catalogue")
	defer cleanup()

	rows, err := env.Queries.GetSongsForAudit(ctx)
	if err != nil {
		script.Fatal("failed to load the catalogue", err)
	}

	byID := make(map[int64]db.GetSongsForAuditRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}

	rep := newReport()
	checkCollections(rep, rows)
	checkKeys(rep, rows)
	checkNormalizedNames(rep, rows)
	checkDuplicates(rep, rows)
	checkParents(rep, rows, byID)
	checkLinks(rep, rows)

	total := 0
	for _, name := range rep.order {
		c := rep.checks[name]
		total += c.count
		slog.Warn("invariant violated",
			slog.String("check", c.name),
			slog.Int("rows", c.count),
			slog.String("fix", c.remedy))
		for _, s := range c.rows {
			slog.Info("  " + s)
		}
		if c.count > len(c.rows) {
			slog.Info(fmt.Sprintf("  ... and %d more", c.count-len(c.rows)))
		}
	}

	slog.Info("Audit complete",
		slog.Int("songs", len(rows)),
		slog.Int("checks_failed", len(rep.order)),
		slog.Int("rows_flagged", total))
	if total == 0 {
		slog.Info("The catalogue satisfies every invariant")
	}
}

// checkCollections re-derives the release/track distinction. Getting this wrong in
// either direction is expensive: a song wrongly marked a release disappears from
// search, the radio and its own remixes' parenting, while a release left as a song
// gets queued as if a whole EP were one track.
func checkCollections(rep *report, rows []db.GetSongsForAuditRow) {
	for _, r := range rows {
		want := utils.IsCollection(r.Name, r.MixName.String, r.LengthMs.Int32) ||
			utils.AppleURLNamesThisRelease(r.Name, r.AppleMusicUrl.String) ||
			utils.StmpdSlugNamesRelease(r.Name, r.StmpdSlug.String)
		if want == r.IsCollection {
			continue
		}
		state := "stored as a track, looks like a release"
		if r.IsCollection {
			state = "stored as a release, looks like a track"
		}
		rep.flag("collection flag disagrees with the row", "rekey-songs",
			r.ID, fmt.Sprintf("%s (%s) -- %s", r.Name, r.MixName.String, state))
	}
}

// checkKeys catches rows whose stored keys no longer match what the current
// normalisation produces -- which silently disables every matcher tier built on them.
func checkKeys(rep *report, rows []db.GetSongsForAuditRow) {
	for _, r := range rows {
		wantMatch := utils.MatchKey(r.Name, "", r.MixName.String, r.Artists)
		wantBase := utils.BaseKey(r.Name, r.Artists)
		if r.MatchKey.String != wantMatch || r.BaseKey.String != wantBase {
			rep.flag("match/base key is stale", "rekey-songs",
				r.ID, fmt.Sprintf("%s -- stored %q, want %q", r.Name, r.MatchKey.String, wantMatch))
		}
	}
}

// checkNormalizedNames catches two things the quiz depends on.
//
// A stale normalized_name is the milder one: readers fall back to deriving it, so the
// answers stay right, but the stored value is then a lie that anyone reading the table
// will believe. An absent one is not flagged at all -- NULL is a legitimate state that
// means "derive it", and flagging it would make a fresh column look like a fault.
//
// Two canonical rows reducing to the same normalized name is the interesting one. It
// means the same song is stored under several spellings of its credits -- the
// catalogue holds "Now that I've Found You Feat. \"John & Michel\"", "Now That I've
// Found You feat. John & Michel" and "Now That I've Found You feat. John Martin feat.
// Michel Zitron" -- and no match key sees it, because the credits differ. Only a
// title-only key does.
func checkNormalizedNames(rep *report, rows []db.GetSongsForAuditRow) {
	groups := map[string][]db.GetSongsForAuditRow{}

	for _, r := range rows {
		want := utils.NormalizedTitle(r.Name)
		if r.NormalizedName.Valid && r.NormalizedName.String != want {
			rep.flag("normalized name is stale", "rekey-songs",
				r.ID, fmt.Sprintf("%s -- stored %q, want %q", r.Name, r.NormalizedName.String, want))
		}

		// Renditions and releases legitimately share a song's title with it, so only
		// canonical tracks are compared against each other.
		if r.IsCollection || r.ParentSongID.Valid {
			continue
		}
		if key := utils.NormalizeToken(want); key != "" {
			groups[key] = append(groups[key], r)
		}
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		g := groups[k]
		if len(g) < 2 {
			continue
		}
		// Different artists writing songs with the same title is ordinary. Only flag a
		// group whose credits overlap, which is what makes it one song written several
		// ways rather than several songs sharing a name.
		if !anyArtistsOverlap(g) {
			continue
		}
		ids := make([]string, len(g))
		for i, r := range g {
			ids[i] = fmt.Sprint(r.ID)
		}
		rep.flag("one song stored under several spellings of its credits", "dedupe-songs",
			g[0].ID, fmt.Sprintf("%s -- ids %s", g[0].Name, strings.Join(ids, ", ")))
	}
}

// anyArtistsOverlap reports whether two rows in the group credit at least one artist
// in common.
func anyArtistsOverlap(g []db.GetSongsForAuditRow) bool {
	for i := range g {
		for j := i + 1; j < len(g); j++ {
			if utils.ArtistsSubsume(g[i].Artists, g[j].Artists) ||
				utils.ArtistsSubsume(g[j].Artists, g[i].Artists) {
				return true
			}
		}
	}
	return false
}

// checkDuplicates finds rows that are the same recording. Two rows sharing a match key
// agree on artists, title and rendition, which leaves nothing to tell them apart.
func checkDuplicates(rep *report, rows []db.GetSongsForAuditRow) {
	groups := map[string][]db.GetSongsForAuditRow{}
	for _, r := range rows {
		// A release and its title track share every field a match key is built from:
		// "Void" and the "Void EP" are one artist, one title and no rendition apart.
		// They are not the same recording, and the collection flag is what says so.
		if r.IsCollection {
			continue
		}
		key := utils.MatchKey(r.Name, "", r.MixName.String, r.Artists)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], r)
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		g := groups[k]
		if len(g) < 2 {
			continue
		}
		ids := make([]string, len(g))
		for i, r := range g {
			ids[i] = fmt.Sprint(r.ID)
		}
		rep.flag("same recording stored more than once", "dedupe-songs",
			g[0].ID, fmt.Sprintf("%s (%s) -- ids %s",
				g[0].Name, g[0].MixName.String, strings.Join(ids, ", ")))
	}
}

// checkParents verifies the shape of the rendition tree: one level deep, never rooted
// at a release, and only ever joining rows that credit compatible artists.
func checkParents(rep *report, rows []db.GetSongsForAuditRow, byID map[int64]db.GetSongsForAuditRow) {
	canonical := map[string]int64{}
	for _, r := range rows {
		if r.ParentSongID.Valid || r.IsCollection {
			continue
		}
		if _, variant := utils.SplitVariant(r.Name, "", r.MixName.String); variant == "" {
			canonical[utils.BaseKey(r.Name, r.Artists)] = r.ID
		}
	}

	for _, r := range rows {
		if !r.ParentSongID.Valid {
			// A rendition with no parent, while the plain song sits right there, is
			// the Aurora case: the remix and the original read as two songs.
			if _, variant := utils.SplitVariant(r.Name, "", r.MixName.String); variant != "" {
				if id, ok := canonical[utils.BaseKey(r.Name, r.Artists)]; ok && id != r.ID {
					rep.flag("rendition is not filed under its song", "link-remix-parents",
						r.ID, fmt.Sprintf("%s (%s) -- should hang off #%d",
							r.Name, r.MixName.String, id))
				}
			}
			continue
		}

		if r.ParentSongID.Int64 == r.ID {
			rep.flag("row is its own parent", "link-remix-parents",
				r.ID, fmt.Sprintf("%s (%s)", r.Name, r.MixName.String))
			continue
		}

		parent, ok := byID[r.ParentSongID.Int64]
		if !ok {
			rep.flag("parent row does not exist", "link-remix-parents",
				r.ID, fmt.Sprintf("%s -- parent #%d", r.Name, r.ParentSongID.Int64))
			continue
		}
		if parent.IsCollection {
			rep.flag("filed under a release rather than a song", "link-remix-parents",
				r.ID, fmt.Sprintf("%s -- parent #%d %q is a collection",
					r.Name, parent.ID, parent.Name))
		}
		if parent.ParentSongID.Valid {
			rep.flag("rendition tree is deeper than one level", "link-remix-parents",
				r.ID, fmt.Sprintf("%s -- parent #%d is itself a child", r.Name, parent.ID))
		}
		if !utils.ArtistsSubsume(r.Artists, parent.Artists) {
			rep.flag("filed under a song by different artists", "link-remix-parents",
				r.ID, fmt.Sprintf("%q by %s -- parent #%d by %s",
					r.Name, r.Artists, parent.ID, parent.Artists))
		}
	}
}

// checkLinks looks for buttons that cannot be built or would not resolve.
func checkLinks(rep *report, rows []db.GetSongsForAuditRow) {
	for _, r := range rows {
		if r.BeatportID.Valid && !r.BeatportSlug.Valid && !r.BeatportUrl.Valid {
			rep.flag("beatport id with no slug, so no link can be built", "import-beatport",
				r.ID, fmt.Sprintf("%s -- track %d", r.Name, r.BeatportID.Int32))
		}

		for _, u := range []string{
			r.SpotifyUrl.String, r.YoutubeUrl.String, r.AppleMusicUrl.String,
			r.DeezerUrl.String, r.TidalUrl.String, r.AmazonMusicUrl.String,
			r.YoutubeMusicUrl.String, r.BeatportUrl.String,
		} {
			if strings.Contains(u, "?si=") || strings.Contains(u, "&si=") ||
				strings.Contains(u, "utm_") {
				rep.flag("streaming link carries tracking parameters", "backfill-stmpd",
					r.ID, fmt.Sprintf("%s -- %s", r.Name, u))
				break
			}
		}

		if r.IsCollection || r.ParentSongID.Valid {
			continue
		}
		if !r.SpotifyUrl.Valid && !r.YoutubeUrl.Valid && !r.AppleMusicUrl.Valid &&
			!r.DeezerUrl.Valid && !r.TidalUrl.Valid && !r.AmazonMusicUrl.Valid &&
			!r.YoutubeMusicUrl.Valid && !r.BeatportUrl.Valid && !r.BeatportID.Valid {
			rep.flag("song has no link of any kind", "backfill-stmpd",
				r.ID, fmt.Sprintf("%s by %s", r.Name, r.Artists))
		}
	}
}
