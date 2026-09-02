package dashboard

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

// settingKind decides which lookup resolves a setting's ID to a name, and which
// dropdown the field gets.
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

	// Inert marks a setting the bot stores but never reads. Rendering these as
	// ordinary settings would imply they do something.
	Inert bool

	// Options are the choices for this field's dropdown.
	Options []settingOption
}

type settingOption struct {
	ID       string
	Label    string
	Selected bool
}

// settingGroup keeps related settings together on the page. Each group is its
// own form, so a save touches one section rather than the whole page.
type settingGroup struct {
	ID       string
	Title    string
	Help     string
	Settings []setting
}

// xpMultiplierKey is handled outside the channel/role machinery: it is the one
// numeric setting on the page.
const xpMultiplierKey = "xp_multiplier"

const (
	minXPMultiplier = 0.1
	maxXPMultiplier = 5.0
)

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	guildID := guildFrom(ctx)

	guild, err := s.queries.GetGuild(ctx, int64(guildID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The bot creates this row on GuildJoin and backfills on ready, so
			// a missing row means the bot has not finished setting this guild
			// up rather than anything the user did wrong.
			s.renderError(w, r, http.StatusNotFound, "No configuration yet",
				"The bot has not finished setting this server up. Try again in a minute.")
			return
		}
		s.serverError(w, r, err)
		return
	}

	p := s.newPage(r, "Settings")
	p.Nav = "settings"
	s.withGuild(r, p, guildID)
	s.renderSettings(w, r, p, guild, "", nil)
}

// handleSettingsSave writes one group of settings.
//
// Every field in the group is submitted on every save, including the empty ones,
// so that clearing a setting works: an absent value means "unset this", which a
// partial-update query could not express.
func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	guildID := guildFrom(ctx)
	sess, _ := sessionFrom(ctx)

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Bad request",
			"That form could not be read.")
		return
	}

	guild, err := s.queries.GetGuild(ctx, int64(guildID))
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// Validation needs the guild's real channels and roles. If the bot cannot
	// be reached we cannot check that a submitted ID even belongs to this
	// guild, and writing an unvalidated snowflake is worse than not saving.
	channels, cErr := s.bots.Channels(ctx, guildID)
	roles, rErr := s.bots.Roles(ctx, guildID)
	if cErr != nil || rErr != nil {
		p := s.newPage(r, "Settings")
		p.Nav = "settings"
		p.Degraded = true
		s.withGuild(r, p, guildID)
		s.renderSettings(w, r, p, guild,
			"Cannot save while the bot is unreachable — settings could not be checked against this server.", nil)
		return
	}

	params, problems := s.buildUpdate(guild, r.PostForm, guildID, channels, roles)
	if len(problems) > 0 {
		p := s.newPage(r, "Settings")
		p.Nav = "settings"
		s.withGuild(r, p, guildID)
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderSettings(w, r, p, guild, strings.Join(problems, " "), nil)
		return
	}

	changed := changedFields(guild, params)

	updated, err := s.queries.UpdateGuildConfig(ctx, params)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// A privileged action taken over the web deserves an audit trail.
	slog.Info("Dashboard settings saved",
		slog.String("guild_id", guildID.String()),
		slog.String("user_id", sess.UserID.String()),
		slog.Any("changed", changed))

	p := s.newPage(r, "Settings")
	p.Nav = "settings"
	s.withGuild(r, p, guildID)
	s.renderSettings(w, r, p, updated, "", changed)
}

// renderSettings builds the page model from whatever the database currently
// holds, so a save always re-renders the stored truth rather than the submitted
// form.
func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, p *pageData, guild db.Guild, problem string, saved []string) {
	ctx := r.Context()
	guildID := guildFrom(ctx)

	channels := map[string]BotChannel{}
	roles := map[string]BotRole{}
	var channelList []BotChannel
	var roleList []BotRole

	if list, err := s.bots.Channels(ctx, guildID); err == nil {
		channelList = list
		channels = ChannelLookup(list)
	} else {
		p.Degraded = true
	}
	if list, err := s.bots.Roles(ctx, guildID); err == nil {
		roleList = list
		roles = RoleLookup(list)
	} else {
		p.Degraded = true
	}

	build := func(key, label, help string, kind settingKind, value pgtype.Int8, inert bool) setting {
		out := setting{Key: key, Label: label, Help: help, Kind: kind, Set: value.Valid, Inert: inert}
		if value.Valid {
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
		}
		out.Options = optionsFor(kind, out.ID, guildID, channelList, roleList)
		return out
	}

	groups := []settingGroup{
		{
			ID:    "logging",
			Title: "Logging",
			Help:  "Where the bot reports things that happen in the server. Leave one unset to turn it off.",
			Settings: []setting{
				build("modlogs_channel", "Moderation log", "Kicks, bans and timeouts — including ones done through Discord itself.", kindTextChannel, guild.ModlogsChannel, false),
				build("leave_join_logs_channel", "Joins & leaves", "Members joining and leaving.", kindTextChannel, guild.LeaveJoinLogsChannel, false),
				build("delete_logs_channel", "Deleted messages", "Deleted messages, with their content.", kindTextChannel, guild.DeleteLogsChannel, false),
				build("edit_logs_channel", "Edited messages", "Edited messages, before and after.", kindTextChannel, guild.EditLogsChannel, false),
				build("voice_logs_channel", "Voice activity", "Members joining, leaving and moving between voice channels.", kindTextChannel, guild.VoiceLogsChannel, false),
				build("member_logs_channel", "Profile changes", "Avatar, username, nickname and role changes.", kindTextChannel, guild.MemberLogsChannel, false),
			},
		},
		{
			ID:    "notifications",
			Title: "Notifications",
			Help:  "New releases and posts. A role is pinged only when both the channel and the role are set.",
			Settings: []setting{
				build("youtube_notifications_channel", "YouTube channel", "New uploads.", kindTextChannel, guild.YoutubeNotificationsChannel, false),
				build("youtube_notifications_role", "YouTube role", "Pinged for new uploads.", kindRole, guild.YoutubeNotificationsRole, false),
				build("reddit_notifications_channel", "Reddit channel", "New posts.", kindTextChannel, guild.RedditNotificationsChannel, false),
				build("reddit_notifications_role", "Reddit role", "Pinged for new posts.", kindRole, guild.RedditNotificationsRole, false),
				build("stmpd_notifications_channel", "STMPD channel", "New releases.", kindTextChannel, guild.StmpdNotificationsChannel, false),
				build("stmpd_notifications_role", "STMPD role", "Pinged for new releases.", kindRole, guild.StmpdNotificationsRole, false),
				build("tour_notifications_channel", "Tour channel", "New tour dates.", kindTextChannel, guild.TourNotificationsChannel, false),
				build("tour_notifications_role", "Tour role", "Pinged for new tour dates.", kindRole, guild.TourNotificationsRole, false),
				build("anniversary_notifications_channel", "Anniversaries channel", "Song release anniversaries.", kindTextChannel, guild.AnniversaryNotificationsChannel, false),
				build("anniversary_notifications_role", "Anniversaries role", "Pinged for anniversaries.", kindRole, guild.AnniversaryNotificationsRole, false),
			},
		},
		{
			ID:    "access",
			Title: "Access & radio",
			Help:  "Who may use moderation commands, and where the radio connects.",
			Settings: []setting{
				build("moderator_role", "Moderator role", "Members with this role may use the moderation commands.", kindRole, guild.ModeratorRole, false),
				build("radio_voice_channel", "Radio voice channel", "The radio auto-connects here on startup.", kindVoiceChannel, guild.RadioVoiceChannel, false),
			},
		},
		{
			ID:    "inert",
			Title: "Not wired up yet",
			Help:  "These are stored, but no bot feature reads them today. Setting one has no effect until the corresponding feature is built.",
			Settings: []setting{
				build("welcomes_channel", "Welcomes", "No welcome message is sent; joins go to the join & leave log instead.", kindTextChannel, guild.WelcomesChannel, true),
				build("bot_channel", "Bot channel", "Nothing restricts commands to a channel.", kindTextChannel, guild.BotChannel, true),
				build("news_role", "News role", "Never pinged.", kindRole, guild.NewsRole, true),
			},
		},
	}

	p.Data = map[string]any{
		"Groups":       groups,
		"XPMultiplier": guild.XpMultiplier,
		"XPKey":        xpMultiplierKey,
		"XPMin":        minXPMultiplier,
		"XPMax":        maxXPMultiplier,
		"Anniversary": map[string]any{
			"Hour":     guild.AnniversaryHour,
			"Timezone": guild.AnniversaryTimezone,
			"Hours":    hourChoices(guild.AnniversaryHour),
		},
		"Problem": problem,
		"Saved":   saved,
	}
	s.render(w, r, "settings", "settings-form", p)
}

// optionsFor builds a dropdown, always with a "Not set" entry first so a
// setting can be cleared.
func optionsFor(kind settingKind, selected string, guildID snowflake.ID, channels []BotChannel, roles []BotRole) []settingOption {
	options := []settingOption{{ID: "", Label: "— Not set —", Selected: selected == ""}}

	switch kind {
	case kindRole:
		for _, role := range roles {
			// @everyone has the guild's own ID and is never a useful choice;
			// managed roles belong to integrations and cannot be assigned.
			if role.ID == guildID.String() || role.Managed {
				continue
			}
			options = append(options, settingOption{
				ID: role.ID, Label: "@" + role.Name, Selected: role.ID == selected,
			})
		}
	case kindVoiceChannel:
		for _, channel := range channels {
			if channel.Type != ChannelTypeVoice {
				continue
			}
			options = append(options, settingOption{
				ID: channel.ID, Label: "🔊 " + channel.Name, Selected: channel.ID == selected,
			})
		}
	default:
		for _, channel := range channels {
			if !channel.IsText() {
				continue
			}
			options = append(options, settingOption{
				ID: channel.ID, Label: "#" + channel.Name, Selected: channel.ID == selected,
			})
		}
	}

	// A configured ID the bot cannot see any more (deleted channel, or the bot
	// lost access) would otherwise vanish from the dropdown and be silently
	// cleared on the next save.
	if selected != "" && !slices.ContainsFunc(options, func(o settingOption) bool { return o.ID == selected }) {
		options = append(options, settingOption{
			ID: selected, Label: selected + " (not found in this server)", Selected: true,
		})
	}
	return options
}

func hourChoices(selected int32) []settingOption {
	out := make([]settingOption, 0, 24)
	for h := range int32(24) {
		out = append(out, settingOption{
			ID:       strconv.Itoa(int(h)),
			Label:    fmt.Sprintf("%02d:00", h),
			Selected: h == selected,
		})
	}
	return out
}
