package llm

import (
	"context"
)

// ProviderConfig holds authentication and configuration for a provider.
type ProviderConfig struct {
	APIKey string
	OAuth  *OAuthConfig
}

// OAuthConfig holds OAuth tokens and expiry information.
type OAuthConfig struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Expires int64  `json:"expires"` // Unix timestamp in milliseconds
}

// GetAccessToken returns the current access token (OAuth or API key).
func (c *ProviderConfig) GetAccessToken() string {
	if c.OAuth != nil && c.OAuth.Access != "" {
		return c.OAuth.Access
	}
	return c.APIKey
}

// StreamEventType identifies the kind of streaming event from a provider.
type StreamEventType string

const (
	StreamEventDelta     StreamEventType = "delta"
	StreamEventReasoning StreamEventType = "reasoning"
	StreamEventToolCall  StreamEventType = "tool_call"
	StreamEventDone      StreamEventType = "done"
	StreamEventError     StreamEventType = "error"
)

// Usage holds token counts and cost from a provider response.
type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Cost         float64
}

// StreamEvent is a single event emitted by a provider during streaming.
type StreamEvent struct {
	Type      StreamEventType
	Delta     string
	Reasoning string
	ToolCall  *ToolCall
	Error     error
	Usage     *Usage
}

// SendOptions configures a provider SendMessage call.
type SendOptions struct {
	Model    string
	Messages []Message
	Tools    []ToolDefinition
}

// Provider is the interface each LLM backend must implement.
type Provider interface {
	Name() string
	Models() []Model
	SendMessage(ctx context.Context, opts SendOptions) (<-chan StreamEvent, error)
}

// ModelFetcher is an optional interface providers can implement to list
// models dynamically from their API instead of returning a static list.
type ModelFetcher interface {
	FetchModels(ctx context.Context) ([]Model, error)
}
