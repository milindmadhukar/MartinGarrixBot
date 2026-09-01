package dashboard

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/disgoorg/snowflake/v2"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

// panelTimeout bounds one metric panel.
//
// The messages table had no index at all before migration 000015 and is the
// largest thing in the database, so a panel that goes wrong must degrade to
// "this panel timed out" rather than taking the whole page with it.
const panelTimeout = 8 * time.Second

// panels are loaded individually over htmx, so one slow aggregate costs one
// card instead of the whole overview -- and the request log gets per-panel
// timings for free.
var panelNames = []string{
	"summary",
	"growth",
	"messages",
	"heatmap",
	"channels",
	"members",
	"moderation",
	"punishments",
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	guildID := guildFrom(r.Context())

	p := s.newPage(r, "Overview")
	p.Nav = "overview"
	s.withGuild(r, p, guildID)
	p.Data = map[string]any{
		"Panels":     panelNames,
		"WindowDays": windowDays(r),
	}
	s.render(w, r, "overview", "", p)
}

// handlePanel renders one metric card. Each is its own request, so a failure is
// scoped to the card that failed.
func (s *Server) handlePanel(w http.ResponseWriter, r *http.Request) {
	guildID := guildFrom(r.Context())
	panel := r.PathValue("panel")
	window := windowDays(r)

	ctx, cancel := context.WithTimeout(r.Context(), panelTimeout)
	defer cancel()

	p := s.newPage(r, "")
	p.GuildID = guildID.String()

	data := map[string]any{"WindowDays": window, "Panel": panel}
	var err error

	switch panel {
	case "summary":
		var row db.DashGuildOverviewRow
		row, err = s.queries.DashGuildOverview(ctx, db.DashGuildOverviewParams{
			GuildID: int64(guildID), WindowDays: int32(window),
		})
		data["Summary"] = row
		// The live member count only exists on the bot. The gap between it and
		// tracked_users is itself informative, so both are shown.
		if guild, gErr := s.bots.Guild(ctx, guildID); gErr == nil {
			data["MemberCount"] = guild.MemberCount
		}

	case "growth":
		var rows []db.DashJoinLeaveDailyRow
		rows, err = s.queries.DashJoinLeaveDaily(ctx, db.DashJoinLeaveDailyParams{
			GuildID: int64(guildID), Tz: s.opts.TimeZone, WindowDays: int32(window),
		})
		data["Series"] = rows
		data["Max"] = maxJoinLeave(rows)
		var totals db.DashJoinLeaveTotalsRow
		if totals, err = s.queries.DashJoinLeaveTotals(ctx, db.DashJoinLeaveTotalsParams{
			GuildID: int64(guildID), WindowDays: int32(window),
		}); err == nil {
			data["Totals"] = totals
			data["Net"] = totals.Joins - totals.Leaves
		}

	case "messages":
		var rows []db.DashMessagesDailyRow
		rows, err = s.queries.DashMessagesDaily(ctx, db.DashMessagesDailyParams{
			GuildID: int64(guildID), Tz: s.opts.TimeZone, WindowDays: int32(window),
		})
		data["Series"] = rows
		data["Max"] = maxMessages(rows)

	case "heatmap":
		var rows []db.DashActivityHeatmapRow
		rows, err = s.queries.DashActivityHeatmap(ctx, db.DashActivityHeatmapParams{
			GuildID: int64(guildID), Tz: s.opts.TimeZone, WindowDays: int32(window),
		})
		grid, peak := heatmapGrid(rows)
		data["Grid"] = grid
		data["Peak"] = peak

	case "channels":
		var rows []db.DashTopChannelsRow
		rows, err = s.queries.DashTopChannels(ctx, db.DashTopChannelsParams{
			GuildID: int64(guildID), WindowDays: int32(window), Limit: 10,
		})
		data["Channels"] = s.namedChannels(ctx, guildID, rows)

	case "members":
		sort := r.URL.Query().Get("sort")
		if sort != "xp" && sort != "coins" && sort != "messages" {
			sort = "xp"
		}
		var rows []db.DashTopMembersRow
		rows, err = s.queries.DashTopMembers(ctx, db.DashTopMembersParams{
			GuildID: int64(guildID), Sort: sort, Limit: 10,
		})
		data["Sort"] = sort
		data["Members"] = s.namedMembers(ctx, guildID, rows)

	case "moderation":
		var byType []db.DashModlogsByTypeRow
		byType, err = s.queries.DashModlogsByType(ctx, db.DashModlogsByTypeParams{
			GuildID: int64(guildID), WindowDays: int32(window),
		})
		data["ByType"] = byType
		data["MaxType"] = maxByType(byType)

		var mods []db.DashTopModeratorsRow
		if mods, err = s.queries.DashTopModerators(ctx, db.DashTopModeratorsParams{
			GuildID: int64(guildID), WindowDays: int32(window), Limit: 5,
		}); err == nil {
			data["Moderators"] = s.namedModerators(ctx, guildID, mods)
		}

	case "punishments":
		var rows []db.Modlog
		rows, err = s.queries.DashActivePunishments(ctx, db.DashActivePunishmentsParams{
			GuildID: int64(guildID), Limit: 20,
		})
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, strconv.FormatInt(row.UserID, 10))
		}
		names := s.bots.ResolveUsers(ctx, guildID, dedupe(ids))
		out := make([]modlogRow, 0, len(rows))
		for _, row := range rows {
			userID := strconv.FormatInt(row.UserID, 10)
			entry := modlogRow{Modlog: row, UserName: userID, UserAvatar: avatarURL(userID, "")}
			if u, ok := names[userID]; ok {
				entry.UserName = u.DisplayName
				entry.UserAvatar = avatarURL(userID, u.Avatar)
			}
			out = append(out, entry)
		}
		data["Punishments"] = out

	default:
		http.NotFound(w, r)
		return
	}

	if err != nil {
		// A failed panel is not a failed page. Render the card's error state so
		// the rest of the overview keeps working.
		data["Error"] = panelErrorMessage(ctx, err)
	}

	p.Data = data
	s.render(w, r, "overview", "panel-"+panel, p)
}

func panelErrorMessage(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "This panel took too long to load."
	}
	return "This panel could not be loaded."
}

// windowDays reads the ?days filter, clamped to something the indexes can serve.
func windowDays(r *http.Request) int {
	const (
		fallback = defaultWindowDays
		maxDays  = 365
	)
	raw := r.URL.Query().Get("days")
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 1 {
		return fallback
	}
	return min(parsed, maxDays)
}

// namedChannel is a top-channel row with its name resolved.
type namedChannel struct {
	ID       string
	Name     string
	Messages int64
}

func (s *Server) namedChannels(ctx context.Context, guildID snowflake.ID, rows []db.DashTopChannelsRow) []namedChannel {
	lookup := map[string]BotChannel{}
	if channels, err := s.bots.Channels(ctx, guildID); err == nil {
		lookup = ChannelLookup(channels)
	}

	out := make([]namedChannel, 0, len(rows))
	for _, row := range rows {
		id := strconv.FormatInt(row.ChannelID, 10)
		name := "#" + id
		if c, ok := lookup[id]; ok {
			name = "#" + c.Name
		}
		out = append(out, namedChannel{ID: id, Name: name, Messages: row.Messages})
	}
	return out
}

type namedMember struct {
	db.DashTopMembersRow
	ID     string
	Name   string
	Avatar string
}

func (s *Server) namedMembers(ctx context.Context, guildID snowflake.ID, rows []db.DashTopMembersRow) []namedMember {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, strconv.FormatInt(row.ID, 10))
	}
	names := s.bots.ResolveUsers(ctx, guildID, dedupe(ids))

	out := make([]namedMember, 0, len(rows))
	for _, row := range rows {
		id := strconv.FormatInt(row.ID, 10)
		entry := namedMember{DashTopMembersRow: row, ID: id, Name: id, Avatar: avatarURL(id, "")}
		if u, ok := names[id]; ok {
			entry.Name = u.DisplayName
			entry.Avatar = avatarURL(id, u.Avatar)
		}
		out = append(out, entry)
	}
	return out
}

type namedModerator struct {
	db.DashTopModeratorsRow
	ID     string
	Name   string
	Avatar string
}

func (s *Server) namedModerators(ctx context.Context, guildID snowflake.ID, rows []db.DashTopModeratorsRow) []namedModerator {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, strconv.FormatInt(row.ModeratorID, 10))
	}
	names := s.bots.ResolveUsers(ctx, guildID, dedupe(ids))

	out := make([]namedModerator, 0, len(rows))
	for _, row := range rows {
		id := strconv.FormatInt(row.ModeratorID, 10)
		entry := namedModerator{DashTopModeratorsRow: row, ID: id, Name: id, Avatar: avatarURL(id, "")}
		if u, ok := names[id]; ok {
			entry.Name = u.DisplayName
			entry.Avatar = avatarURL(id, u.Avatar)
		}
		out = append(out, entry)
	}
	return out
}

// heatmapCell is one hour of one weekday.
type heatmapCell struct {
	DOW      int
	Hour     int
	Messages int64
	// Intensity is 0-100, used directly as a CSS opacity percentage.
	Intensity int
}

// heatmapGrid densifies the sparse (dow, hour) rows into a full 7x24 grid.
// Missing cells must render as zero rather than being absent, or the grid
// collapses and every column shifts.
func heatmapGrid(rows []db.DashActivityHeatmapRow) ([][]heatmapCell, int64) {
	var peak int64
	byKey := make(map[[2]int]int64, len(rows))
	for _, row := range rows {
		byKey[[2]int{int(row.Dow), int(row.Hour)}] = row.Messages
		if row.Messages > peak {
			peak = row.Messages
		}
	}

	grid := make([][]heatmapCell, 7)
	for dow := range grid {
		grid[dow] = make([]heatmapCell, 24)
		for hour := range grid[dow] {
			count := byKey[[2]int{dow, hour}]
			intensity := 0
			if peak > 0 {
				intensity = int(count * 100 / peak)
			}
			grid[dow][hour] = heatmapCell{
				DOW: dow, Hour: hour, Messages: count, Intensity: intensity,
			}
		}
	}
	return grid, peak
}

func maxJoinLeave(rows []db.DashJoinLeaveDailyRow) int64 {
	var maximum int64 = 1
	for _, row := range rows {
		maximum = max(maximum, row.Joins, row.Leaves)
	}
	return maximum
}

func maxMessages(rows []db.DashMessagesDailyRow) int64 {
	var maximum int64 = 1
	for _, row := range rows {
		maximum = max(maximum, row.Messages)
	}
	return maximum
}

func maxByType(rows []db.DashModlogsByTypeRow) int64 {
	var maximum int64 = 1
	for _, row := range rows {
		maximum = max(maximum, row.Count)
	}
	return maximum
}
