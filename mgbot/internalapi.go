package mgbot

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// The internal API lets the dashboard render role and channel pickers, guild
// names and member counts without ever holding the bot token. Everything here
// is served from the disgo cache first and falls back to REST on a miss, which
// is the same cache-first rule the rest of the bot follows.
//
// It listens on its own port rather than sharing the health mux: /health is
// deliberately unauthenticated and is hit by the container HEALTHCHECK, so
// keeping the two on separate listeners means publishing one to the host can
// never accidentally publish the other.

type internalGuild struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon,omitempty"`
	OwnerID     string `json:"owner_id,omitempty"`
	MemberCount int    `json:"member_count"`
}

type internalRole struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    int    `json:"color"`
	Position int    `json:"position"`
	Managed  bool   `json:"managed"`
}

type internalChannel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     int    `json:"type"`
	ParentID string `json:"parent_id,omitempty"`
	Position int    `json:"position"`
}

type internalUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar,omitempty"`
}

type resolveRequest struct {
	GuildID string   `json:"guild_id"`
	UserIDs []string `json:"user_ids"`
}

// maxResolveIDs bounds a single resolve request. Every uncached ID costs one
// REST call, so an unbounded list is a way to make the bot rate-limit itself.
const maxResolveIDs = 100

// StartInternalAPI serves live guild data to the dashboard on its own listener.
//
// It is a no-op when no secret is configured: an unauthenticated endpoint
// listing every guild the bot is in is worse than no endpoint at all, so a
// missing secret disables the server rather than opening it.
func (b *MartinGarrixBot) StartInternalAPI() {
	if b.Cfg.Internal.Secret == "" {
		slog.Info("Internal API disabled: no secret configured")
		return
	}

	addr := b.Cfg.Internal.Address
	if addr == "" {
		addr = ":8082"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/guilds", b.handleInternalGuilds)
	mux.HandleFunc("GET /internal/guilds/{guildID}", b.handleInternalGuild)
	mux.HandleFunc("GET /internal/guilds/{guildID}/roles", b.handleInternalRoles)
	mux.HandleFunc("GET /internal/guilds/{guildID}/channels", b.handleInternalChannels)
	mux.HandleFunc("POST /internal/users/resolve", b.handleInternalResolve)

	srv := &http.Server{
		Addr:              addr,
		Handler:           b.internalAuth(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("Internal API listening", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Internal API stopped", slog.Any("err", err))
		}
	}()
}

// internalAuth compares the presented token in constant time. A plain ==
// leaks the length of the shared prefix to anything that can time the response.
func (b *MartinGarrixBot) internalAuth(next http.Handler) http.Handler {
	want := []byte(b.Cfg.Internal.Secret)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("X-Internal-Token"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (b *MartinGarrixBot) handleInternalGuilds(w http.ResponseWriter, r *http.Request) {
	guilds := make([]internalGuild, 0)
	b.Client.Caches().GuildsForEach(func(g discord.Guild) {
		guilds = append(guilds, internalGuild{
			ID:          g.ID.String(),
			Name:        g.Name,
			Icon:        derefString(g.Icon),
			OwnerID:     g.OwnerID.String(),
			MemberCount: g.MemberCount,
		})
	})
	sort.Slice(guilds, func(i, j int) bool { return guilds[i].Name < guilds[j].Name })
	writeInternalJSON(w, guilds)
}

func (b *MartinGarrixBot) handleInternalGuild(w http.ResponseWriter, r *http.Request) {
	guildID, ok := parseGuildID(w, r)
	if !ok {
		return
	}

	g, found := b.Client.Caches().Guild(guildID)
	if !found {
		// A guild the bot is not in is a 404, not an error: the dashboard
		// uses this to decide whether to offer an invite link.
		http.NotFound(w, r)
		return
	}

	writeInternalJSON(w, internalGuild{
		ID:          g.ID.String(),
		Name:        g.Name,
		Icon:        derefString(g.Icon),
		OwnerID:     g.OwnerID.String(),
		MemberCount: g.MemberCount,
	})
}

func (b *MartinGarrixBot) handleInternalRoles(w http.ResponseWriter, r *http.Request) {
	guildID, ok := parseGuildID(w, r)
	if !ok {
		return
	}

	roles := make([]internalRole, 0)
	b.Client.Caches().RolesForEach(guildID, func(role discord.Role) {
		roles = append(roles, newInternalRole(role))
	})

	// The cache is only populated when cache.FlagRoles is enabled and the
	// guild has been received; fall back to REST so a cold start or a
	// misconfigured cache degrades to slow rather than to empty dropdowns.
	if len(roles) == 0 {
		fetched, err := b.Client.Rest().GetRoles(guildID)
		if err != nil {
			slog.Error("Failed to fetch roles for internal API",
				slog.String("guild_id", guildID.String()), slog.Any("err", err))
			http.Error(w, "failed to fetch roles", http.StatusBadGateway)
			return
		}
		for _, role := range fetched {
			roles = append(roles, newInternalRole(role))
		}
	}

	// Highest role first, matching how Discord itself presents the list.
	sort.Slice(roles, func(i, j int) bool { return roles[i].Position > roles[j].Position })
	writeInternalJSON(w, roles)
}

func (b *MartinGarrixBot) handleInternalChannels(w http.ResponseWriter, r *http.Request) {
	guildID, ok := parseGuildID(w, r)
	if !ok {
		return
	}

	channels := make([]internalChannel, 0)
	// ChannelsForEach is global, not guild-scoped, so this has to filter.
	b.Client.Caches().ChannelsForEach(func(ch discord.GuildChannel) {
		if ch.GuildID() != guildID {
			return
		}
		channels = append(channels, newInternalChannel(ch))
	})

	if len(channels) == 0 {
		fetched, err := b.Client.Rest().GetGuildChannels(guildID)
		if err != nil {
			slog.Error("Failed to fetch channels for internal API",
				slog.String("guild_id", guildID.String()), slog.Any("err", err))
			http.Error(w, "failed to fetch channels", http.StatusBadGateway)
			return
		}
		for _, ch := range fetched {
			channels = append(channels, newInternalChannel(ch))
		}
	}

	sort.Slice(channels, func(i, j int) bool {
		if channels[i].Position != channels[j].Position {
			return channels[i].Position < channels[j].Position
		}
		return channels[i].ID < channels[j].ID
	})
	writeInternalJSON(w, channels)
}

// handleInternalResolve turns the raw snowflakes stored in modlogs and
// join_leave_logs into names the dashboard can render. Unresolvable IDs are
// simply absent from the response — a member who has left the guild is the
// normal case here, not an error.
func (b *MartinGarrixBot) handleInternalResolve(w http.ResponseWriter, r *http.Request) {
	var req resolveRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	guildID, err := snowflake.Parse(req.GuildID)
	if err != nil {
		http.Error(w, "invalid guild id", http.StatusBadRequest)
		return
	}

	if len(req.UserIDs) > maxResolveIDs {
		req.UserIDs = req.UserIDs[:maxResolveIDs]
	}

	out := make(map[string]internalUser, len(req.UserIDs))
	for _, raw := range req.UserIDs {
		userID, err := snowflake.Parse(raw)
		if err != nil {
			continue
		}
		if _, done := out[raw]; done {
			continue
		}

		member, err := utils.GetMember(b.Client, guildID, userID)
		if err != nil || member == nil {
			continue
		}

		display := member.User.Username
		if member.Nick != nil && *member.Nick != "" {
			display = *member.Nick
		} else if member.User.GlobalName != nil && *member.User.GlobalName != "" {
			display = *member.User.GlobalName
		}

		out[raw] = internalUser{
			ID:          userID.String(),
			Username:    member.User.Username,
			DisplayName: display,
			Avatar:      derefString(member.User.Avatar),
		}
	}

	writeInternalJSON(w, out)
}

func newInternalRole(role discord.Role) internalRole {
	return internalRole{
		ID:       role.ID.String(),
		Name:     role.Name,
		Color:    role.Color,
		Position: role.Position,
		Managed:  role.Managed,
	}
}

func newInternalChannel(ch discord.GuildChannel) internalChannel {
	c := internalChannel{
		ID:       ch.ID().String(),
		Name:     ch.Name(),
		Type:     int(ch.Type()),
		Position: ch.Position(),
	}
	if parent := ch.ParentID(); parent != nil {
		c.ParentID = parent.String()
	}
	return c
}

func parseGuildID(w http.ResponseWriter, r *http.Request) (snowflake.ID, bool) {
	guildID, err := snowflake.Parse(r.PathValue("guildID"))
	if err != nil {
		http.Error(w, "invalid guild id", http.StatusBadRequest)
		return 0, false
	}
	return guildID, true
}

func writeInternalJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("Failed to write internal API response", slog.Any("err", err))
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
