package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AgentClient talks to cmd/agent, the standalone process that holds the LLM
// API key and runs the AI persona's tool-calling loop. The bot never talks to
// an LLM provider directly -- everything about how a reply is generated lives
// on the other side of this HTTP call, which is what lets that side redeploy
// without touching the bot at all.
type AgentClient struct {
	httpClient *http.Client
	baseURL    string
	secret     string
}

func NewAgentClient(baseURL, secret string) *AgentClient {
	return &AgentClient{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		baseURL:    strings.TrimRight(baseURL, "/"),
		secret:     secret,
	}
}

// AgentMessage is one turn of the conversation sent to the agent service. It
// only ever carries role "user" or "assistant" -- tool calls and tool
// results are an implementation detail of the agent service's own loop and
// never cross this boundary.
type AgentMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type agentRespondRequest struct {
	GuildID  int64          `json:"guild_id"`
	UserID   int64          `json:"user_id"`
	Messages []AgentMessage `json:"messages"`
}

type agentRespondResponse struct {
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Respond asks the agent service for a reply to the given conversation,
// scoped to guildID/userID for memory and per-user tools. messages holds
// only the reconstructed conversation -- no system prompt: the agent service
// owns its own identity, persona and memory, and assembles all of that
// itself.
func (c *AgentClient) Respond(ctx context.Context, guildID, userID int64, messages []AgentMessage) (string, error) {
	body, err := json.Marshal(agentRespondRequest{GuildID: guildID, UserID: userID, Messages: messages})
	if err != nil {
		return "", fmt.Errorf("agentclient: failed to encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/respond", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("agentclient: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", c.secret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("agentclient: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("agentclient: failed to read response: %w", err)
	}

	var out agentRespondResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("agentclient: failed to decode response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		if out.Error != "" {
			return "", fmt.Errorf("agentclient: %s", out.Error)
		}
		return "", fmt.Errorf("agentclient: request failed with status %d", resp.StatusCode)
	}
	return out.Content, nil
}
