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
