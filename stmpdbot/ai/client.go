// Package ai is the AI persona feature's core: a chat-completions client, the
// tools it can call (catalogue, tour dates, message sampling, memory), and
// the agent loop that ties them together. It is imported only by cmd/agent
// -- the standalone service that is the feature's only home for the LLM API
// key and the tool-calling loop, kept out of the bot's own process so a
// prompt/tool/memory change redeploys independently of the bot.
//
// Removing the feature entirely: delete this package, cmd/agent, the
// stmpdbot/listeners/ai.go trigger, LLMConfig/AgentConfig from
// stmpdbot/config.go, the agent Docker/compose service, and the
// b.SetupLLM()/AIListener(b) call sites in main.go.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to an OpenAI-compatible chat completions endpoint, such as the
// cliproxy.milind.dev proxy this feature was built against.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
	maxTokens  int
}

func NewClient(baseURL, apiKey, model string, maxTokens int) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		model:      model,
		maxTokens:  maxTokens,
	}
}

// Message is one turn in a chat-completions conversation. Content is empty on
// an assistant message that only carries ToolCalls; ToolCallID is set only on
// a role "tool" message answering one of those calls.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool describes one function the model may call, in the standard
// OpenAI "tools" shape.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatRequest struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	Tools     []Tool    `json:"tools,omitempty"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ChatCompletion sends one request and returns the assistant's reply message,
// which may carry ToolCalls instead of (or alongside) Content.
func (c *Client) ChatCompletion(ctx context.Context, messages []Message, tools []Tool) (Message, error) {
	body, err := json.Marshal(chatRequest{
		Model:     c.model,
		Messages:  messages,
		Tools:     tools,
		MaxTokens: c.maxTokens,
	})
	if err != nil {
		return Message{}, fmt.Errorf("ai: failed to encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, fmt.Errorf("ai: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("ai: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, fmt.Errorf("ai: failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Message{}, fmt.Errorf("ai: chat completion returned status %d: %s", resp.StatusCode, string(data))
	}

	var out chatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return Message{}, fmt.Errorf("ai: failed to decode response: %w", err)
	}
	if out.Error != nil {
		return Message{}, fmt.Errorf("ai: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return Message{}, errors.New("ai: response carried no choices")
	}
	return out.Choices[0].Message, nil
}
