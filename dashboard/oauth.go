package dashboard

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/oauth2"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/STMPDBot/dashboard/session"
)

// The OAuth handshake is the only place the dashboard talks to Discord
// directly. It asks for `identify guilds` and nothing else: the permissions
// bitfield on GET /users/@me/guilds already answers the only question we have,
// and guilds.members.read would cost one REST call per guild to learn the same
// thing.
//
// The access token is deliberately discarded at the end of the callback. It is
// used to read the user and their guilds, the eligible set is derived, and then
// it goes out of scope -- so there is no bearer token in a cookie, nothing to
// refresh, and no token custody to get wrong.

const (
	authorizeURL = "https://discord.com/oauth2/authorize"
	// discordEpochShift is how Discord derives the default-avatar index from a
	// user ID.
	discordEpochShift = 22
	defaultAvatars    = 6
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := session.RandomToken()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.sessions.WriteState(w, state)

	// prompt=none skips the consent screen for a user who has already
	// authorised, which matters because refreshing a stale guild list re-runs
	// this whole flow. handleCallback retries without it if consent is needed.
	params := url.Values{
		"client_id":     {s.opts.ClientID},
		"redirect_uri":  {s.opts.RedirectURI},
		"response_type": {"code"},
		"scope":         {"identify guilds"},
		"state":         {state},
	}
	if r.URL.Query().Get("consent") != "1" {
		params.Set("prompt", "none")
	}

	http.Redirect(w, r, authorizeURL+"?"+params.Encode(), http.StatusSeeOther)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// prompt=none returns an error instead of a consent screen when the user
	// has revoked the app. Without this retry such a user could never log in
	// again, and the error page would not say why.
	if errCode := query.Get("error"); errCode != "" {
		if errCode == "consent_required" || errCode == "interaction_required" {
			http.Redirect(w, r, "/login?consent=1", http.StatusSeeOther)
			return
		}
		s.renderError(w, r, http.StatusBadRequest,
			"Discord declined the login", "Discord returned: "+errCode)
		return
	}

	want := s.sessions.ReadState(w, r)
	got := query.Get("state")
	if want == "" || subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
		s.renderError(w, r, http.StatusBadRequest, "Login expired",
			"That login attempt is no longer valid. Please try again.")
		return
	}

	code := query.Get("code")
	if code == "" {
		s.renderError(w, r, http.StatusBadRequest, "Login failed",
			"Discord did not return an authorization code.")
		return
	}

	token, err := s.oauth.Rest.GetAccessToken(
		s.oauth.ID, s.opts.ClientSecret, code, s.opts.RedirectURI)
	if err != nil {
		slog.Error("OAuth token exchange failed", slog.Any("err", err))
		s.renderError(w, r, http.StatusBadGateway, "Login failed",
			"Could not complete the exchange with Discord. Please try again.")
		return
	}

	// Built by hand rather than via StartSession, which would route state
	// through disgo's in-memory StateController -- a map that loses every
	// pending login on restart. State is handled with a cookie instead.
	discordSession := oauth2.Session{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Scopes:       token.Scope,
		TokenType:    token.TokenType,
		Expiration:   time.Now().Add(token.ExpiresIn * time.Second),
	}

	user, err := s.oauth.GetUser(discordSession)
	if err != nil {
		slog.Error("OAuth user fetch failed", slog.Any("err", err))
		s.renderError(w, r, http.StatusBadGateway, "Login failed",
			"Could not read your Discord profile.")
		return
	}

	userGuilds, err := s.oauth.GetGuilds(discordSession)
	if err != nil {
		slog.Error("OAuth guild fetch failed", slog.Any("err", err))
		s.renderError(w, r, http.StatusBadGateway, "Login failed",
			"Discord would not list your servers. This is usually rate limiting; try again shortly.")
		return
	}

	sess, err := s.sessions.New(user.ID, displayName(user), avatarHash(user))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	sess.Owner = s.opts.OwnerIDs[user.ID.String()]

	if err := s.applyEligibility(r, &sess, userGuilds); err != nil {
		slog.Error("Could not determine guild eligibility", slog.Any("err", err))
		s.renderError(w, r, http.StatusBadGateway, "The bot is unreachable",
			"The dashboard could not ask the bot which servers it is in. Try again in a moment.")
		return
	}

	// No eligible guild means no session at all.
	//
	// This is the only gate on the dashboard now that Authelia no longer fronts
	// it, so "signed in but with nothing to administer" is not a state worth
	// having: it would hand a session cookie to anyone with a Discord account.
	// Owners are exempt by definition -- applyEligibility gives them every guild
	// the bot is in.
	if len(sess.Eligible) == 0 {
		slog.Warn("Dashboard login refused: no administered guilds",
			slog.String("user_id", user.ID.String()),
			slog.String("username", user.Username),
			slog.Int("administered_elsewhere", len(sess.Missing)))

		s.renderNoAccess(w, r, sess.Missing)
		return
	}

	if err := s.sessions.Write(w, sess); err != nil {
		s.serverError(w, r, err)
		return
	}

	slog.Info("Dashboard login",
		slog.String("user_id", user.ID.String()),
		slog.Int("eligible_guilds", len(sess.Eligible)),
		slog.Bool("owner", sess.Owner))

	// A single administered guild has no choice to offer, so skip the picker.
	if len(sess.Eligible) == 1 {
		http.Redirect(w, r, "/g/"+sess.Eligible[0].String(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/guilds", http.StatusSeeOther)
}

// applyEligibility intersects the guilds the user administers with the guilds
// the bot is actually in:
//
//	eligible = administered AND bot-present   (or every bot guild, for an owner)
//
// The guilds the user administers but the bot is missing from are kept
// separately so the picker can offer an invite link for each.
func (s *Server) applyEligibility(r *http.Request, sess *session.Session, userGuilds []discord.OAuth2Guild) error {
	botGuilds, err := s.bots.Guilds(r.Context())
	if err != nil {
		return err
	}

	present := make(map[string]struct{}, len(botGuilds))
	for _, g := range botGuilds {
		present[g.ID] = struct{}{}
	}

	// An owner administers everything the bot is in, whatever Discord says
	// about their permissions there.
	if sess.Owner {
		eligible := make([]snowflake.ID, 0, len(botGuilds))
		for _, g := range botGuilds {
			if id, err := snowflake.Parse(g.ID); err == nil {
				eligible = append(eligible, id)
			}
		}
		sess.Eligible = eligible
		sess.Missing = nil
		sess.GuildsAt = time.Now().Unix()
		return nil
	}

	var (
		eligible []snowflake.ID
		missing  []session.MissingGuild
	)
	for _, g := range userGuilds {
		if !administers(g) {
			continue
		}
		if _, ok := present[g.ID.String()]; ok {
			eligible = append(eligible, g.ID)
			continue
		}
		missing = append(missing, session.MissingGuild{ID: g.ID, Name: g.Name})
	}

	slices.Sort(eligible)
	sess.Eligible = eligible
	sess.Missing = missing
	sess.GuildsAt = time.Now().Unix()
	return nil
}

// administers reports whether the user may configure a guild.
//
// Owner is checked separately from the permission bit because Discord marks the
// owner without necessarily granting ADMINISTRATOR through any role. The
// Permissions field here is Discord's own computed value for this user in this
// guild, already accounting for role inheritance -- it must not be recomputed.
func administers(g discord.OAuth2Guild) bool {
	return g.Owner || g.Permissions.Has(discord.PermissionAdministrator)
}

// renderNoAccess explains a refused login without issuing a session.
//
// Someone who administers servers the bot has not been added to still gets the
// invite link -- that is the one action that would make them eligible, and
// offering it grants nothing.
func (s *Server) renderNoAccess(w http.ResponseWriter, r *http.Request, missing []session.MissingGuild) {
	invitable := make([]guildChoice, 0, len(missing))
	for _, m := range missing {
		invitable = append(invitable, guildChoice{
			ID:        m.ID.String(),
			Name:      m.Name,
			Invitable: true,
			InviteURL: inviteURL(s.opts.ClientID, m.ID),
		})
	}

	p := s.newPage(r, "No access")
	p.Bare = true
	p.Data = map[string]any{
		"Invitable": invitable,
		"InviteURL": generalInviteURL(s.opts.ClientID),
	}

	w.WriteHeader(http.StatusForbidden)
	s.render(w, r, "noaccess", "", p)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.Clear(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func displayName(u *discord.OAuth2User) string {
	if u.GlobalName != nil && *u.GlobalName != "" {
		return *u.GlobalName
	}
	return u.Username
}

func avatarHash(u *discord.OAuth2User) string {
	if u.Avatar == nil {
		return ""
	}
	return *u.Avatar
}

// avatarURL builds a CDN URL from a hash, falling back to Discord's default
// avatar set. A broken image on every row of the modlog table is exactly the
// kind of detail that makes a dashboard feel unfinished.
func avatarURL(userID, hash string) string {
	if hash == "" {
		id, err := snowflake.Parse(userID)
		if err != nil {
			return "https://cdn.discordapp.com/embed/avatars/0.png"
		}
		index := (uint64(id) >> discordEpochShift) % defaultAvatars
		return "https://cdn.discordapp.com/embed/avatars/" + strconv.FormatUint(index, 10) + ".png"
	}
	ext := "png"
	if strings.HasPrefix(hash, "a_") {
		ext = "gif"
	}
	return "https://cdn.discordapp.com/avatars/" + userID + "/" + hash + "." + ext + "?size=64"
}

func guildIconURL(guildID, hash string) string {
	if hash == "" {
		return ""
	}
	ext := "png"
	if strings.HasPrefix(hash, "a_") {
		ext = "gif"
	}
	return "https://cdn.discordapp.com/icons/" + guildID + "/" + hash + "." + ext + "?size=64"
}
