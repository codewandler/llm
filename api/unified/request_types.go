package unified

import (
	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

// Role is the canonical role used by unified.Request messages.
type Role string

const (
	RoleSystem    Role = Role(msg.RoleSystem)
	RoleDeveloper Role = Role(msg.RoleDeveloper)
	RoleUser      Role = Role(msg.RoleUser)
	RoleAssistant Role = Role(msg.RoleAssistant)
	RoleTool      Role = Role(msg.RoleTool)
)

// PartType is the canonical content-part type used in unified messages.
type PartType string

const (
	PartTypeText       PartType = PartType(msg.PartTypeText)
	PartTypeThinking   PartType = PartType(msg.PartTypeThinking)
	PartTypeToolCall   PartType = PartType(msg.PartTypeToolCall)
	PartTypeToolResult PartType = PartType(msg.PartTypeToolResult)
)

// Re-export core request enums from llm to keep unified and provider options aligned.
type (
	Effort       = llm.Effort
	ThinkingMode = llm.ThinkingMode
	OutputFormat = llm.OutputFormat
)

const (
	EffortUnspecified = llm.EffortUnspecified
	EffortLow         = llm.EffortLow
	EffortMedium      = llm.EffortMedium
	EffortHigh        = llm.EffortHigh
	EffortMax         = llm.EffortMax

	ThinkingAuto = llm.ThinkingAuto
	ThinkingOn   = llm.ThinkingOn
	ThinkingOff  = llm.ThinkingOff

	OutputFormatText = llm.OutputFormatText
	OutputFormatJSON = llm.OutputFormatJSON
)

// Request is the canonical internal request schema used by api/unified.
type Request struct {
	Model        string
	Messages     []Message
	MaxTokens    int
	Temperature  float64
	TopP         float64
	TopK         int
	OutputFormat OutputFormat

	Tools      []Tool
	ToolChoice llm.ToolChoice

	Effort   Effort
	Thinking ThinkingMode

	CacheHint   *msg.CacheHint
	UserID      string
	ApiTypeHint llm.ApiType

	Extras RequestExtras
}

// Message is a canonical conversation message.
type Message struct {
	Role      Role
	Parts     []Part
	CacheHint *msg.CacheHint
}

// Part is a canonical message content part.
type Part struct {
	Type       PartType
	Text       string
	Thinking   *ThinkingPart
	ToolCall   *ToolCall
	ToolResult *ToolResult
	Native     any
}

// ThinkingPart represents a thinking/reasoning content part.
type ThinkingPart struct {
	Provider  string
	Text      string
	Signature string
}

// ToolCall represents an assistant tool invocation.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolResult represents a tool output message.
type ToolResult struct {
	ToolCallID string
	ToolOutput string
	IsError    bool
}

// Tool is a canonical tool definition.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
	Strict      bool
}
