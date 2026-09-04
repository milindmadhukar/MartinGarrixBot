package listeners

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/stmpdbot/ai"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// AIListener answers a mention or a reply to one of the bot's own messages
// with an LLM-generated response, grounded on the song catalogue and this
// server's own message history via stmpdbot/ai's tools.
//
// This is the experimental AI persona feature's only entry point into the
// rest of the bot: deleting this file, the stmpdbot/ai package, LLMConfig
// and the b.SetupLLM()/AIListener(b) lines in main.go removes it completely.
func AIListener(b *stmpdbot.STMPDBot) bot.EventListener {
	cooldowns := newCooldowns()

	return bot.NewListenerFunc(func(e *events.MessageCreate) {
		if b.AIClient == nil {
			return
		}
		if e.Message.Author.Bot || e.Message.Author.System || e.GuildID == nil {
			return
		}
		if e.Message.MentionEveryone {
			return
		}

		isReply, ok := triggered(b, e.Message)
		if !ok {
			return
		}

		// A reply to the bot's own message is a conversation someone is
		// already in and actively waiting on -- cooldown-blocking it silently
		// dropped a message a user was staring at, which is worse than the
		// spam the cooldown exists to prevent. Only a fresh, unsolicited
		// mention is rate-limited.
		if !isReply {
			cooldown := time.Duration(b.Cfg.LLM.CooldownSeconds) * time.Second
			if cooldown <= 0 {
				cooldown = 15 * time.Second
			}
			if !cooldowns.allow(e.Message.Author.ID, cooldown) {
				slog.Debug("ai: cooldown active, skipping mention",
					slog.String("user_id", e.Message.Author.ID.String()))
				return
			}
		}

		go respond(b, e, isReply)
	})
}

// triggered reports whether message either @mentions the bot or replies to
// one of the bot's own messages, and which of the two it was.
func triggered(b *stmpdbot.STMPDBot, message discord.Message) (isReply, ok bool) {
	selfID := b.Client.ID()

	if message.ReferencedMessage != nil && message.ReferencedMessage.Author.ID == selfID {
		return true, true
	}

	for _, user := range message.Mentions {
		if user.ID == selfID {
			return false, true
		}
	}

	return false, false
}

// respond runs off the gateway goroutine: an LLM round-trip (plus its own
// tool-calling round-trips) can easily take longer than disgo's event
// dispatch should be blocked for.
func respond(b *stmpdbot.STMPDBot, e *events.MessageCreate, isReply bool) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := b.Client.Rest.SendTyping(e.ChannelID); err != nil {
		slog.Debug("ai: failed to send typing indicator", slog.Any("err", err))
	}

	history := buildHistory(ctx, b, e)

	content, err := b.AIClient.Respond(ctx, b.Queries, int64(*e.GuildID), history)
	if err != nil {
		slog.Error("ai: failed to generate a response",
			slog.String("user_id", e.Message.Author.ID.String()),
			slog.Bool("is_reply", isReply), slog.Any("err", err))
		return
	}
	if content == "" {
		slog.Warn("ai: model returned an empty reply",
			slog.String("user_id", e.Message.Author.ID.String()), slog.Bool("is_reply", isReply))
		return
	}

	utils.ReplyToMessage(b.Client, e.ChannelID, e.Message, content)
	slog.Info("ai: replied",
		slog.String("user_id", e.Message.Author.ID.String()),
		slog.Bool("is_reply", isReply), slog.Duration("took", time.Since(start)))
}

// buildHistory walks the Discord reply chain backwards from the triggering
// message, turning it into a conversation ai.Client.Respond can answer.
// Stateless on purpose: no conversation is ever stored -- it is
// reconstructed from Discord's own reply references every time the bot is
// pinged.
func buildHistory(ctx context.Context, b *stmpdbot.STMPDBot, e *events.MessageCreate) []ai.Message {
	maxHops := b.Cfg.LLM.MaxContextMessages
	if maxHops <= 0 {
		maxHops = 6
	}
	selfID := b.Client.ID()

	type turn struct {
		role    string
		content string
	}
	var chain []turn

	current := e.Message
	for i := 0; i < maxHops; i++ {
		ref := current.MessageReference
		if ref == nil || ref.MessageID == nil {
			break
		}

		refMsg := current.ReferencedMessage
		if refMsg == nil {
			fetched, err := b.Client.Rest.GetMessage(e.ChannelID, *ref.MessageID, rest.WithCtx(ctx))
			if err != nil {
				break
			}
			refMsg = fetched
		}
		if refMsg.Content == "" {
			break
		}

		role := "user"
		if refMsg.Author.ID == selfID {
			role = "assistant"
		}
		content := resolveMentions(b, *e.GuildID, refMsg.Content, refMsg.Mentions)
		chain = append(chain, turn{role: role, content: content})
		current = *refMsg
	}

	messages := make([]ai.Message, 0, len(chain)+2)
	messages = append(messages, ai.Message{Role: "system", Content: ai.SystemPrompt()})
	for i := len(chain) - 1; i >= 0; i-- {
		messages = append(messages, ai.Message{Role: chain[i].role, Content: chain[i].content})
	}
	messages = append(messages, ai.Message{
		Role:    "user",
		Content: resolveMentions(b, *e.GuildID, e.Message.Content, e.Message.Mentions),
	})
	return messages
}

var mentionPattern = regexp.MustCompile(`<@!?(\d+)>`)

// resolveMentions replaces Discord's raw <@id> mention syntax with a readable
// display name, so the model sees "what do you think about Sourav?" instead
// of an opaque snowflake it has no way to identify. Prefers the guild
// nickname (from cache) over the global display name over the bare username,
// same precedence Discord's own client uses to show a mention.
func resolveMentions(b *stmpdbot.STMPDBot, guildID snowflake.ID, content string, mentions []discord.User) string {
	if len(mentions) == 0 || !strings.Contains(content, "<@") {
		return content
	}

	names := make(map[snowflake.ID]string, len(mentions))
	for _, u := range mentions {
		names[u.ID] = displayName(b, guildID, u)
	}

	return mentionPattern.ReplaceAllStringFunc(content, func(token string) string {
		id, err := snowflake.Parse(mentionPattern.FindStringSubmatch(token)[1])
		if err != nil {
			return token
		}
		if name, ok := names[id]; ok {
			return "@" + name
		}
		return token
	})
}

func displayName(b *stmpdbot.STMPDBot, guildID snowflake.ID, u discord.User) string {
	if member, ok := b.Client.Caches.Member(guildID, u.ID); ok && member.Nick != nil && *member.Nick != "" {
		return *member.Nick
	}
	if u.GlobalName != nil && *u.GlobalName != "" {
		return *u.GlobalName
	}
	return u.Username
}

// cooldowns is a simple per-user in-memory rate limit, kept out of the
// database on purpose: this feature is stateless and meant to be removed
// entirely once the experiment is over.
type cooldowns struct {
	mu   sync.Mutex
	last map[snowflake.ID]time.Time
}

func newCooldowns() *cooldowns {
	return &cooldowns{last: make(map[snowflake.ID]time.Time)}
}

func (c *cooldowns) allow(userID snowflake.ID, cooldown time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if last, ok := c.last[userID]; ok && now.Sub(last) < cooldown {
		return false
	}
	c.last[userID] = now
	return true
}
