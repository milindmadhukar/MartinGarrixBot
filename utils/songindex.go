package utils

import (
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

// MatchTier names how a candidate row was identified, so the caller can log it and
// the confidence of a match can be judged after the fact. Lower is more certain.
type MatchTier string

const (
	MatchNone            MatchTier = ""
	MatchStmpdSlug       MatchTier = "stmpd_slug"
	MatchBeatportID      MatchTier = "beatport_id"
	MatchBeatportRelease MatchTier = "beatport_release_id"
	MatchStreamingURL    MatchTier = "streaming_url"
	MatchKeyExact        MatchTier = "match_key"
	MatchBaseKeyVariant  MatchTier = "base_key"
	MatchFuzzyTitle      MatchTier = "fuzzy_title"
)

// Exact returns whether a tier identifies a row by a stable external identifier
// rather than by inference. Destructive operations -- merging two rows, correcting a
// stored release date -- must only ever act on an exact match.
func (t MatchTier) Exact() bool {
	switch t {
	case MatchStmpdSlug, MatchBeatportID, MatchBeatportRelease, MatchStreamingURL:
		return true
	default:
		return false
	}
}

// fuzzyTitleThreshold is the similarity a title must reach to match within an
// already-agreeing artist set. It is stricter than the 0.85 the old whole-string
// matcher used, because it is now applied only to the title: with the artists
// already matched exactly, a loose title threshold is the only remaining way to
// conflate two genuinely different songs by the same artist.
const fuzzyTitleThreshold = 0.90

// SongIndex resolves an incoming release to the row that already represents it.
//
// It replaces a linear scan that compared every incoming track against every stored
// row with Levenshtein -- O(rows x tracks x len^2) on every 15 minute cycle, in both
// fetchers. The index is built once per cycle and every tier but the last is an O(1)
// map lookup.
type SongIndex struct {
	rows []db.GetAllSongsForMatchingRow

	bySlug            map[string]int
	byBeatportID      map[int32]int
	byBeatportRelease map[int32]int
	byStreamingURL    map[string]int
	byMatchKey        map[string][]int
	byBaseKey         map[string][]int
	byArtistSet       map[string][]int
}

func NewSongIndex(rows []db.GetAllSongsForMatchingRow) *SongIndex {
	ix := &SongIndex{
		rows:              rows,
		bySlug:            make(map[string]int, len(rows)),
		byBeatportID:      make(map[int32]int, len(rows)),
		byBeatportRelease: make(map[int32]int, len(rows)),
		byStreamingURL:    make(map[string]int, len(rows)),
		byMatchKey:        make(map[string][]int, len(rows)),
		byBaseKey:         make(map[string][]int, len(rows)),
		byArtistSet:       make(map[string][]int, len(rows)),
	}
	for i := range rows {
		ix.add(i)
	}
	return ix
}

func (ix *SongIndex) add(i int) {
	r := ix.rows[i]

	if r.StmpdSlug.Valid && r.StmpdSlug.String != "" {
		ix.bySlug[r.StmpdSlug.String] = i
	}
	if r.BeatportID.Valid {
		ix.byBeatportID[r.BeatportID.Int32] = i
	}
	if r.BeatportReleaseID.Valid {
		ix.byBeatportRelease[r.BeatportReleaseID.Int32] = i
	}
	if key := StreamingURLKey(r.SpotifyUrl.String); key != "" {
		ix.byStreamingURL[key] = i
	}

	// Fall back to computing the keys when a row has not been keyed yet, so the
	// index is usable before the one-off keying pass has run.
	matchKey, baseKey := r.MatchKey.String, r.BaseKey.String
	if !r.MatchKey.Valid || matchKey == "" {
		matchKey = MatchKey(r.Name, "", r.MixName.String, r.Artists)
	}
	if !r.BaseKey.Valid || baseKey == "" {
		baseKey = BaseKey(r.Name, r.Artists)
	}

	ix.byMatchKey[matchKey] = append(ix.byMatchKey[matchKey], i)
	ix.byBaseKey[baseKey] = append(ix.byBaseKey[baseKey], i)
	ix.byArtistSet[ArtistSetKey(r.Artists)] = append(ix.byArtistSet[ArtistSetKey(r.Artists)], i)
}

// Append registers a row inserted during this cycle, so that later releases in the
// same batch match against it instead of inserting a duplicate.
func (ix *SongIndex) Append(row db.GetAllSongsForMatchingRow) {
	ix.rows = append(ix.rows, row)
	ix.add(len(ix.rows) - 1)
}

// Claim records that a row now belongs to an STMPD release, so that later releases
// in the same pass see the ownership immediately.
//
// Without this the stickiness in claimedByAnother would only take effect on the next
// run: the row's slug is written to the database, but the copy held in the index --
// the copy every subsequent lookup consults -- would still show it as unowned.
func (ix *SongIndex) Claim(row *db.GetAllSongsForMatchingRow, slug string) {
	if row == nil || slug == "" {
		return
	}
	row.StmpdSlug = pgtype.Text{String: slug, Valid: true}
	for i := range ix.rows {
		if ix.rows[i].ID == row.ID {
			ix.rows[i].StmpdSlug = row.StmpdSlug
			ix.bySlug[slug] = i
			return
		}
	}
}

// SongQuery is everything known about an incoming release that could identify it.
type SongQuery struct {
	Title             string
	Version           string
	MixName           string
	Artists           string
	StmpdSlug         string
	BeatportID        pgtype.Int4
	BeatportReleaseID pgtype.Int4
	SpotifyURL        string
}

// Lookup resolves a query to a stored row, returning the tier that identified it.
func (ix *SongIndex) Lookup(q SongQuery) (*db.GetAllSongsForMatchingRow, MatchTier) {
	if q.StmpdSlug != "" {
		if i, ok := ix.bySlug[q.StmpdSlug]; ok {
			return &ix.rows[i], MatchStmpdSlug
		}
	}
	if q.BeatportID.Valid {
		if i, ok := ix.byBeatportID[q.BeatportID.Int32]; ok {
			return &ix.rows[i], MatchBeatportID
		}
	}
	if q.BeatportReleaseID.Valid {
		if i, ok := ix.byBeatportRelease[q.BeatportReleaseID.Int32]; ok {
			return &ix.rows[i], MatchBeatportRelease
		}
	}
	if key := StreamingURLKey(q.SpotifyURL); key != "" {
		if i, ok := ix.byStreamingURL[key]; ok {
			return &ix.rows[i], MatchStreamingURL
		}
	}

	base, variant := SplitVariant(q.Title, q.Version, q.MixName)
	artistSet := ArtistSetKey(q.Artists)

	for _, i := range ix.byMatchKey[artistSet+"|"+base+"|"+variant] {
		if ix.claimedByAnother(i, q) {
			continue
		}
		return &ix.rows[i], MatchKeyExact
	}

	// Base-key match, but only when one of the two sides has no variant. STMPD
	// publishes "Catharina" while beatport lists "Catharina (Extended Mix)"; those
	// are the same record and should pair. Two *named* remixes of one song also
	// share a base key and must not.
	//
	// Candidates are preferred by how little inference they need: a stored row with
	// no variant of its own is the original, and an incoming release with no version
	// should land on that rather than on whichever remix happens to be indexed
	// first. Six rows share the "Catharina" base key in production, so first-wins
	// here would be close to a coin toss.
	var fallback *db.GetAllSongsForMatchingRow
	for _, i := range ix.byBaseKey[artistSet+"|"+base] {
		if ix.claimedByAnother(i, q) {
			continue
		}
		otherVariant := storedVariant(ix.rows[i])
		switch {
		case variant == otherVariant:
			return &ix.rows[i], MatchBaseKeyVariant
		case (variant == "" || otherVariant == "") && fallback == nil:
			fallback = &ix.rows[i]
		}
	}
	if fallback != nil {
		return fallback, MatchBaseKeyVariant
	}

	// Last resort: a close title within an artist set that already agrees exactly.
	//
	// Two guards keep this from being the loose tier it replaced. Variants must be
	// compatible, exactly as on the base-key tier -- without that, "Sicko Drop
	// (Majestic Remix)" and "Sicko Drop (Claudinho Brasil Remix)" reduce to the same
	// base and match each other. And digits must agree, because Levenshtein treats
	// them as ordinary characters: "STMPD RCRDS Mixtape 2019 Side A" scores 0.923
	// against the 2025 edition, comfortably over any threshold worth having.
	for _, i := range ix.byArtistSet[artistSet] {
		if ix.claimedByAnother(i, q) {
			continue
		}
		otherBase, otherVariant := SplitVariant(ix.rows[i].Name, "", ix.rows[i].MixName.String)
		if variant != otherVariant && variant != "" && otherVariant != "" {
			continue
		}
		if digitsOf(base) != digitsOf(otherBase) {
			continue
		}
		if IsCloseMatch(base, otherBase, fuzzyTitleThreshold) {
			return &ix.rows[i], MatchFuzzyTitle
		}
	}

	return nil, MatchNone
}

// digitsOf returns the digits of s in order. Numbers in a title carry meaning that
// character-level similarity does not see: a year, a volume, a part number. Two
// titles that differ only in their digits are different releases.
func digitsOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// claimedByAnother reports whether a candidate row already belongs to a different
// STMPD release.
//
// The slug is unique per release, so a row carrying one is that release's row. Three
// separate releases -- "Holding On To You", its remix and its acoustic version --
// otherwise all resolve to the same row on the inferred tiers, and each write
// overwrites the slug and date the previous one set. The result never settles: the
// backfill reported 142 rows written on its second run and 72 on its third, and the
// periodic fetcher would have rewritten the same rows every fifteen minutes forever.
//
// Only applies to queries that carry a slug of their own. A beatport track has none,
// and enriching an STMPD-owned row with BPM and key is exactly what it should do.
func (ix *SongIndex) claimedByAnother(i int, q SongQuery) bool {
	if q.StmpdSlug == "" {
		return false
	}
	existing := ix.rows[i].StmpdSlug
	return existing.Valid && existing.String != "" && existing.String != q.StmpdSlug
}

func storedVariant(r db.GetAllSongsForMatchingRow) string {
	_, variant := SplitVariant(r.Name, "", r.MixName.String)
	return variant
}

// StreamingURLKey reduces a Spotify URL to a namespaced identity, so that the same
// record found via two differently-parameterised links compares equal.
//
// The namespace is kept: open.spotify.com/album/<id> and /track/<id> are different
// id spaces, and comparing bare ids across them would produce false matches.
func StreamingURLKey(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	trimmed := rawURL
	if i := strings.IndexAny(trimmed, "?#"); i >= 0 {
		trimmed = trimmed[:i]
	}
	trimmed = strings.TrimRight(trimmed, "/")

	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 {
		return ""
	}

	kind, id := parts[len(parts)-2], parts[len(parts)-1]
	if kind == "" || id == "" {
		return ""
	}
	return kind + ":" + id
}
