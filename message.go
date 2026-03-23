package llm

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Role represents the role of a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// --- Message Interface ---

// Message is the interface all message types implement.
type Message interface {
	Role() Role
	Validate() error
	json.Marshaler
	messageMarker() // unexported - prevents external implementations
}

// --- Concrete Message Types ---

// SystemMsg contains a system prompt.
type SystemMsg struct {
	Content   string
	CacheHint *CacheHint
}

func (m *SystemMsg) Role() Role { return RoleSystem }

func (m *SystemMsg) Validate() error {
	if m.Content == "" {
		return errors.New("system message: content is required")
	}
	return nil
}

func (m *SystemMsg) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Role    Role   `json:"role"`
		Content string `json:"content"`
	}{RoleSystem, m.Content})
}

func (m *SystemMsg) messageMarker() {}

// UserMsg contains user input.
type UserMsg struct {
	Content   string
	CacheHint *CacheHint
}

func (m *UserMsg) Role() Role { return RoleUser }

func (m *UserMsg) Validate() error {
	if m.Content == "" {
		return errors.New("user message: content is required")
	}
	return nil
}

func (m *UserMsg) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Role    Role   `json:"role"`
		Content string `json:"content"`
	}{RoleUser, m.Content})
}

func (m *UserMsg) messageMarker() {}

// AssistantMsg contains an assistant response, optionally with tool calls.
type AssistantMsg struct {
	Content   string
	ToolCalls []ToolCall
	CacheHint *CacheHint
}

func (m *AssistantMsg) Role() Role { return RoleAssistant }

func (m *AssistantMsg) Validate() error {
	if m.Content == "" && len(m.ToolCalls) == 0 {
		return errors.New("assistant message: content or tool_calls is required")
	}
	for i, tc := range m.ToolCalls {
		if err := tc.Validate(); err != nil {
			return fmt.Errorf("assistant message: tool_calls[%d]: %w", i, err)
		}
	}
	return nil
}

func (m *AssistantMsg) MarshalJSON() ([]byte, error) {
	type wire struct {
		Role      Role       `json:"role"`
		Content   string     `json:"content,omitempty"`
		ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	}
	return json.Marshal(wire{RoleAssistant, m.Content, m.ToolCalls})
}

func (m *AssistantMsg) messageMarker() {}

// ToolCallResult contains the result of executing a tool call.
type ToolCallResult struct {
	ToolCallID string
	Output     string
	IsError    bool
	CacheHint  *CacheHint
}

func (m *ToolCallResult) Role() Role { return RoleTool }

func (m *ToolCallResult) Validate() error {
	if m.ToolCallID == "" {
		return errors.New("tool call result: tool_call_id is required")
	}
	if m.Output == "" {
		return errors.New("tool call result: output is required")
	}
	return nil
}

func (m *ToolCallResult) MarshalJSON() ([]byte, error) {
	// Output marshals as "content" for backwards compatibility
	type wire struct {
		Role       Role   `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
		IsError    bool   `json:"is_error,omitempty"`
	}
	return json.Marshal(wire{RoleTool, m.ToolCallID, m.Output, m.IsError})
}

func (m *ToolCallResult) messageMarker() {}

// --- ToolCall ---

// ToolCall represents a request from the LLM to invoke a tool.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

func (tc ToolCall) Validate() error {
	if tc.ID == "" {
		return errors.New("tool call: id is required")
	}
	if tc.Name == "" {
		return errors.New("tool call: name is required")
	}
	return nil
}

func (tc ToolCall) MarshalJSON() ([]byte, error) {
	type wire struct {
		ID        string         `json:"id"`
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	return json.Marshal(wire{tc.ID, tc.Name, tc.Arguments})
}

func (tc *ToolCall) UnmarshalJSON(data []byte) error {
	type wire struct {
		ID        string         `json:"id"`
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	tc.ID = w.ID
	tc.Name = w.Name
	tc.Arguments = w.Arguments
	return nil
}

// --- ToolChoice ---

// ToolChoice controls whether and which tools the model should call.
type ToolChoice interface {
	toolChoice() // marker method - prevents external implementations
}

// ToolChoiceAuto lets the model decide whether to call tools.
// This is the default behavior when ToolChoice is nil.
type ToolChoiceAuto struct{}

func (ToolChoiceAuto) toolChoice() {}

// ToolChoiceRequired forces the model to call at least one tool.
type ToolChoiceRequired struct{}

func (ToolChoiceRequired) toolChoice() {}

// ToolChoiceNone prevents the model from calling any tools.
type ToolChoiceNone struct{}

func (ToolChoiceNone) toolChoice() {}

// ToolChoiceTool forces the model to call a specific tool by name.
type ToolChoiceTool struct {
	Name string
}

func (ToolChoiceTool) toolChoice() {}

// --- CacheHint ---

// CacheHint requests provider-side prompt caching for a message or request.
// It is a provider-neutral instruction: Anthropic and Bedrock translate it to
// explicit cache breakpoints on content blocks; OpenAI caching is always
// automatic and ignores per-message hints, but honours TTL on
// Request.CacheHint.
type CacheHint struct {
	// Enabled marks this content as a cache breakpoint candidate.
	// For Anthropic/Bedrock: emits cache_control / cachePoint at this position.
	// For OpenAI: no-op (caching is automatic).
	Enabled bool

	// TTL requests a specific cache duration.
	// Valid values: "" (provider default, typically 5m), "5m", "1h".
	// The "1h" option requires a supporting model (Claude Haiku/Sonnet/Opus 4.5+).
	TTL string
}

// --- Messages Wrapper ---

// Messages is a slice of Message with JSON unmarshal support.
type Messages []Message

func (m *Messages) Append(msg ...Message) {
	*m = append(*m, msg...)
}

func (m *Messages) AddUserMsg(content string) {
	*m = append(*m, &UserMsg{Content: content})
}

func (m *Messages) AddAssistantMsg(content string, toolCalls ...ToolCall) {
	*m = append(*m, &AssistantMsg{Content: content, ToolCalls: toolCalls})
}

func (m *Messages) AddToolCallResult(toolCallID, output string, isError bool) {
	*m = append(*m, &ToolCallResult{ToolCallID: toolCallID, Output: output, IsError: isError})
}

func (m *Messages) AddSystemMsg(content string) {
	*m = append(*m, &SystemMsg{Content: content})
}

func (m *Messages) UnmarshalJSON(data []byte) error {
	var rawMessages []json.RawMessage
	if err := json.Unmarshal(data, &rawMessages); err != nil {
		return err
	}

	*m = make(Messages, 0, len(rawMessages))
	for i, raw := range rawMessages {
		// Peek at the role field
		var peek struct {
			Role Role `json:"role"`
		}
		if err := json.Unmarshal(raw, &peek); err != nil {
			return fmt.Errorf("message[%d]: %w", i, err)
		}

		var msg Message
		switch peek.Role {
		case RoleSystem:
			var sm struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(raw, &sm); err != nil {
				return fmt.Errorf("message[%d]: %w", i, err)
			}
			msg = &SystemMsg{Content: sm.Content}

		case RoleUser:
			var um struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(raw, &um); err != nil {
				return fmt.Errorf("message[%d]: %w", i, err)
			}
			msg = &UserMsg{Content: um.Content}

		case RoleAssistant:
			var am struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			}
			if err := json.Unmarshal(raw, &am); err != nil {
				return fmt.Errorf("message[%d]: %w", i, err)
			}
			msg = &AssistantMsg{Content: am.Content, ToolCalls: am.ToolCalls}

		case RoleTool:
			var tr struct {
				ToolCallID string `json:"tool_call_id"`
				Content    string `json:"content"`
				IsError    bool   `json:"is_error"`
			}
			if err := json.Unmarshal(raw, &tr); err != nil {
				return fmt.Errorf("message[%d]: %w", i, err)
			}
			msg = &ToolCallResult{ToolCallID: tr.ToolCallID, Output: tr.Content, IsError: tr.IsError}

		default:
			return fmt.Errorf("message[%d]: unknown role %q", i, peek.Role)
		}

		*m = append(*m, msg)
	}

	return nil
}
