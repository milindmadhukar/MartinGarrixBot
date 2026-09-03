package dashboard

import (
	"context"
	"net/http"
	"sync"
	"time"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/utils"
	"github.com/milindmadhukar/STMPDBot/utils/catalogue"
)

// songAuditTTL bounds how stale the problems view may be.
//
// The audit is a full table read plus a pure Go pass over every row, and it backs three
// different things -- the problems page, the list page's problem filter, and the badges
// on every song page -- so an unmemoized version would run it three times for one
// navigation. Every write handler invalidates it eagerly, so a repair is visibly gone
// on the next paint rather than up to a minute later.
const songAuditTTL = 60 * time.Second

type songAuditCache struct {
	mu       sync.Mutex
	at       time.Time
	findings []catalogue.Finding
}

// audit returns the current findings, recomputing them if the cached set has expired.
func (s *Server) audit(ctx context.Context) ([]catalogue.Finding, error) {
	s.songAudit.mu.Lock()
	defer s.songAudit.mu.Unlock()

	if s.songAudit.findings != nil && time.Since(s.songAudit.at) < songAuditTTL {
		return s.songAudit.findings, nil
	}

	rows, err := s.queries.GetSongsForAudit(ctx)
	if err != nil {
		return nil, err
	}
	s.songAudit.findings = catalogue.Audit(rows)
	s.songAudit.at = time.Now()
	return s.songAudit.findings, nil
}

// invalidateAudit drops the cached findings after a write.
func (s *Server) invalidateAudit() {
	s.songAudit.mu.Lock()
	s.songAudit.findings = nil
	s.songAudit.mu.Unlock()
}

// songLink is one streaming service's presence on a row, so the templates can render
// the eight of them as a strip without repeating the same markup eight times.
type songLink struct {
	Field string
	Label string
	URL   string
}

func songLinks(s db.Song) []songLink {
	return []songLink{
		{"spotify_url", "Spotify", s.SpotifyUrl.String},
		{"apple_music_url", "Apple Music", s.AppleMusicUrl.String},
		{"youtube_url", "YouTube", s.YoutubeUrl.String},
		{"youtube_music_url", "YouTube Music", s.YoutubeMusicUrl.String},
		{"deezer_url", "Deezer", s.DeezerUrl.String},
		{"tidal_url", "Tidal", s.TidalUrl.String},
		{"amazon_music_url", "Amazon Music", s.AmazonMusicUrl.String},
		{"beatport_url", "Beatport", s.BeatportUrl.String},
	}
}

// songListRow is one row of the catalogue table plus the problems the audit found on it.
type songListRow struct {
	db.DashSongsRow
	Problems []catalogue.Check
}

func (s *Server) handleSongs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query()
	page, pageSize := parsePaging(query)

	terms := utils.SearchTerms(query.Get("q"))

	// The problem filter is expressed as an id set rather than as a predicate per
	// check, so every invariant is filterable without a query per check and the other
	// filters still compose with it.
	var ids []int64
	problem := query.Get("problem")
	if problem != "" {
		findings, err := s.audit(ctx)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		for _, f := range findings {
			if f.Check == problem {
				ids = append(ids, f.SongID)
			}
		}
		// A check that fired on nothing must return nothing, not everything. A nil
		// slice reads as "no filter" in the query, so stand in a sentinel that cannot
		// be a real id.
		if len(ids) == 0 {
			ids = []int64{-1}
		}
	}

	filters := db.DashSongsParams{
		Terms:        terms,
		Source:       optText(query.Get("source")),
		IsCollection: optBool(query.Get("collection")),
		IsCanonical:  optBool(query.Get("canonical")),
		HasLyrics:    optBool(query.Get("lyrics")),
		HasArtwork:   optBool(query.Get("artwork")),
		HasLinks:     optBool(query.Get("links")),
		Ids:          ids,
		Lim:          int32(pageSize),
		Off:          int32((page - 1) * pageSize),
	}

	rows, err := s.queries.DashSongs(ctx, filters)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	total, err := s.queries.DashSongsCount(ctx, db.DashSongsCountParams{
		Terms:        filters.Terms,
		Source:       filters.Source,
		IsCollection: filters.IsCollection,
		IsCanonical:  filters.IsCanonical,
		HasLyrics:    filters.HasLyrics,
		HasArtwork:   filters.HasArtwork,
		HasLinks:     filters.HasLinks,
		Ids:          filters.Ids,
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// Problem badges on the visible rows only. The audit is already cached, so this is
	// a map lookup per row rather than another pass.
	findings, err := s.audit(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	bySong := catalogue.GroupBySong(findings)

	rendered := make([]songListRow, 0, len(rows))
	for _, row := range rows {
		entry := songListRow{DashSongsRow: row}
		for _, id := range bySong[row.ID] {
			if c, ok := catalogue.CheckByID(id); ok {
				entry.Problems = append(entry.Problems, c)
			}
		}
		rendered = append(rendered, entry)
	}

	p := s.newPage(r, "Catalogue")
	p.Nav = "songs"
	p.Data = map[string]any{
		"Rows":       rendered,
		"Checks":     catalogue.Checks(),
		"Pagination": newPagination(page, pageSize, total, filterQuery(query)),
		"Filters": map[string]string{
			"q":          query.Get("q"),
			"source":     query.Get("source"),
			"collection": query.Get("collection"),
			"canonical":  query.Get("canonical"),
			"lyrics":     query.Get("lyrics"),
			"artwork":    query.Get("artwork"),
			"links":      query.Get("links"),
			"problem":    problem,
		},
	}
	s.render(w, r, "songs", "songs-table", p)
}

// songDetail is everything the song page shows about one row.
type songDetail struct {
	Song       db.Song
	Links      []songLink
	Problems   []catalogue.Check
	Parent     *db.Song
	Renditions []db.GetSongMixesRow
	// Siblings are the other renditions of this row's parent, shown when this row is
	// itself a rendition so the whole family is visible from any member of it.
	Siblings      []db.GetSongMixesRow
	Announcements []db.DashSongAnnouncementsRow
	Lockable      []string
}

func (s *Server) handleSong(w http.ResponseWriter, r *http.Request) {
	song, ok := s.songFromPath(w, r)
	if !ok {
		return
	}
	s.renderSong(w, r, song, "", "")
}

// problemGroup is one invariant and the rows breaking it.
type problemGroup struct {
	Check    catalogue.Check
	Count    int
	Samples  []catalogue.Finding
	Overflow int
}

// samplesPerProblem bounds one group on the page. A check firing on 300 rows is a
// broken rule rather than 300 broken rows, and the list page behind "show all" is where
// someone works through them.
const samplesPerProblem = 8

func (s *Server) handleSongProblems(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	findings, err := s.audit(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	byCheck := catalogue.GroupByCheck(findings)

	var failing, passing []problemGroup
	for _, c := range catalogue.Checks() {
		group := byCheck[c.ID]
		if len(group) == 0 {
			passing = append(passing, problemGroup{Check: c})
			continue
		}
		shown := min(len(group), samplesPerProblem)
		failing = append(failing, problemGroup{
			Check:    c,
			Count:    len(group),
			Samples:  group[:shown],
			Overflow: len(group) - shown,
		})
	}

	p := s.newPage(r, "Catalogue problems")
	p.Nav = "songs"
	p.Data = map[string]any{
		"Failing": failing,
		"Passing": passing,
		"Total":   len(findings),
	}
	s.render(w, r, "songproblems", "problems-list", p)
}
