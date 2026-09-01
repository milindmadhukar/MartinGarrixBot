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

// botInvitePermissions is what the bot needs to do its job: manage roles, kick,
// ban, moderate members, read and send messages, and connect to voice for radio.
const botInvitePermissions = "1101927878150"

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
		"Guilds":    choices,
		"Invitable": invitable,
		"Owner":     sess.Owner,
	}
	s.render(w, r, "guilds", "", p)
}

func inviteURL(clientID string, guildID snowflake.ID) string {
	return "https://discord.com/oauth2/authorize" +
		"?client_id=" + clientID +
		"&scope=bot%20applications.commands" +
		"&permissions=" + botInvitePermissions +
		"&guild_id=" + guildID.String()
}
