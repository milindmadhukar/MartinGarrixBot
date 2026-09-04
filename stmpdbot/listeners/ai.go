package listeners

import (
	"context"
	"log/slog"
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
		if !triggered(b, e.Message) {
			return
		}

		cooldown := time.Duration(b.Cfg.LLM.CooldownSeconds) * time.Second
		if cooldown <= 0 {
			cooldown = 15 * time.Second
		}
		if !cooldowns.allow(e.Message.Author.ID, cooldown) {
			return
		}

		go respond(b, e)
	})
}

// triggered reports whether message either @mentions the bot or replies to
// one of the bot's own messages.
func triggered(b *stmpdbot.STMPDBot, message discord.Message) bool {
	selfID := b.Client.ID()

	for _, user := range message.Mentions {
		if user.ID == selfID {
			return true
		}
	}

	return message.ReferencedMessage != nil && message.ReferencedMessage.Author.ID == selfID
}

// respond runs off the gateway goroutine: an LLM round-trip (plus its own
// tool-calling round-trips) can easily take longer than disgo's event
// dispatch should be blocked for.
func respond(b *stmpdbot.STMPDBot, e *events.MessageCreate) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := b.Client.Rest.SendTyping(e.ChannelID); err != nil {
		slog.Debug("ai: failed to send typing indicator", slog.Any("err", err))
	}

	history := buildHistory(ctx, b, e)

	content, err := b.AIClient.Respond(ctx, b.Queries, int64(*e.GuildID), history)
	if err != nil {
		slog.Error("ai: failed to generate a response", slog.Any("err", err))
		return
	}
	if content == "" {
		return
	}

	utils.ReplyToMessage(b.Client, e.ChannelID, e.Message, content)
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
		chain = append(chain, turn{role: role, content: refMsg.Content})
		current = *refMsg
	}

	messages := make([]ai.Message, 0, len(chain)+2)
	messages = append(messages, ai.Message{Role: "system", Content: ai.SystemPrompt()})
	for i := len(chain) - 1; i >= 0; i-- {
		messages = append(messages, ai.Message{Role: chain[i].role, Content: chain[i].content})
	}
	messages = append(messages, ai.Message{Role: "user", Content: e.Message.Content})
	return messages
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
