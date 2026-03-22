package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

// StreamEventType identifies the kind of streaming event from a provider.
type StreamEventType string

const (
	StreamEventCreated   StreamEventType = "created"
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
	InputTokens int `json:"input_tokens"`

	// OutputTokens is the number of tokens generated in the response.
	OutputTokens int `json:"output_tokens"`

	// TotalTokens is InputTokens + OutputTokens.
	TotalTokens int `json:"total_tokens"`

	// Cost is the total request cost in USD.
	// For Anthropic, Bedrock, and OpenAI this is locally calculated from
	// provider pricing tables and equals the sum of the breakdown fields below.
	// For OpenRouter this is API-reported by the proxy (already includes cache pricing).
	Cost float64 `json:"cost"`

	// Detailed token breakdown (provider-specific, may be zero).
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`  // Input tokens served from an existing cache entry (all providers).
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"` // Input tokens written to a new cache entry (Anthropic, Bedrock).
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`   // Output tokens consumed by model reasoning (e.g. extended thinking).

	// Granular cost breakdown in USD (zero if provider/model pricing is unknown).
	// Sum of InputCost + CacheReadCost + CacheWriteCost + OutputCost == Cost.
	// Not populated for OpenRouter (API-reported cost is used instead).
	InputCost      float64 `json:"input_cost,omitempty"`       // Cost of non-cached, non-write input tokens.
	CacheReadCost  float64 `json:"cache_read_cost,omitempty"`  // Cost of cache-read tokens.
	CacheWriteCost float64 `json:"cache_write_cost,omitempty"` // Cost of cache-write tokens.
	OutputCost     float64 `json:"output_cost,omitempty"`      // Cost of output tokens.
}

// NewRequestID generates a unique correlation ID for a stream request.
// Uses a URL-safe nanoid with a length of 12 characters.
func NewRequestID() string {
	id, err := gonanoid.New(12)
	if err != nil {
		// Fall back to a fixed placeholder — gonanoid only fails if the
		// alphabet or size is invalid, which cannot happen with defaults.
		return "req_unknown"
	}
	return id
}

// EventStream wraps a buffered StreamEvent channel and stamps every outgoing
// event with the same RequestID, an incrementing sequence number, and a
// timestamp. Providers create one at the top of CreateStream via NewEventStream,
// send all events through Send, and return C() to callers.
type EventStream struct {
	id        string
	seq       uint64
	createdAt time.Time
	ch        chan StreamEvent
	closeOnce sync.Once
}

// NewEventStream creates an EventStream with a freshly generated RequestID,
// records the creation time, emits a StreamEventCreated event, and returns
// a buffered channel of 64 events.
func NewEventStream() *EventStream {
	s := &EventStream{
		id:        NewRequestID(),
		createdAt: time.Now(),
		ch:        make(chan StreamEvent, 64),
	}
	s.Send(StreamEvent{Type: StreamEventCreated})
	return s
}

// Send stamps ev with the stream's RequestID, a monotonically incrementing
// sequence number, and the current timestamp, then sends it on the channel.
// The first event sent has Seq 1.
func (s *EventStream) Send(ev StreamEvent) {
	ev.RequestID = s.id
	ev.Seq = atomic.AddUint64(&s.seq, 1)
	ev.Timestamp = time.Now()
	s.ch <- ev
}

// Error sends a StreamEventError event and is a convenience wrapper around Send.
func (s *EventStream) Error(err error) {
	s.Send(StreamEvent{Type: StreamEventError, Error: err})
}

// ToolCall sends a StreamEventToolCall event for the given tool call.
func (s *EventStream) ToolCall(tc ToolCall) {
	s.Send(StreamEvent{Type: StreamEventToolCall, ToolCall: &tc})
}

// Close closes the underlying channel. Safe to call multiple times.
func (s *EventStream) Close() {
	s.closeOnce.Do(func() { close(s.ch) })
}

// C returns the read-only channel to hand back to the caller of CreateStream.
func (s *EventStream) C() <-chan StreamEvent {
	return s.ch
}

// StreamStart contains metadata about the stream, emitted with StreamEventStart.
type StreamStart struct {
	// ProviderRequestID is the unique identifier returned by the upstream API.
	// Useful for debugging and support tickets. May be empty if the API doesn't provide one.
	ProviderRequestID string `json:"provider_request_id,omitempty"`

	// ModelRequested is what the caller passed in StreamRequest.Model.
	// e.g., "fast", "sonnet", "work/claude/sonnet"
	ModelRequested string `json:"model_requested,omitempty"`

	// ModelResolved is the fully qualified model path after alias resolution.
	// For aggregate: "instance/type/model" e.g., "work/claude/claude-haiku-4-5-20251001"
	// For simple providers: same as what was sent to the API.
	ModelResolved string `json:"model_resolved,omitempty"`

	// ModelProviderID is the model identifier returned by the upstream API in its response.
	// e.g., "claude-haiku-4-5-20251001". May be empty if the API doesn't echo the model back.
	ModelProviderID string `json:"model_provider_id,omitempty"`

	// TimeToFirstToken is the duration from request dispatch until the first response byte.
	// Serialised as a human-readable string (e.g. "412ms") by MarshalJSON.
	TimeToFirstToken time.Duration `json:"-"`
}

// MarshalJSON renders TimeToFirstToken as a human-readable string (e.g. "412ms")
// instead of raw nanoseconds. All other fields use their struct tags directly via
// the type alias trick to avoid infinite recursion.
func (s StreamStart) MarshalJSON() ([]byte, error) {
	// streamStartAlias breaks the MarshalJSON recursion while keeping all tags.
	type streamStartAlias StreamStart
	return json.Marshal(struct {
		streamStartAlias
		TimeToFirstToken string `json:"time_to_first_token,omitempty"`
	}{
		streamStartAlias: streamStartAlias(s),
		TimeToFirstToken: fmt.Sprintf("%dms", s.TimeToFirstToken.Milliseconds()),
	})
}

// StreamEvent is a single event emitted by a provider during streaming.
type StreamEvent struct {
	// Type identifies which kind of event this is.
	Type StreamEventType `json:"type"`

	// RequestID is the library-assigned correlation ID for this stream.
	// Generated once per CreateStream call; identical across all events in a stream.
	RequestID string `json:"request_id,omitempty"`

	// Seq is a monotonically incrementing sequence number within a stream.
	// The first event has Seq 1. Useful for detecting dropped or reordered events.
	Seq uint64 `json:"seq,omitempty"`

	// Timestamp is the wall-clock time at which this event was sent.
	Timestamp time.Time `json:"timestamp,omitempty"`

	// Delta is the incremental text content. Populated for StreamEventDelta.
	Delta string `json:"delta,omitempty"`

	// Reasoning is the incremental reasoning/thinking text. Populated for StreamEventReasoning.
	Reasoning string `json:"reasoning,omitempty"`

	// ToolCall is the tool invocation requested by the model. Populated for StreamEventToolCall.
	ToolCall *ToolCall `json:"tool_call,omitempty"`

	// Error holds the error that terminated the stream. Populated for StreamEventError.
	// Not JSON-serialisable directly; callers that need to serialise should convert to string.
	Error error `json:"-"`

	// Usage holds token counts and cost for the completed request. Populated for StreamEventDone.
	Usage *Usage `json:"usage,omitempty"`

	// Start holds stream metadata. Populated for StreamEventStart.
	Start *StreamStart `json:"start,omitempty"`
}

// StreamRequest configures a provider CreateStream call.
type StreamRequest struct {
	// Model is the model identifier or alias to use, e.g. "fast", "anthropic/claude-sonnet-4-5".
	Model string `json:"model"`

	// Messages is the conversation history to send to the model.
	Messages Messages `json:"messages"`

	// Tools is the set of tools the model may call during the response.
	Tools []ToolDefinition `json:"tools,omitempty"`

	// ToolChoice controls how the model selects tools. Defaults to Auto when Tools are provided.
	ToolChoice ToolChoice `json:"tool_choice,omitempty"`

	// ReasoningEffort controls the depth of reasoning for models that support it (e.g. OpenAI o-series).
	ReasoningEffort ReasoningEffort `json:"reasoning_effort,omitempty"`

	// CacheHint is a top-level prompt caching hint. Behaviour is provider-specific:
	// Anthropic auto mode, Bedrock trailing cachePoint, OpenAI extended retention.
	CacheHint *CacheHint `json:"cache_hint,omitempty"`
}

// Validate checks that the options are valid.
func (o StreamRequest) Validate() error {
	// Validate Model
	if o.Model == "" {
		return errors.New("model is required")
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
