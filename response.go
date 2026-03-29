package llm

import (
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
)

// --- StopReason ---

// StopReason describes why the model stopped generating.
type StopReason string

const (
	// StopReasonEndTurn is natural completion — the model finished its response.
	StopReasonEndTurn StopReason = "end_turn"
	// StopReasonToolUse means the model emitted one or more tool calls.
	StopReasonToolUse StopReason = "tool_use"
	// StopReasonMaxTokens means the output length limit was reached.
	StopReasonMaxTokens StopReason = "max_tokens"
	// StopReasonContentFilter means output was blocked by the provider.
	StopReasonContentFilter StopReason = "content_filter"
	// StopReasonCancelled means the context was cancelled before the eventPub ended.
	StopReasonCancelled StopReason = "cancelled"
	// StopReasonError means the eventPub ended with a StreamEventError.
	StopReasonError StopReason = "error"

	StopReasonUnknown StopReason = ""
)

type Response interface {
	Message() msg.Message
	Text() string
	Thought() string
	StopReason() StopReason
	Usage() *Usage
	Error() error
	ToolCalls() []tool.Call
	ToolResults() []tool.Result
}
