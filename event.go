package llm

import (
	"encoding/json"
	"time"

	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
)

// EventType identifies the kind of streaming event from a provider.
type EventType string

const (
	StreamEventCreated         EventType = "created"
	StreamEventClosed          EventType = "closed"
	StreamEventModelResolved   EventType = "model_resolved"
	StreamEventProviderFailover EventType = "provider_failover"
	StreamEventStarted         EventType = "started"
	StreamEventUsageUpdated    EventType = "usage"
	StreamEventDelta           EventType = "delta"
	StreamEventToolCall        EventType = "tool_call"
	StreamEventContentPart     EventType = "content_part"
	StreamEventCompleted       EventType = "completed"
	StreamEventError           EventType = "error"
	StreamEventDebug           EventType = "debug"
	StreamEventRequest         EventType = "request"
)

type (
	Event interface {
		Type() EventType
	}

	Stream <-chan Envelope

	Publisher interface {
		Publish(payload Event)

		Started(started StreamStartedEvent)
		ModelResolved(resolver, name, resolved string)
		Failover(from, to string, err error)
		Delta(d *DeltaEvent)
		ToolCall(tc tool.Call)
		ContentBlock(evt ContentPartEvent)

		Usage(usage Usage)
		Completed(completed CompletedEvent)

		Error(err error)
		Debug(msg string, data any)

		Close()
	}
)

type EventMeta struct {
	RequestID string            `json:"request_id,omitempty"`
	Seq       uint64            `json:"seq,omitempty"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
	After     time.Duration     `json:"after,omitempty"`
	TraceID   string            `json:"trace_id,omitempty"`
	Model     string            `json:"model,omitempty"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}

type Envelope struct {
	Type EventType `json:"type"`
	Meta EventMeta `json:"meta"`
	Data any       `json:"data,omitempty"`
}

type (
	StreamCreatedEvent struct{}

	StreamStartedEvent struct {
		RequestID string `json:"request_id,omitempty"`

		// Model is the model identifier returned by the upstream API in its response.
		// e.g., "claude-haiku-4-5-20251001". May be empty if the API doesn't echo the model back.
		Model string `json:"model,omitempty"`

		// Extra holds provider-specific data such as rate-limit headers.
		Extra map[string]any `json:"extra,omitempty"`
	}

	StreamClosedEvent struct{}

	DebugEvent struct {
		Message string `json:"message,omitempty"`
		Data    any    `json:"data,omitempty"`
	}

	// ModelResolvedEvent is emitted whenever a requested model name is
	// translated to a different resolved name: by router alias lookup,
	// by OpenRouter's default-model normalization, or by a provider
	// revealing the actual model chosen for the request.
	ModelResolvedEvent struct {
		Resolver string `json:"resolver"`
		Name     string `json:"name,omitempty"`
		Resolved string `json:"resolved,omitempty"`
	}

	// ProviderFailoverEvent is emitted by the router each time a provider
	// attempt fails with a retriable error and the next provider is tried.
	// It is NOT emitted when the last provider in the list fails (that is
	// terminal, surfaced as an error return, not an event).
	ProviderFailoverEvent struct {
		Provider         string `json:"provider"`          // failed provider
		FailoverProvider string `json:"failover_provider"` // next provider being tried
		Error            error  `json:"-"`
	}

	ToolCallEvent struct {
		ToolCall tool.Call `json:"tool_call"`
	}

	UsageUpdatedEvent struct {
		Usage Usage `json:"usage"`
	}

	CompletedEvent struct {
		StopReason StopReason `json:"stop_reason"`
	}

	ErrorEvent struct {
		Error error `json:"error"`
	}

	// ContentPartEvent is emitted once per content block when the provider signals
	// block completion (content_block_stop). Index is the position of this block in
	// the model's original output array — required to preserve the exact interleaving
	// order of text and thinking blocks when re-serializing the assistant message.
	ContentPartEvent struct {
		Part  msg.Part `json:"part"`
		Index int      `json:"index"`
	}

	ProviderRequest struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	}

	// RequestEvent is emitted by a provider once per stream, carrying the
	// final resolved request parameters (after alias resolution, default
	// application, thinking-budget mapping, etc.). Consumers can use this
	// for observability / debugging without inspecting the raw HTTP body.
	RequestEvent struct {
		OriginalRequest Request         `json:"original_request"`
		ProviderRequest ProviderRequest `json:"provider_request"`
	}
)

func (e DebugEvent) Type() EventType          { return StreamEventDebug }
func (e RequestEvent) Type() EventType        { return StreamEventRequest }
func (e ModelResolvedEvent) Type() EventType  { return StreamEventModelResolved }
func (e ProviderFailoverEvent) Type() EventType { return StreamEventProviderFailover }
func (e StreamCreatedEvent) Type() EventType  { return StreamEventCreated }
func (e StreamClosedEvent) Type() EventType   { return StreamEventClosed }
func (e ToolCallEvent) Type() EventType       { return StreamEventToolCall }
func (e StreamStartedEvent) Type() EventType  { return StreamEventStarted }
func (e CompletedEvent) Type() EventType      { return StreamEventCompleted }
func (e UsageUpdatedEvent) Type() EventType   { return StreamEventUsageUpdated }
func (e ErrorEvent) Type() EventType          { return StreamEventError }
func (e ContentPartEvent) Type() EventType    { return StreamEventContentPart }
