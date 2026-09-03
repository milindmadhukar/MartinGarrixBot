package dashboard

import (
	"net/http"

	"github.com/disgoorg/snowflake/v2"
)

// guildChoice is one row in the picker.
type guildChoice struct {
	ID          string
	Name        string
	IconURL     string
	MemberCount int
	// Invitable marks a guild the user administers that the bot is not in, so
	// the row offers an invite link instead of a dashboard link.
	Invitable bool
	InviteURL string
}

// botInvitePermissions is what the bot needs to do its job.
//
// The previous value (1101927878150) was missing four permissions the bot
// actually depends on, all of which fail silently rather than loudly:
//
//   - VIEW_AUDIT_LOG (0x80) gates delivery of GUILD_AUDIT_LOG_ENTRY_CREATE
//     entirely. Without it the bot never sees moderation done through Discord's
//     own UI, and the moderation log just reads zero.
//   - MODERATE_MEMBERS (0x400000000) is what /moderation mute needs: it applies
//     a native Discord timeout via CommunicationDisabledUntil.
//   - CONNECT and SPEAK (0x100000, 0x200000) are what the radio needs to join a
//     voice channel at all.
//
// Also added: MANAGE_MESSAGES (the bot deletes legacy `mg.` prefix messages),
// EMBED_LINKS and ATTACH_FILES (embeds and rank cards).
//
// This only affects NEW invites. A guild that added the bot earlier keeps
// whatever it granted then, which is why the moderation page checks the bot's
// live permissions and says so when View Audit Log is missing.
const botInvitePermissions = "1119110950534"

func (s *Server) handleGuildList(w http.ResponseWriter, r *http.Request) {
	sess, _ := sessionFrom(r.Context())

	// A single guild has no choice to offer, so go straight there -- but never
	// trap the user: ?pick=1 always renders the list, and the nav links to it,
	// so someone who later joins a second server is not stuck on a page that
	// bounces them.
	if len(sess.Eligible) == 1 && r.URL.Query().Get("pick") != "1" {
		http.Redirect(w, r, "/g/"+sess.Eligible[0].String(), http.StatusSeeOther)
		return
	}

	// Names and icons come from the bot rather than the user's OAuth snapshot:
	// the bot's copy is authoritative and fresher, which is why only IDs are
	// kept in the session cookie.
	byID := make(map[string]BotGuild)
	botGuilds, err := s.bots.Guilds(r.Context())
	degraded := err != nil
	for _, g := range botGuilds {
		byID[g.ID] = g
	}

	choices := make([]guildChoice, 0, len(sess.Eligible))
	for _, id := range sess.Eligible {
		choice := guildChoice{ID: id.String(), Name: id.String()}
		if g, ok := byID[id.String()]; ok {
			choice.Name = g.Name
			choice.IconURL = guildIconURL(g.ID, g.Icon)
			choice.MemberCount = g.MemberCount
		}
		choices = append(choices, choice)
	}

	invitable := make([]guildChoice, 0, len(sess.Missing))
	for _, m := range sess.Missing {
		invitable = append(invitable, guildChoice{
			ID:        m.ID.String(),
			Name:      m.Name,
			Invitable: true,
			InviteURL: inviteURL(s.opts.ClientID, m.ID),
		})
	}

	p := s.newPage(r, "Your servers")
	p.Nav = "guilds"
	p.Degraded = degraded
	p.Data = map[string]any{
		"Guilds":     choices,
		"Invitable":  invitable,
		"SuperAdmin": sess.SuperAdmin,
	}
	s.render(w, r, "guilds", "", p)
}

// generalInviteURL is the invite with no guild preselected, for the refused
// login page where there may be no particular server to point at.
func generalInviteURL(clientID string) string {
	return "https://discord.com/oauth2/authorize" +
		"?client_id=" + clientID +
		"&scope=bot%20applications.commands" +
		"&permissions=" + botInvitePermissions
}

func inviteURL(clientID string, guildID snowflake.ID) string {
	return "https://discord.com/oauth2/authorize" +
		"?client_id=" + clientID +
		"&scope=bot%20applications.commands" +
		"&permissions=" + botInvitePermissions +
		"&guild_id=" + guildID.String()
}
