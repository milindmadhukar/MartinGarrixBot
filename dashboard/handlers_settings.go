package dashboard

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

// settingKind decides which lookup resolves a setting's ID to a name, and in
// phase 2 which dropdown the field gets.
type settingKind int

const (
	kindTextChannel settingKind = iota
	kindVoiceChannel
	kindRole
)

// setting is one row of the settings page.
type setting struct {
	Key   string
	Label string
	Help  string
	Kind  settingKind

	// Set reports whether the column is non-null, so an unconfigured setting
	// reads as "Not set" rather than as a zero snowflake.
	Set   bool
	ID    string
	Name  string
	Known bool
}

// settingGroup keeps related settings together on the page.
type settingGroup struct {
	Title    string
	Settings []setting
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	guildID := guildFrom(ctx)

	guild, err := s.queries.GetGuild(ctx, int64(guildID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The bot creates this row on GuildJoin and backfills on ready, so
			// a missing row means the bot has not seen the guild since being
			// added rather than anything the user did wrong.
			s.renderError(w, r, http.StatusNotFound, "No configuration yet",
				"The bot has not finished setting this server up. Try again in a minute.")
			return
		}
		s.serverError(w, r, err)
		return
	}

	// Names come from the bot. If it is unreachable every setting still renders
	// with its raw ID, flagged degraded, rather than the page failing.
	channels := map[string]BotChannel{}
	roles := map[string]BotRole{}
	degraded := false
	if list, cErr := s.bots.Channels(ctx, guildID); cErr == nil {
		channels = ChannelLookup(list)
	} else {
		degraded = true
	}
	if list, rErr := s.bots.Roles(ctx, guildID); rErr == nil {
		roles = RoleLookup(list)
	} else {
		degraded = true
	}

	resolve := func(key, label, help string, kind settingKind, value pgtype.Int8) setting {
		out := setting{Key: key, Label: label, Help: help, Kind: kind, Set: value.Valid}
		if !value.Valid {
			return out
		}
		out.ID = strconv.FormatInt(value.Int64, 10)
		out.Name = out.ID
		switch kind {
		case kindRole:
			if role, ok := roles[out.ID]; ok {
				out.Name, out.Known = "@"+role.Name, true
			}
		default:
			if channel, ok := channels[out.ID]; ok {
				out.Name, out.Known = "#"+channel.Name, true
			}
		}
		return out
	}

	groups := []settingGroup{
		{
			Title: "Logging",
			Settings: []setting{
				resolve("modlogs_channel", "Moderation log", "Where kicks, bans and mutes are posted.", kindTextChannel, guild.ModlogsChannel),
				resolve("leave_join_logs_channel", "Join & leave log", "Where member joins and leaves are posted.", kindTextChannel, guild.LeaveJoinLogsChannel),
				resolve("delete_logs_channel", "Deleted messages", "Where deleted messages are relayed.", kindTextChannel, guild.DeleteLogsChannel),
				resolve("edit_logs_channel", "Edited messages", "Where edited messages are relayed.", kindTextChannel, guild.EditLogsChannel),
				resolve("welcomes_channel", "Welcomes", "Where new members are greeted.", kindTextChannel, guild.WelcomesChannel),
			},
		},
		{
			Title: "Notifications",
			Settings: []setting{
				resolve("youtube_notifications_channel", "YouTube channel", "New YouTube uploads.", kindTextChannel, guild.YoutubeNotificationsChannel),
				resolve("youtube_notifications_role", "YouTube role", "Pinged for new uploads.", kindRole, guild.YoutubeNotificationsRole),
				resolve("reddit_notifications_channel", "Reddit channel", "New Reddit posts.", kindTextChannel, guild.RedditNotificationsChannel),
				resolve("reddit_notifications_role", "Reddit role", "Pinged for new posts.", kindRole, guild.RedditNotificationsRole),
				resolve("stmpd_notifications_channel", "STMPD channel", "New releases.", kindTextChannel, guild.StmpdNotificationsChannel),
				resolve("stmpd_notifications_role", "STMPD role", "Pinged for new releases.", kindRole, guild.StmpdNotificationsRole),
				resolve("tour_notifications_channel", "Tour channel", "New tour dates.", kindTextChannel, guild.TourNotificationsChannel),
				resolve("tour_notifications_role", "Tour role", "Pinged for new tour dates.", kindRole, guild.TourNotificationsRole),
				resolve("news_role", "News role", "General announcements ping.", kindRole, guild.NewsRole),
			},
		},
		{
			Title: "Roles & channels",
			Settings: []setting{
				resolve("moderator_role", "Moderator role", "Members with this role may use moderation commands.", kindRole, guild.ModeratorRole),
				resolve("bot_channel", "Bot channel", "Where bot commands are expected.", kindTextChannel, guild.BotChannel),
				resolve("radio_voice_channel", "Radio voice channel", "Where the radio auto-connects.", kindVoiceChannel, guild.RadioVoiceChannel),
			},
		},
	}

	p := s.newPage(r, "Settings")
	p.Nav = "settings"
	s.withGuild(r, p, guildID)
	p.Degraded = p.Degraded || degraded
	p.Data = map[string]any{
		"Groups":       groups,
		"XPMultiplier": xpMultiplier(guild),
		// v1 displays configuration; editing is phase 2. The template says so
		// rather than showing inputs that do nothing.
		"ReadOnly": true,
	}
	s.render(w, r, "settings", "", p)
}

func xpMultiplier(guild db.Guild) float64 {
	return guild.XpMultiplier
}
