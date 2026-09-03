package dashboard

import (
	"net/http"
	"strconv"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// leaderboardRow is one rendered row: the database record, the names the bot
// resolved for it, and the level derived from the XP.
type leaderboardRow struct {
	db.DashLeaderboardRow
	Rank   int
	ID     string
	Name   string
	Avatar string
	// Left marks someone who is no longer in the guild. They keep their XP and
	// their place; the flag only lets the UI say so.
	Left bool
	// Level and the two XP figures come from utils, the same code /rank uses, so
	// the dashboard and the bot can never disagree about what a total means.
	Level int
	// int64 because the pct template helper takes int64 and templates do not
	// convert numeric types for a function call.
	CurrentXP    int64
	XPForNextLvl int64
}

// leaderboardSorts is the allowlist. An unrecognised sort falls back to xp
// rather than reaching the query, where it would silently order by id alone.
var leaderboardSorts = map[string]string{"xp": "xp", "coins": "coins", "messages": "messages"}

func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	guildID := guildFrom(ctx)
	query := r.URL.Query()

	page, pageSize := parsePaging(query)

	sort := query.Get("sort")
	if _, ok := leaderboardSorts[sort]; !ok {
		sort = "xp"
	}

	total, err := s.queries.DashLeaderboardCount(ctx, int64(guildID))
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	rows, err := s.queries.DashLeaderboard(ctx, db.DashLeaderboardParams{
		GuildID: int64(guildID),
		Sort:    sort,
		Limit:   int32(pageSize),
		Offset:  int32((page - 1) * pageSize),
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, strconv.FormatInt(row.ID, 10))
	}
	names := s.bots.ResolveUsers(ctx, guildID, dedupe(ids))

	offset := (page - 1) * pageSize
	rendered := make([]leaderboardRow, 0, len(rows))
	for i, row := range rows {
		id := strconv.FormatInt(row.ID, 10)
		lvl := utils.GetUserLevelData(row.TotalXp)

		entry := leaderboardRow{
			DashLeaderboardRow: row,
			// Rank is positional rather than a window function, so it stays
			// correct under every sort without a second query.
			Rank:         offset + i + 1,
			ID:           id,
			Name:         id,
			Avatar:       avatarURL(id, ""),
			Left:         true,
			Level:        lvl.Lvl,
			CurrentXP:    int64(lvl.CurrentXp),
			XPForNextLvl: int64(lvl.XpForNextLvl),
		}
		if u, ok := names[id]; ok {
			entry.Name = u.DisplayName
			entry.Avatar = avatarURL(id, u.Avatar)
			entry.Left = !u.Member
		}
		rendered = append(rendered, entry)
	}

	p := s.newPage(r, "Leaderboard")
	p.Nav = "leaderboard"
	s.withGuild(r, p, guildID)
	p.Data = map[string]any{
		"Rows":       rendered,
		"Sort":       sort,
		"Pagination": newPagination(page, pageSize, total, filterQuery(query)),
	}

	s.render(w, r, "leaderboard", "leaderboard-table", p)
}
