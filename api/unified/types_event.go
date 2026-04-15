package unified

import (
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/usage"
)

// StreamEventType identifies the canonical unified stream event kind.
type StreamEventType string

const (
	StreamEventStarted   StreamEventType = "started"
	StreamEventDelta     StreamEventType = "delta"
	StreamEventToolCall  StreamEventType = "tool_call"
	StreamEventContent   StreamEventType = "content"
	StreamEventUsage     StreamEventType = "usage"
	StreamEventCompleted StreamEventType = "completed"
	StreamEventError     StreamEventType = "error"
	StreamEventUnknown   StreamEventType = "unknown"
)

// StreamEvent is the canonical unified stream envelope used by api/unified
// converters and publisher bridge.
type StreamEvent struct {
	Type      StreamEventType
	Started   *Started
	Delta     *Delta
	ToolCall  *ToolCall
	Content   *ContentPart
	Usage     *Usage
	Completed *Completed
	Error     *StreamError
	Extras    EventExtras
}

// Started carries stream-start metadata.
type Started struct {
	RequestID string
	Model     string
	Provider  string
	Extra     map[string]any
}

// Delta is a canonical incremental content fragment.
type Delta struct {
	Kind llm.DeltaKind

	// Index is output block index when available.
	Index *uint32

	Text     string
	Thinking string

	ToolID   string
	ToolName string
	ToolArgs string
}

// ContentPart is a canonical completed content block.
type ContentPart struct {
	Part  msg.Part
	Index int
}

// Usage is canonical token/cost usage metadata.
type Usage struct {
	Provider  string
	Model     string
	RequestID string

	Tokens usage.TokenItems
	Cost   usage.Cost

	RecordedAt time.Time
	Extras     map[string]any
}

// Completed is canonical stream completion metadata.
type Completed struct {
	StopReason llm.StopReason
}

// StreamError is canonical stream error metadata.
type StreamError struct {
	Err error
}

// EventExtras holds forward-compatible/raw event data.
type EventExtras struct {
	RawEventName string
	RawEvent     map[string]any
	Provider     map[string]any
}
