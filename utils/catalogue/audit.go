package catalogue

import (
	"fmt"
	"sort"
	"strings"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// Check is one invariant the catalogue is expected to satisfy.
//
// ID is stable and kebab-case because it travels in a URL and in a filter value; the
// wording of Title and Explain is free to change without breaking a bookmarked link.
// Remedy names the pass that repairs the violation, so a report is a to-do list rather
// than a list of complaints.
type Check struct {
	ID      string
	Title   string
	Remedy  string
	Explain string
}

// Finding is one row breaking one check.
type Finding struct {
	Check  string
	SongID int64
	Detail string
}

// checks is the declaration order, which is also the display order.
var checks = []Check{
	{"collection-flag", "collection flag disagrees with the row", "rekey-songs",
		"A release stored as a track is queued as if a whole EP were one song; a song stored as a release disappears from search, the radio, and contention as the parent of its own remixes."},
	{"stale-keys", "match/base key is stale", "rekey-songs",
		"The stored key no longer matches what the current normalisation produces, which silently disables every matcher tier built on it."},
	{"stale-search-text", "search text is stale", "rekey-songs",
		"The folded haystack no longer matches the row's own name and credits, so the song is findable only under its old spelling."},
	{"stale-normalized-name", "normalized name is stale", "rekey-songs",
		"Readers fall back to deriving it, so quiz answers stay right -- but the stored value is a lie anyone reading the table will believe."},
	{"credit-spellings", "one song stored under several spellings of its credits", "dedupe-songs",
		"The same song written several ways. No match key can see it, because the credits differ; only a title-only key can."},
	{"duplicate-recording", "same recording stored more than once", "dedupe-songs",
		"Two rows agreeing on artists, title and rendition, which leaves nothing to tell them apart."},
	{"unfiled-rendition", "rendition is not filed under its song", "link-remix-parents",
		"A remix with no parent while the plain song sits right there, so the two read as separate songs in autocomplete."},
	{"self-parent", "row is its own parent", "link-remix-parents", "A row pointing at itself."},
	{"missing-parent", "parent row does not exist", "link-remix-parents", "A dangling parent pointer."},
	{"parent-is-release", "filed under a release rather than a song", "link-remix-parents",
		"A rendition hanging off an EP. \"Catharina (Remixes)\" is a child of \"Catharina\", not its parent."},
	{"deep-tree", "rendition tree is deeper than one level", "link-remix-parents",
		"A rendition filed under another rendition. The tree is meant to be flat."},
	{"parent-artists", "filed under a song by different artists", "link-remix-parents",
		"A rendition whose credits do not contain its parent's."},
	{"canonical-worse-than-child", "canonical row is worse than its own rendition", "link-remix-parents",
		"The canonical row is missing a streaming link or lyrics that one of its own renditions has. Only the canonical is ever shown, so what the row's own family already knows is invisible to every listener. Either the wrong row was elected -- promote the better one -- or the right row is simply missing a link that can be filled in."},
	{"shared-thumbnail", "artwork belongs to a different song", "fix-shared-artwork",
		"One cover on rows that are not the same song. Beatport has no per-track image, only a per-release one, so every track on a compilation comes back wearing the compilation's cover."},
	{"no-artwork", "song has no artwork", "backfill-artwork",
		"A track with no cover to put on its card."},
	{"beatport-no-slug", "beatport id with no slug, so no link can be built", "import-beatport",
		"A track page is /track/<slug>/<id>; without the slug every Beatport button leads to a 404."},
	{"tracking-params", "streaming link carries tracking parameters", "backfill-stmpd",
		"A link still carrying ?si= or utm_."},
	{"no-links", "song has no link of any kind", "backfill-stmpd",
		"A card with no buttons on it."},
}

// Checks returns every invariant, in display order.
func Checks() []Check { return append([]Check(nil), checks...) }

// CheckByID looks one up.
func CheckByID(id string) (Check, bool) {
	for _, c := range checks {
		if c.ID == id {
			return c, true
		}
	}
	return Check{}, false
}

// Audit runs every check over the whole catalogue.
//
// Findings come back grouped by check in declaration order, and by song id within a
// check, so two runs over the same data produce byte-identical output.
func Audit(rows []db.GetSongsForAuditRow) []Finding {
	byID := make(map[int64]db.GetSongsForAuditRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}

	var f []Finding
	add := func(check string, id int64, format string, args ...any) {
		f = append(f, Finding{Check: check, SongID: id, Detail: fmt.Sprintf(format, args...)})
	}

	checkCollections(add, rows)
	checkKeys(add, rows)
	checkNormalizedNames(add, rows)
	checkDuplicates(add, rows)
	checkParents(add, rows, byID)
	checkCanonicalQuality(add, rows, byID)
	checkArtwork(add, rows)
	checkLinks(add, rows)

	order := map[string]int{}
	for i, c := range checks {
		order[c.ID] = i
	}
	sort.SliceStable(f, func(i, j int) bool {
		if order[f[i].Check] != order[f[j].Check] {
			return order[f[i].Check] < order[f[j].Check]
		}
		return f[i].SongID < f[j].SongID
	})
	return f
}

// GroupByCheck buckets findings by check ID.
func GroupByCheck(f []Finding) map[string][]Finding {
	m := map[string][]Finding{}
	for _, x := range f {
		m[x.Check] = append(m[x.Check], x)
	}
	return m
}

// GroupBySong buckets check IDs by the row that breaks them, which is what a song page
// and the list page's problem filter both need.
func GroupBySong(f []Finding) map[int64][]string {
	m := map[int64][]string{}
	for _, x := range f {
		m[x.SongID] = append(m[x.SongID], x.Check)
	}
	return m
}

type addFunc func(check string, id int64, format string, args ...any)

// checkCollections re-derives the release/track distinction. Getting this wrong in
// either direction is expensive: a song wrongly marked a release disappears from
// search, the radio and its own remixes' parenting, while a release left as a song
// gets queued as if a whole EP were one track.
func checkCollections(add addFunc, rows []db.GetSongsForAuditRow) {
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
		add("collection-flag", r.ID, "%s (%s) -- %s", r.Name, r.MixName.String, state)
	}
}

// checkKeys catches rows whose stored keys no longer match what the current
// normalisation produces -- which silently disables every matcher tier built on them.
func checkKeys(add addFunc, rows []db.GetSongsForAuditRow) {
	for _, r := range rows {
		wantMatch := utils.MatchKey(r.Name, "", r.MixName.String, r.Artists)
		wantBase := utils.BaseKey(r.Name, r.Artists)
		if r.MatchKey.String != wantMatch || r.BaseKey.String != wantBase {
			add("stale-keys", r.ID, "%s -- stored %q, want %q", r.Name, r.MatchKey.String, wantMatch)
		}

		// Absent is a legitimate state meaning "derive it on read", the same contract
		// normalized_name has; only a stored value that disagrees is a fault.
		if want := utils.SearchText(r.Artists, r.Name, r.MixName.String, ""); r.SearchText.Valid &&
			!strings.HasPrefix(r.SearchText.String, want) {
			add("stale-search-text", r.ID, "%s -- stored %q, want %q", r.Name, r.SearchText.String, want)
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
// means the same song is stored under several spellings of its credits, and no match
// key sees it, because the credits differ. Only a title-only key does.
func checkNormalizedNames(add addFunc, rows []db.GetSongsForAuditRow) {
	groups := map[string][]db.GetSongsForAuditRow{}

	for _, r := range rows {
		want := utils.NormalizedTitle(r.Name)
		if r.NormalizedName.Valid && r.NormalizedName.String != want {
			add("stale-normalized-name", r.ID, "%s -- stored %q, want %q",
				r.Name, r.NormalizedName.String, want)
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

	for _, k := range sortedKeys(groups) {
		g := groups[k]
		if len(g) < 2 || !anyArtistsOverlap(g) {
			continue
		}
		add("credit-spellings", g[0].ID, "%s -- ids %s", g[0].Name, idList(g))
	}
}

// anyArtistsOverlap reports whether two rows in the group credit at least one artist
// in common. Different artists writing songs with the same title is ordinary; only an
// overlapping credit makes it one song written several ways.
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
func checkDuplicates(add addFunc, rows []db.GetSongsForAuditRow) {
	groups := map[string][]db.GetSongsForAuditRow{}
	for _, r := range rows {
		// A release and its title track share every field a match key is built from:
		// "Void" and the "Void EP" are one artist, one title and no rendition apart.
		// They are not the same recording, and the collection flag is what says so.
		if r.IsCollection {
			continue
		}
		if key := utils.MatchKey(r.Name, "", r.MixName.String, r.Artists); key != "" {
			groups[key] = append(groups[key], r)
		}
	}
	for _, k := range sortedKeys(groups) {
		if g := groups[k]; len(g) >= 2 {
			add("duplicate-recording", g[0].ID, "%s (%s) -- ids %s",
				g[0].Name, g[0].MixName.String, idList(g))
		}
	}
}

// checkParents verifies the shape of the rendition tree: one level deep, never rooted
// at a release, and only ever joining rows that credit compatible artists.
func checkParents(add addFunc, rows []db.GetSongsForAuditRow, byID map[int64]db.GetSongsForAuditRow) {
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
					add("unfiled-rendition", r.ID, "%s (%s) -- should hang off #%d",
						r.Name, r.MixName.String, id)
				}
			}
			continue
		}

		if r.ParentSongID.Int64 == r.ID {
			add("self-parent", r.ID, "%s (%s)", r.Name, r.MixName.String)
			continue
		}

		parent, ok := byID[r.ParentSongID.Int64]
		if !ok {
			add("missing-parent", r.ID, "%s -- parent #%d", r.Name, r.ParentSongID.Int64)
			continue
		}
		if parent.IsCollection {
			add("parent-is-release", r.ID, "%s -- parent #%d %q is a collection",
				r.Name, parent.ID, parent.Name)
		}
		if parent.ParentSongID.Valid {
			add("deep-tree", r.ID, "%s -- parent #%d is itself a child", r.Name, parent.ID)
		}
		if !utils.ArtistsSubsume(r.Artists, parent.Artists) {
			add("parent-artists", r.ID, "%q by %s -- parent #%d by %s",
				r.Name, r.Artists, parent.ID, parent.Artists)
		}
	}
}

// checkCanonicalQuality finds families where the row every listener sees is worse than
// one hidden underneath it.
//
// This is the shape of the "Break Through The Silence" defect: the canonical row was a
// beatport listing with no streaming links at all, and the STMPD row carrying YouTube,
// Spotify and the lyrics was filed under it as a rendition. Nothing in the tree's shape
// is wrong -- the parent exists, the tree is flat, the credits agree -- so every
// structural check passes while the song is unusable.
func checkCanonicalQuality(add addFunc, rows []db.GetSongsForAuditRow, byID map[int64]db.GetSongsForAuditRow) {
	type gap struct {
		child   int64
		missing []string
	}
	worse := map[int64][]gap{}

	for _, c := range rows {
		if !c.ParentSongID.Valid {
			continue
		}
		// A remix pack filed under its song carries links to the pack, not to the
		// track. "Clap Clap (Remixes)" having a Spotify link its song does not is not
		// a defect in the song -- and promoting a release to be the song is exactly
		// what BetterCanonical refuses to do.
		if c.IsCollection {
			continue
		}
		p, ok := byID[c.ParentSongID.Int64]
		if !ok {
			continue
		}
		var missing []string
		for _, f := range []struct {
			name          string
			parent, child bool
		}{
			{"spotify", p.SpotifyUrl.Valid, c.SpotifyUrl.Valid},
			{"youtube", p.YoutubeUrl.Valid, c.YoutubeUrl.Valid},
			{"apple music", p.AppleMusicUrl.Valid, c.AppleMusicUrl.Valid},
			{"lyrics", p.HasLyrics, c.HasLyrics},
		} {
			if !f.parent && f.child {
				missing = append(missing, f.name)
			}
		}
		if len(missing) > 0 {
			worse[p.ID] = append(worse[p.ID], gap{child: c.ID, missing: missing})
		}
	}

	for _, id := range sortedInt64Keys(worse) {
		g := worse[id][0]
		p := byID[id]
		add("canonical-worse-than-child", id,
			"%s by %s -- rendition #%d has %s and this row does not",
			p.Name, p.Artists, g.child, strings.Join(g.missing, ", "))
	}
}

// checkArtwork finds covers that are not this song's.
//
// Beatport exposes no per-track image, only a per-release one, so a track on a
// compilation comes back wearing the compilation's cover: one image in production sat
// on twelve unrelated songs, and a listener asking for "Dragon" got a card showing the
// Tomorrowland 2016 sleeve.
//
// What makes that wrong is not the sharing. Sharing a cover is normal and correct in
// two shapes, and neither is a defect:
//
//   - a song and its own renditions, which is what a single's artwork is; and
//   - the tracks of one act's own EP, which is what an EP's artwork is -- "The Street"
//     and "Front 2 Back" are both Bart B More on "The Street EP".
//
// What is wrong is a cover shared by rows that credit nobody in common, because then it
// can only be a compilation's. Artist overlap is therefore the test, not the count.
// Counting alone flagged 139 rows of which almost all were somebody's own EP.
func checkArtwork(add addFunc, rows []db.GetSongsForAuditRow) {
	groups := map[string][]db.GetSongsForAuditRow{}
	for _, r := range rows {
		if r.ThumbnailUrl.Valid && r.ThumbnailUrl.String != "" {
			groups[r.ThumbnailUrl.String] = append(groups[r.ThumbnailUrl.String], r)
		}
	}

	// A cover is suspect when its group contains at least one pair of rows sharing no
	// artist at all.
	suspect := map[string]int{}
	for url, g := range groups {
		if len(g) < 2 {
			continue
		}
		credits := make([]string, len(g))
		for i, r := range g {
			credits[i] = r.Artists
		}
		if n := UnrelatedActs(credits); n > 1 {
			suspect[url] = n
		}
	}

	for _, r := range rows {
		if !r.ThumbnailUrl.Valid || r.ThumbnailUrl.String == "" {
			if !r.IsCollection && !r.ParentSongID.Valid {
				add("no-artwork", r.ID, "%s by %s", r.Name, r.Artists)
			}
			continue
		}
		if n, ok := suspect[r.ThumbnailUrl.String]; ok {
			add("shared-thumbnail", r.ID, "%s by %s -- cover is shared by %d unrelated acts",
				r.Name, r.Artists, n)
		}
	}
}

// UnrelatedActs counts how many mutually-unrelated credit groups a set of credit
// strings falls into. One
// means everything on it is by the same act or its collaborators, however many rows
// there are.
//
// The test is a shared artist, not credit containment. Containment is right for
// deciding whether one row is a rendition of another, but wrong here: the twelve
// remixes of "Told You So" are each credited to the original artists plus a different
// remixer, so no two of them contain each other -- and they legitimately share the
// single's cover. What they do have is Jex and Martin Garrix in common, and that is
// what says they are the same release rather than a compilation.
func UnrelatedActs(credits []string) int {
	type group struct{ acts map[string]bool }
	var groups []*group

	for _, credit := range credits {
		acts := map[string]bool{}
		for _, a := range utils.SplitArtists(credit) {
			if key := utils.NormalizeToken(a); key != "" {
				acts[key] = true
			}
		}
		if len(acts) == 0 {
			continue
		}

		// Fold into every group this row shares an artist with; a row crediting two
		// acts that were previously unconnected joins them into one.
		var merged *group
		kept := groups[:0]
		for _, existing := range groups {
			shared := false
			for a := range acts {
				if existing.acts[a] {
					shared = true
					break
				}
			}
			if !shared {
				kept = append(kept, existing)
				continue
			}
			if merged == nil {
				merged = existing
				kept = append(kept, existing)
				continue
			}
			for a := range existing.acts {
				merged.acts[a] = true
			}
		}
		groups = kept
		if merged == nil {
			groups = append(groups, &group{acts: acts})
			continue
		}
		for a := range acts {
			merged.acts[a] = true
		}
	}
	return len(groups)
}

// checkLinks looks for buttons that cannot be built or would not resolve.
func checkLinks(add addFunc, rows []db.GetSongsForAuditRow) {
	for _, r := range rows {
		if r.BeatportID.Valid && !r.BeatportSlug.Valid && !r.BeatportUrl.Valid {
			add("beatport-no-slug", r.ID, "%s -- track %d", r.Name, r.BeatportID.Int32)
		}

		for _, u := range []string{
			r.SpotifyUrl.String, r.YoutubeUrl.String, r.AppleMusicUrl.String,
			r.DeezerUrl.String, r.TidalUrl.String, r.AmazonMusicUrl.String,
			r.YoutubeMusicUrl.String, r.BeatportUrl.String,
		} {
			if strings.Contains(u, "?si=") || strings.Contains(u, "&si=") ||
				strings.Contains(u, "utm_") {
				add("tracking-params", r.ID, "%s -- %s", r.Name, u)
				break
			}
		}

		if r.IsCollection || r.ParentSongID.Valid {
			continue
		}
		if !r.SpotifyUrl.Valid && !r.YoutubeUrl.Valid && !r.AppleMusicUrl.Valid &&
			!r.DeezerUrl.Valid && !r.TidalUrl.Valid && !r.AmazonMusicUrl.Valid &&
			!r.YoutubeMusicUrl.Valid && !r.BeatportUrl.Valid && !r.BeatportID.Valid {
			add("no-links", r.ID, "%s by %s", r.Name, r.Artists)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

func sortedInt64Keys[V any](m map[int64]V) []int64 {
	k := make([]int64, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Slice(k, func(i, j int) bool { return k[i] < k[j] })
	return k
}

func idList(g []db.GetSongsForAuditRow) string {
	ids := make([]string, len(g))
	for i, r := range g {
		ids[i] = fmt.Sprint(r.ID)
	}
	return strings.Join(ids, ", ")
}
