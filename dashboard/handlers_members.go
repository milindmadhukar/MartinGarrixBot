package dashboard

import (
	"net/http"
	"strconv"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

// memberLogRow is one join/leave event with the member's name resolved.
type memberLogRow struct {
	db.JoinLeaveLog
	MemberName   string
	MemberAvatar string
	MemberLeft   bool
	IsJoin       bool
}

func (s *Server) handleMemberLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	guildID := guildFrom(ctx)
	query := r.URL.Query()

	page, pageSize := parsePaging(query)

	action := query.Get("action")
	if action != "join" && action != "leave" {
		action = ""
	}

	filters := db.DashMemberLogsParams{
		GuildID:  int64(guildID),
		Limit:    int32(pageSize),
		Offset:   int32((page - 1) * pageSize),
		Action:   optText(action),
		MemberID: optInt8(query.Get("member")),
		After:    optTimestamp(query.Get("after"), s.opts.Location, false),
		Before:   optTimestamp(query.Get("before"), s.opts.Location, true),
	}

	rows, err := s.queries.DashMemberLogs(ctx, filters)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	total, err := s.queries.DashMemberLogsCount(ctx, db.DashMemberLogsCountParams{
		GuildID:  filters.GuildID,
		Action:   filters.Action,
		MemberID: filters.MemberID,
		After:    filters.After,
		Before:   filters.Before,
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, strconv.FormatInt(row.MemberID, 10))
	}
	// Most rows here are people who have left, so most lookups miss. That is
	// expected: an unresolved ID renders as the raw snowflake, which is still
	// enough to search Discord with.
	names := s.bots.ResolveUsers(ctx, guildID, dedupe(ids))

	rendered := make([]memberLogRow, 0, len(rows))
	for _, row := range rows {
		memberID := strconv.FormatInt(row.MemberID, 10)
		entry := memberLogRow{
			JoinLeaveLog: row,
			MemberName:   memberID,
			MemberAvatar: avatarURL(memberID, ""),
			IsJoin:       row.Action == "join",
		}
		if u, ok := names[memberID]; ok {
			entry.MemberName = u.DisplayName
			entry.MemberAvatar = avatarURL(memberID, u.Avatar)
			entry.MemberLeft = !u.Member
		}
		rendered = append(rendered, entry)
	}

	p := s.newPage(r, "Join & leave log")
	p.Nav = "members"
	s.withGuild(r, p, guildID)
	p.Data = map[string]any{
		"Rows":       rendered,
		"Pagination": newPagination(page, pageSize, total, filterQuery(query)),
		"Filters": map[string]string{
			"action": action,
			"member": query.Get("member"),
			"after":  query.Get("after"),
			"before": query.Get("before"),
		},
	}
	s.render(w, r, "members", "members-table", p)
}
