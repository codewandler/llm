package llm

import (
	"time"

	"github.com/codewandler/llm/tool"
)

// EventType identifies the kind of streaming event from a provider.
type EventType string

const (
	StreamEventCreated      EventType = "created"
	StreamEventClosed       EventType = "closed"
	StreamEventRouted       EventType = "routed"
	StreamEventStarted      EventType = "started"
	StreamEventUsageUpdated EventType = "usage"
	StreamEventDelta        EventType = "delta"
	StreamEventToolCall     EventType = "tool_call"
	StreamEventContentBlock EventType = "content_block"
	StreamEventCompleted    EventType = "completed"
	StreamEventError        EventType = "error"
	StreamEventDebug        EventType = "debug"
)

type (
	Event interface {
		Type() EventType
	}

	Stream <-chan Envelope

	Publisher interface {
		Publish(payload Event)

		Started(started StreamStartedEvent)
		Routed(routed RouteInfo)
		Delta(d *DeltaEvent)
		ToolCall(tc tool.Call)
		ContentBlock(evt ContentBlockEvent)

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

// ContentBlockKind identifies the kind of a completed content block.
type ContentBlockKind string

const (
	// ContentBlockKindText is a text block.
	ContentBlockKindText ContentBlockKind = "text"
	// ContentBlockKindThinking is an extended-thinking block.
	// It carries a Signature that must be re-sent verbatim to the API.
	ContentBlockKindThinking ContentBlockKind = "thinking"
)

// ContentBlock is a completed content block from the model.
//
// For text blocks: Text is populated, Signature is empty.
// For thinking blocks: Text holds the thinking content and Signature holds the
// cryptographic verification token. The Signature must be preserved exactly and
// re-submitted verbatim in the next assistant message content array when the
// response includes tool calls (tool-use loop continuity).
type ContentBlock struct {
	Kind      ContentBlockKind `json:"kind"`
	Text      string           `json:"text,omitempty"`
	Signature string           `json:"signature,omitempty"` // only for thinking blocks
}

// ContentBlockEvent is emitted once per content block when the provider signals
// block completion (content_block_stop). Index is the position of this block in
// the model's original output array — required to preserve the exact interleaving
// order of text and thinking blocks when re-serializing the assistant message.
type ContentBlockEvent struct {
	ContentBlock
	Index int `json:"index"`
}

func (e ContentBlockEvent) Type() EventType { return StreamEventContentBlock }

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

	RouteInfo struct {
		Provider       string  `json:"provider"`
		ModelRequested string  `json:"model_requested,omitempty"`
		ModelResolved  string  `json:"model_resolved,omitempty"`
		Errors         []error `json:"-"`
	}

	RouteInfoEvent struct {
		RouteInfo RouteInfo `json:"route_info"`
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
)

func (e DebugEvent) Type() EventType         { return StreamEventDebug }
func (e RouteInfoEvent) Type() EventType     { return StreamEventRouted }
func (e StreamCreatedEvent) Type() EventType { return StreamEventCreated }
func (e StreamClosedEvent) Type() EventType  { return StreamEventClosed }
func (e ToolCallEvent) Type() EventType      { return StreamEventToolCall }
func (e StreamStartedEvent) Type() EventType { return StreamEventStarted }
func (e CompletedEvent) Type() EventType     { return StreamEventCompleted }
func (e UsageUpdatedEvent) Type() EventType  { return StreamEventUsageUpdated }
func (e ErrorEvent) Type() EventType         { return StreamEventError }
