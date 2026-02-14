package llm

import "errors"

// Common errors
var (
	ErrNotFound   = errors.New("not found")
	ErrBadRequest = errors.New("bad request")
)

// Role represents the role of a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Model represents an LLM model.
type Model struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// Message represents a single message in a conversation.
type Message struct {
	ID         string     `json:"id,omitempty"`
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a request from the LLM to invoke a tool.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments map[string]any  `json:"arguments"`
	Result    *ToolCallResult `json:"result,omitempty"`
}

// ToolCallResult represents the result of executing a tool.
type ToolCallResult struct {
	Output  string `json:"output"`
	IsError bool   `json:"is_error"`
}
