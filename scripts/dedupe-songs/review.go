package main

import (
	"context"
	"encoding/csv"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"

	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/scripts/internal/script"
)

// reportSuspects writes the duplicate groups that an exact match_key cannot catch.
//
// These are songs whose rows agree on the title but disagree on who made it, and the
// disagreement comes in three shapes that need different judgement:
//
//   - one row simply omits a featured artist ("Moksi Ft. Adam McInnis" / "Moksi")
//   - the same act is credited two ways ("Duncan" / "Düncan Musique")
//   - the artists field is corrupted, usually by the title bleeding into it
//     ("Able HeartDetonate", "rionosremixes")
//
// Only the first is close to safe to merge blind, and even then a shared title plus a
// subset credit can be two different recordings. So this reports rather than acts:
// the output is a CSV to read, not a queue to execute.
func reportSuspects(ctx context.Context, env *script.Env, path string) {
	rows, err := env.Queries.GetCanonicalSongsForReview(ctx)
	if err != nil {
		script.Fatal("failed to load songs for review", err)
	}

	byTitle := map[string][]db.GetCanonicalSongsForReviewRow{}
	var order []string
	for _, r := range rows {
		title := titleOf(r.BaseKey.String)
		if title == "" {
			continue
		}
		if _, seen := byTitle[title]; !seen {
			order = append(order, title)
		}
		byTitle[title] = append(byTitle[title], r)
	}

	f, err := os.Create(path)
	if err != nil {
		script.Fatal("failed to create the report", err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"reason", "title", "song_id", "name", "artists", "release_date", "source", "has_links"})

	var groups int
	for _, title := range order {
		group := byTitle[title]
		if len(group) < 2 {
			continue
		}
		reason := classify(group)
		if reason == "" {
			continue
		}
		groups++
		for _, r := range group {
			_ = w.Write([]string{
				reason, title, strconv.FormatInt(r.ID, 10), r.Name, r.Artists,
				r.ReleaseDate, r.Source, strconv.FormatBool(reviewHasLinks(r)),
			})
		}
	}

	slog.Info("Wrote review report",
		slog.String("path", path),
		slog.Int("groups", groups),
		slog.String("note", "these need a human decision; nothing was changed"))
}

func titleOf(baseKey string) string {
	if i := strings.IndexByte(baseKey, '|'); i >= 0 {
		return baseKey[i+1:]
	}
	return ""
}

func artistSetOf(baseKey string) string {
	if i := strings.IndexByte(baseKey, '|'); i >= 0 {
		return baseKey[:i]
	}
	return ""
}

// classify names why a group of same-titled songs might be one song, or returns ""
// when the credits look like genuinely different acts.
func classify(group []db.GetCanonicalSongsForReviewRow) string {
	sets := map[string]bool{}
	for _, r := range group {
		sets[artistSetOf(r.BaseKey.String)] = true
	}
	if len(sets) < 2 {
		return ""
	}

	list := make([]string, 0, len(sets))
	for s := range sets {
		list = append(list, s)
	}
	sort.Strings(list)

	var subset, alias bool
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			a, b := splitSet(list[i]), splitSet(list[j])
			switch {
			case isSubset(a, b) || isSubset(b, a):
				subset = true
			case looksAliased(a, b):
				alias = true
			}
		}
	}

	switch {
	case subset:
		return "feature-omitted"
	case alias:
		return "artist-alias-or-corrupt"
	default:
		return ""
	}
}

func splitSet(s string) []string { return strings.Split(s, "+") }

func isSubset(small, large []string) bool {
	if len(small) >= len(large) {
		return false
	}
	in := map[string]bool{}
	for _, x := range large {
		in[x] = true
	}
	for _, x := range small {
		if !in[x] {
			return false
		}
	}
	return true
}

// looksAliased reports whether every artist on one side has a counterpart on the
// other by containment. The length floor keeps short names like "ra" or "kev" from
// matching everything they happen to be a substring of.
func looksAliased(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
	for _, x := range a {
		found := false
		for j, y := range b {
			if used[j] {
				continue
			}
			if x == y || (len(x) >= 6 && len(y) >= 6 && (strings.Contains(x, y) || strings.Contains(y, x))) {
				used[j], found = true, true
				break
			}
		}
		if !found {
			return false
		}
	}
	// Identical sets are not aliases.
	return !equalSets(a, b)
}

func equalSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func reviewHasLinks(r db.GetCanonicalSongsForReviewRow) bool {
	return r.SpotifyUrl.Valid || r.YoutubeUrl.Valid || r.AppleMusicUrl.Valid || r.BeatportID.Valid
}
