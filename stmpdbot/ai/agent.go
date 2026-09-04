package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

// maxToolRounds bounds how many tool round-trips one triggered response can
// spend before it is forced to answer in text. Each round is one completion
// call, so this is also a hard cap on the cost of a single Discord ping.
const maxToolRounds = 3

// Respond runs the tool-calling loop and returns the assistant's final text
// reply. history must already open with a system message -- normally
// SystemPrompt() -- followed by the reconstructed conversation and the
// triggering message.
func (c *Client) Respond(ctx context.Context, queries *db.Queries, guildID int64, history []Message) (string, error) {
	messages := append([]Message(nil), history...)

	for round := 0; round <= maxToolRounds; round++ {
		tools := Tools()
		if round == maxToolRounds {
			// Force a text-only answer: no tools offered, so there is
			// nothing left for the model to call.
			tools = nil
		}

		reply, err := c.ChatCompletion(ctx, messages, tools)
		if err != nil {
			return "", err
		}

		if len(reply.ToolCalls) == 0 {
			return reply.Content, nil
		}

		messages = append(messages, reply)
		for _, call := range reply.ToolCalls {
			result, err := Dispatch(ctx, queries, guildID, call.Function.Name, call.Function.Arguments)
			if err != nil {
				slog.Warn("ai: tool call failed",
					slog.String("tool", call.Function.Name), slog.Any("err", err))
				result = fmt.Sprintf(`{"error": %q}`, err.Error())
			}
			messages = append(messages, Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    result,
			})
		}
	}

	return "", errors.New("ai: model kept calling tools past the round limit")
}
