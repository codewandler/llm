package unified

import (
	"encoding/json"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/usage"
)

// StreamEventType identifies the canonical unified stream event kind.
type StreamEventType string

const (
	StreamEventStarted      StreamEventType = "started"
	StreamEventDelta        StreamEventType = "delta"
	StreamEventToolCall     StreamEventType = "tool_call"
	StreamEventContent      StreamEventType = "content"
	StreamEventUsage        StreamEventType = "usage"
	StreamEventCompleted    StreamEventType = "completed"
	StreamEventError        StreamEventType = "error"
	StreamEventUnknown      StreamEventType = "unknown"
	StreamEventLifecycle    StreamEventType = "lifecycle"
	StreamEventContentDelta StreamEventType = "content_delta"
	StreamEventToolDelta    StreamEventType = "tool_delta"
	StreamEventAnnotation   StreamEventType = "annotation"
)

// StreamEvent is the canonical unified stream envelope used by api/unified
// stream bridges and forwarders. Type identifies the primary semantic payload.
// Compatibility projections to the current llm layer may also populate legacy
// fields such as Delta, ToolCall, and Content.
type StreamEvent struct {
	Type           StreamEventType
	Started        *Started
	Delta          *Delta
	ToolCall       *ToolCall
	Content        *ContentPart
	Usage          *Usage
	Completed      *Completed
	Error          *StreamError
	Lifecycle      *Lifecycle
	ContentDelta   *ContentDelta
	StreamContent  *StreamContent
	ToolDelta      *ToolDelta
	StreamToolCall *StreamToolCall
	Annotation     *Annotation
	Extras         EventExtras
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

type LifecycleScope string

const (
	LifecycleScopeResponse LifecycleScope = "response"
	LifecycleScopeItem     LifecycleScope = "item"
	LifecycleScopeSegment  LifecycleScope = "segment"
)

type LifecycleState string

const (
	LifecycleStateQueued     LifecycleState = "queued"
	LifecycleStateInProgress LifecycleState = "in_progress"
	LifecycleStateAdded      LifecycleState = "added"
	LifecycleStateDone       LifecycleState = "done"
	LifecycleStateFailed     LifecycleState = "failed"
	LifecycleStateIncomplete LifecycleState = "incomplete"
)

type ContentKind string

const (
	ContentKindText      ContentKind = "text"
	ContentKindReasoning ContentKind = "reasoning"
	ContentKindRefusal   ContentKind = "refusal"
	ContentKindMedia     ContentKind = "media"
)

type ContentVariant string

const (
	ContentVariantPrimary    ContentVariant = "primary"
	ContentVariantSummary    ContentVariant = "summary"
	ContentVariantRaw        ContentVariant = "raw"
	ContentVariantTranscript ContentVariant = "transcript"
)

type ContentEncoding string

const (
	ContentEncodingUTF8   ContentEncoding = "utf8"
	ContentEncodingBase64 ContentEncoding = "base64"
)

type ToolDeltaKind string

const (
	ToolDeltaKindFunctionArguments ToolDeltaKind = "function_arguments"
	ToolDeltaKindCustomInput       ToolDeltaKind = "custom_input"
)

type StreamRef struct {
	ResponseID      string
	ItemIndex       *uint32
	ItemID          string
	SegmentIndex    *uint32
	AnnotationIndex *uint32
}

type Lifecycle struct {
	Scope    LifecycleScope
	State    LifecycleState
	Ref      StreamRef
	Kind     ContentKind
	Variant  ContentVariant
	ItemType string
	Mime     string
}

type ContentDelta struct {
	Ref       StreamRef
	Kind      ContentKind
	Variant   ContentVariant
	Mime      string
	Encoding  ContentEncoding
	Data      string
	Signature string
	Final     bool
}

type StreamContent struct {
	Ref         StreamRef
	Kind        ContentKind
	Variant     ContentVariant
	Mime        string
	Encoding    ContentEncoding
	Data        string
	Signature   string
	Annotations []Annotation
}

type ToolDelta struct {
	Ref      StreamRef
	Kind     ToolDeltaKind
	ToolID   string
	ToolName string
	Data     string
	Final    bool
}

type StreamToolCall struct {
	Ref      StreamRef
	ID       string
	Name     string
	RawInput string
	Args     map[string]any
}

type Annotation struct {
	Ref         StreamRef
	Type        string
	Text        string
	FileID      string
	Filename    string
	URL         string
	Title       string
	ContainerID string
	StartIndex  int
	EndIndex    int
	Offset      int
	Index       int
}

// EventExtras holds forward-compatible/raw event data.
type EventExtras struct {
	RawEventName string
	RawJSON      json.RawMessage
	Provider     map[string]any
}
