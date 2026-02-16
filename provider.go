package llm

import (
	"context"
	"errors"
	"fmt"
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

// StreamOptions configures a provider CreateStream call.
type StreamOptions struct {
	Model           string
	Messages        Messages
	Tools           []ToolDefinition
	ToolChoice      ToolChoice      // nil defaults to Auto when Tools provided
	ReasoningEffort ReasoningEffort // Controls reasoning for reasoning models (OpenAI)
}

// Validate checks that the options are valid.
func (o StreamOptions) Validate() error {
	// Validate messages
	for i, msg := range o.Messages {
		if err := msg.Validate(); err != nil {
			return fmt.Errorf("messages[%d]: %w", i, err)
		}
	}

	// Validate ToolChoice
	if o.ToolChoice != nil && len(o.Tools) == 0 {
		return errors.New("ToolChoice set but no Tools provided")
	}

	if tc, ok := o.ToolChoice.(ToolChoiceTool); ok {
		if tc.Name == "" {
			return errors.New("ToolChoiceTool.Name is required")
		}
		found := false
		for _, t := range o.Tools {
			if t.Name == tc.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("ToolChoiceTool references unknown tool %q", tc.Name)
		}
	}

	return nil
}

// Provider is the interface each LLM backend must implement.
type Provider interface {
	Name() string
	Models() []Model
	CreateStream(ctx context.Context, opts StreamOptions) (<-chan StreamEvent, error)
}

// ModelFetcher is an optional interface providers can implement to list
// models dynamically from their API instead of returning a static list.
type ModelFetcher interface {
	FetchModels(ctx context.Context) ([]Model, error)
}
