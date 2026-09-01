package dashboard

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// modlogRow is one rendered row: the database record plus the names the bot
// resolved for it.
type modlogRow struct {
	db.Modlog
	UserName      string
	UserAvatar    string
	ModeratorName string
	Expired       bool
}

// pagination is the shared paging state for every table view.
type pagination struct {
	Page       int
	PageSize   int
	Total      int64
	TotalPages int
	HasPrev    bool
	HasNext    bool
	PrevPage   int
	NextPage   int
	// Query is the current filter set, re-encoded so page links keep the
	// filters the user chose.
	Query string
}

func newPagination(page, pageSize int, total int64, query string) pagination {
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages < 1 {
		totalPages = 1
	}
	return pagination{
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
		HasPrev:    page > 1,
		HasNext:    page < totalPages,
		PrevPage:   page - 1,
		NextPage:   page + 1,
		Query:      query,
	}
}

func (s *Server) handleModlogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	guildID := guildFrom(ctx)
	query := r.URL.Query()

	page, pageSize := parsePaging(query)

	filters := db.DashModlogsParams{
		GuildID:     int64(guildID),
		Limit:       int32(pageSize),
		Offset:      int32((page - 1) * pageSize),
		LogType:     optText(query.Get("type")),
		UserID:      optInt8(query.Get("user")),
		ModeratorID: optInt8(query.Get("moderator")),
		Active:      optBool(query.Get("active")),
		After:       optTimestamp(query.Get("after"), s.opts.Location, false),
		Before:      optTimestamp(query.Get("before"), s.opts.Location, true),
	}

	rows, err := s.queries.DashModlogs(ctx, filters)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	total, err := s.queries.DashModlogsCount(ctx, db.DashModlogsCountParams{
		GuildID:     filters.GuildID,
		LogType:     filters.LogType,
		UserID:      filters.UserID,
		ModeratorID: filters.ModeratorID,
		Active:      filters.Active,
		After:       filters.After,
		Before:      filters.Before,
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// One batch call for every snowflake on the page. Resolving them one at a
	// time would be up to 100 REST calls per paint.
	ids := make([]string, 0, len(rows)*2)
	for _, row := range rows {
		ids = append(ids,
			strconv.FormatInt(row.UserID, 10),
			strconv.FormatInt(row.ModeratorID, 10))
	}
	names := s.bots.ResolveUsers(ctx, guildID, dedupe(ids))

	now := time.Now().UTC()
	rendered := make([]modlogRow, 0, len(rows))
	for _, row := range rows {
		userID := strconv.FormatInt(row.UserID, 10)
		modID := strconv.FormatInt(row.ModeratorID, 10)

		entry := modlogRow{
			Modlog:        row,
			UserName:      userID,
			ModeratorName: modID,
			UserAvatar:    avatarURL(userID, ""),
			Expired:       row.ExpiresAt.Valid && row.ExpiresAt.Time.Before(now),
		}
		if u, ok := names[userID]; ok {
			entry.UserName = u.DisplayName
			entry.UserAvatar = avatarURL(userID, u.Avatar)
		}
		if m, ok := names[modID]; ok {
			entry.ModeratorName = m.DisplayName
		}
		rendered = append(rendered, entry)
	}

	types, err := s.queries.DashModlogTypes(ctx, int64(guildID))
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	p := s.newPage(r, "Moderation log")
	p.Nav = "modlogs"
	s.withGuild(r, p, guildID)
	p.Data = map[string]any{
		"Rows":       rendered,
		"Types":      types,
		"Pagination": newPagination(page, pageSize, total, filterQuery(query)),
		"Filters": map[string]string{
			"type":      query.Get("type"),
			"user":      query.Get("user"),
			"moderator": query.Get("moderator"),
			"active":    query.Get("active"),
			"after":     query.Get("after"),
			"before":    query.Get("before"),
		},
	}
	s.render(w, r, "modlogs", "modlogs-table", p)
}

func parsePaging(query map[string][]string) (page, pageSize int) {
	page = 1
	if raw := first(query, "page"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}
	pageSize = defaultPageSize
	if raw := first(query, "size"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			pageSize = min(parsed, maxPageSize)
		}
	}
	return page, pageSize
}

func first(query map[string][]string, key string) string {
	if values, ok := query[key]; ok && len(values) > 0 {
		return values[0]
	}
	return ""
}

// filterQuery re-encodes everything except the page number, so a page link
// keeps the filters but not the old page.
func filterQuery(query map[string][]string) string {
	var parts []string
	for key, values := range query {
		if key == "page" || len(values) == 0 || values[0] == "" {
			continue
		}
		parts = append(parts, key+"="+values[0])
	}
	if len(parts) == 0 {
		return ""
	}
	return "&" + strings.Join(parts, "&")
}

func optText(v string) pgtype.Text {
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}

func optInt8(v string) pgtype.Int8 {
	if v == "" {
		return pgtype.Int8{}
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: parsed, Valid: true}
}

func optBool(v string) pgtype.Bool {
	switch v {
	case "true":
		return pgtype.Bool{Bool: true, Valid: true}
	case "false":
		return pgtype.Bool{Bool: false, Valid: true}
	default:
		return pgtype.Bool{}
	}
}

// optTimestamp parses a YYYY-MM-DD filter into the naive UTC value the columns
// actually hold.
//
// The date the user typed is a wall-clock date in their zone, so it is parsed
// in that location and then converted to UTC. Parsing it as UTC directly would
// shift every date filter by the zone offset -- five and a half hours here,
// which is enough to silently drop a day's worth of rows.
//
// endOfDay makes `before` exclusive of the following midnight rather than the
// current one, so "before 2026-01-05" includes the 5th as a user expects.
func optTimestamp(v string, loc *time.Location, endOfDay bool) pgtype.Timestamp {
	if v == "" {
		return pgtype.Timestamp{}
	}
	t, err := time.ParseInLocation("2006-01-02", v, loc)
	if err != nil {
		return pgtype.Timestamp{}
	}
	if endOfDay {
		t = t.AddDate(0, 0, 1)
	}
	return pgtype.Timestamp{Time: t.UTC(), Valid: true}
}

func dedupe(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
