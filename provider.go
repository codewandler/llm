package llm

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// StreamEventType identifies the kind of streaming event from a provider.
type StreamEventType string

const (
	StreamEventStart     StreamEventType = "start"
	StreamEventDelta     StreamEventType = "delta"
	StreamEventReasoning StreamEventType = "reasoning"
	StreamEventToolCall  StreamEventType = "tool_call"
	StreamEventDone      StreamEventType = "done"
	StreamEventError     StreamEventType = "error"
)

// Usage holds token counts and cost from a provider response.
type Usage struct {
	// InputTokens is the total number of input tokens processed, including
	// tokens served from cache (CacheReadTokens) and tokens written to cache
	// (CacheWriteTokens). Callers can use this as the single "how many input
	// tokens did this request consume" figure.
	InputTokens int

	// OutputTokens is the number of tokens generated in the response.
	OutputTokens int

	// TotalTokens is InputTokens + OutputTokens.
	TotalTokens int

	// Cost is the total request cost in USD.
	// For Anthropic, Bedrock, and OpenAI this is locally calculated from
	// provider pricing tables and equals the sum of the breakdown fields below.
	// For OpenRouter this is API-reported by the proxy (already includes cache pricing).
	Cost float64

	// Detailed token breakdown (provider-specific, may be zero).
	CacheReadTokens  int // Input tokens served from an existing cache entry (all providers).
	CacheWriteTokens int // Input tokens written to a new cache entry (Anthropic, Bedrock).
	ReasoningTokens  int // Output tokens consumed by model reasoning (e.g. extended thinking).

	// Granular cost breakdown in USD (zero if provider/model pricing is unknown).
	// Sum of InputCost + CacheReadCost + CacheWriteCost + OutputCost == Cost.
	// Not populated for OpenRouter (API-reported cost is used instead).
	InputCost      float64 // Cost of non-cached, non-write input tokens.
	CacheReadCost  float64 // Cost of cache-read tokens.
	CacheWriteCost float64 // Cost of cache-write tokens.
	OutputCost     float64 // Cost of output tokens.
}

// StreamStart contains metadata about the stream, emitted with StreamEventStart.
type StreamStart struct {
	// RequestedModel is what the caller passed in StreamOptions.Model.
	// e.g., "fast", "sonnet", "work/claude/sonnet"
	RequestedModel string

	// ResolvedModel is the fully qualified model path after resolution.
	// For aggregate: "instance/type/model" e.g., "work/claude/claude-haiku-4-5-20251001"
	// For simple providers: same as what was sent to the API.
	ResolvedModel string

	// ProviderModel is what the underlying API returned in its response.
	// e.g., "claude-haiku-4-5-20251001". May be empty if API doesn't provide it.
	ProviderModel string

	// RequestID is the unique identifier returned by the API for this request.
	// Useful for debugging and support tickets. May be empty.
	RequestID string

	// TimeToFirstToken is the duration from request start until first response data.
	TimeToFirstToken time.Duration
}

// StreamEvent is a single event emitted by a provider during streaming.
type StreamEvent struct {
	Type      StreamEventType
	Delta     string
	Reasoning string
	ToolCall  *ToolCall
	Error     error
	Usage     *Usage
	Start     *StreamStart // Populated for StreamEventStart
}

// StreamOptions configures a provider CreateStream call.
type StreamOptions struct {
	Model                string
	Messages             Messages
	Tools                []ToolDefinition
	ToolChoice           ToolChoice      // nil defaults to Auto when Tools provided
	ReasoningEffort      ReasoningEffort // Controls reasoning for reasoning models (OpenAI)
	CacheHint            *CacheHint      // Top-level prompt caching hint (Anthropic auto mode, Bedrock trailing cachePoint, OpenAI extended retention)
}

// Validate checks that the options are valid.
func (o StreamOptions) Validate() error {
	// Validate Model
	if o.Model == "" {
		return errors.New("Model is required")
	}

	// Validate ReasoningEffort
	if !o.ReasoningEffort.Valid() {
		return fmt.Errorf("invalid ReasoningEffort %q", o.ReasoningEffort)
	}

	// Validate Tools
	for i, tool := range o.Tools {
		if err := tool.Validate(); err != nil {
			return fmt.Errorf("tools[%d]: %w", i, err)
		}
	}

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

type Streamer interface {
	CreateStream(ctx context.Context, opts StreamOptions) (<-chan StreamEvent, error)
}

// Provider is the interface each LLM backend must implement.
type Provider interface {
	Name() string
	Models() []Model
	Streamer
}

// ModelFetcher is an optional interface providers can implement to list
// models dynamically from their API instead of returning a static list.
type ModelFetcher interface {
	FetchModels(ctx context.Context) ([]Model, error)
}
