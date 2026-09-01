package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

// BotAPI is the dashboard's client for the bot's internal API. It is the only
// route to live Discord state; the dashboard holds no bot token and never calls
// Discord directly outside the OAuth handshake.
//
// Every method is best-effort by design. The bot is redeployed automatically on
// every push to main, so it being briefly unreachable is routine, not
// exceptional. Callers render IDs and a degradation banner rather than a 500 --
// a dashboard that dies whenever the bot restarts is not useful.
type BotAPI struct {
	baseURL string
	secret  string
	client  *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry
	ttl   time.Duration
}

type cacheEntry struct {
	value   any
	expires time.Time
}

type BotGuild struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon,omitempty"`
	OwnerID     string `json:"owner_id,omitempty"`
	MemberCount int    `json:"member_count"`
}

type BotRole struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Color    int    `json:"color"`
	Position int    `json:"position"`
	Managed  bool   `json:"managed"`
}

type BotChannel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     int    `json:"type"`
	ParentID string `json:"parent_id,omitempty"`
	Position int    `json:"position"`
}

type BotUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar,omitempty"`
}

// Discord channel type numbers the dashboard cares about.
const (
	ChannelTypeText         = 0
	ChannelTypeVoice        = 2
	ChannelTypeCategory     = 4
	ChannelTypeAnnouncement = 5
)

// IsText reports whether a channel can receive the bot's log and notification
// embeds.
func (c BotChannel) IsText() bool {
	return c.Type == ChannelTypeText || c.Type == ChannelTypeAnnouncement
}

func NewBotAPI(baseURL, secret string, ttl time.Duration) *BotAPI {
	return &BotAPI{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		client:  &http.Client{Timeout: 5 * time.Second},
		cache:   make(map[string]cacheEntry),
		ttl:     ttl,
	}
}

// Configured reports whether a bot API is available at all. When it is not, the
// dashboard still serves every database-backed page.
func (b *BotAPI) Configured() bool {
	return b != nil && b.baseURL != "" && b.secret != ""
}

func (b *BotAPI) Guilds(ctx context.Context) ([]BotGuild, error) {
	return cached(b, ctx, "guilds", func() ([]BotGuild, error) {
		var out []BotGuild
		err := b.get(ctx, "/internal/guilds", &out)
		return out, err
	})
}

func (b *BotAPI) Guild(ctx context.Context, guildID snowflake.ID) (BotGuild, error) {
	return cached(b, ctx, "guild:"+guildID.String(), func() (BotGuild, error) {
		var out BotGuild
		err := b.get(ctx, "/internal/guilds/"+guildID.String(), &out)
		return out, err
	})
}

func (b *BotAPI) Roles(ctx context.Context, guildID snowflake.ID) ([]BotRole, error) {
	return cached(b, ctx, "roles:"+guildID.String(), func() ([]BotRole, error) {
		var out []BotRole
		err := b.get(ctx, "/internal/guilds/"+guildID.String()+"/roles", &out)
		return out, err
	})
}

func (b *BotAPI) Channels(ctx context.Context, guildID snowflake.ID) ([]BotChannel, error) {
	return cached(b, ctx, "channels:"+guildID.String(), func() ([]BotChannel, error) {
		var out []BotChannel
		err := b.get(ctx, "/internal/guilds/"+guildID.String()+"/channels", &out)
		return out, err
	})
}

// ResolveUsers turns snowflakes from modlogs and join/leave rows into names.
//
// It is a batch call because a single page of 50 modlog rows references up to
// 100 distinct users, and one request each would rate-limit the bot on first
// paint. IDs that cannot be resolved are simply absent from the map: a member
// who has since left is the normal case, not an error.
func (b *BotAPI) ResolveUsers(ctx context.Context, guildID snowflake.ID, ids []string) map[string]BotUser {
	out := make(map[string]BotUser)
	if !b.Configured() || len(ids) == 0 {
		return out
	}

	body, err := json.Marshal(map[string]any{
		"guild_id": guildID.String(),
		"user_ids": ids,
	})
	if err != nil {
		return out
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.baseURL+"/internal/users/resolve", bytes.NewReader(body))
	if err != nil {
		return out
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", b.secret)

	resp, err := b.client.Do(req)
	if err != nil {
		return out
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out
	}
	// A decode failure leaves out empty, which renders as raw IDs. That is the
	// intended degradation, so the error is dropped rather than propagated.
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func (b *BotAPI) get(ctx context.Context, path string, into any) error {
	if !b.Configured() {
		return ErrBotAPIUnavailable
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Internal-Token", b.secret)

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBotAPIUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrBotAPIUnavailable, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// cached memoises a fetch for the client's TTL. It is a free function rather
// than a method because Go does not allow type parameters on methods.
//
// Only successes are cached: a failure while the bot is restarting must not
// pin an error in place for the whole TTL.
func cached[T any](b *BotAPI, _ context.Context, key string, fetch func() (T, error)) (T, error) {
	b.mu.Lock()
	if entry, ok := b.cache[key]; ok && time.Now().Before(entry.expires) {
		if value, ok := entry.value.(T); ok {
			b.mu.Unlock()
			return value, nil
		}
	}
	b.mu.Unlock()

	value, err := fetch()
	if err != nil {
		return value, err
	}

	b.mu.Lock()
	b.cache[key] = cacheEntry{value: value, expires: time.Now().Add(b.ttl)}
	b.mu.Unlock()
	return value, nil
}

// ChannelLookup indexes channels by ID string for template rendering.
func ChannelLookup(channels []BotChannel) map[string]BotChannel {
	out := make(map[string]BotChannel, len(channels))
	for _, c := range channels {
		out[c.ID] = c
	}
	return out
}

// RoleLookup indexes roles by ID string for template rendering.
func RoleLookup(roles []BotRole) map[string]BotRole {
	out := make(map[string]BotRole, len(roles))
	for _, r := range roles {
		out[r.ID] = r
	}
	return out
}
