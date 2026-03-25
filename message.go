package llm

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/codewandler/llm/tool"
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

type (
	Messages []Message

	// Message is the interface all message types implement.
	Message interface {
		Role() Role
		Validate() error
		CacheHint() *CacheHint
		MarshalJSON() ([]byte, error)
		UnmarshalJSON([]byte) error
		applyCache(cache *CacheHint)
	}

	TextMessage interface {
		Message
		Content() string
	}

	UserMessage interface {
		TextMessage
		isUser()
	}

	SystemMessage interface {
		TextMessage
		isSystem()
	}

	AssistantMessage interface {
		TextMessage
		ToolCalls() []tool.Call
		isAssistant()
	}

	ToolMessage interface {
		Message
		ToolCallID() string
		ToolOutput() string
		IsError() bool
		isTool()
	}
)

// --- Concrete Message Types ---

type (
	textMsg struct {
		content   string
		role      Role
		cacheHint *CacheHint
	}
	systemMsg    struct{ textMsg }
	userMsg      struct{ textMsg }
	assistantMsg struct {
		textMsg
		toolCalls []tool.Call
	}
	toolMsg struct {
		textMsg
		toolCallID string
		isError    bool
	}
)

func (m *textMsg) Role() Role            { return m.role }
func (m *textMsg) Content() string       { return m.content }
func (m *textMsg) CacheHint() *CacheHint { return m.cacheHint }
func (m *textMsg) Validate() error {
	if m.content == "" {
		return errors.New("message: content is required")
	}
	return nil
}
func (m *textMsg) applyCache(cache *CacheHint) {
	if cache != nil {
		m.cacheHint = cache
	}
}

func System(content string) SystemMessage {
	return &systemMsg{
		textMsg{
			role:    RoleSystem,
			content: content,
		},
	}
}

func User(content string) UserMessage {
	return &userMsg{
		textMsg{
			role:    RoleUser,
			content: content,
		},
	}
}

func ToolCalls(toolCalls ...tool.Call) AssistantMessage {
	return &assistantMsg{
		textMsg: textMsg{
			role: RoleAssistant,
		},
		toolCalls: toolCalls,
	}
}

func Assistant(content string, toolCalls ...tool.Call) AssistantMessage {
	return &assistantMsg{
		textMsg: textMsg{
			role:    RoleAssistant,
			content: content,
		},
		toolCalls: toolCalls,
	}
}

func newToolMsg(toolCallID, output string, isError bool) ToolMessage {
	return &toolMsg{
		textMsg: textMsg{
			role:    RoleTool,
			content: output,
		},
		toolCallID: toolCallID,
		isError:    isError,
	}
}

func ToolResult(tr tool.Result) ToolMessage {
	var output string
	switch x := tr.ToolOutput().(type) {
	case string:
		output = x
	case fmt.Stringer:
		output = x.String()
	default:
		d, _ := json.Marshal(x)
		output = string(d)
	}

	return newToolMsg(tr.ToolCallID(), output, tr.IsError())
}

func Tool(toolCallID, output string) ToolMessage {
	return newToolMsg(toolCallID, output, false)
}

func ToolErr(toolCallID, output string) ToolMessage {
	return newToolMsg(toolCallID, output, true)
}

func (m *systemMsg) isSystem()        {}
func (m *userMsg) isUser()            {}
func (m *toolMsg) ToolCallID() string { return m.toolCallID }
func (m *toolMsg) ToolOutput() string { return m.textMsg.content }
func (m *toolMsg) IsError() bool      { return false }
func (m *toolMsg) isTool()            {}
func (m *assistantMsg) isAssistant()  {}
func (m *assistantMsg) ToolCalls() []tool.Call {
	return m.toolCalls
}

func (m *assistantMsg) Validate() error {
	if err := m.textMsg.Validate(); err != nil {
		return err
	}
	for i, tc := range m.toolCalls {
		if err := tc.Validate(); err != nil {
			return fmt.Errorf("assistant message: tool_calls[%d]: %w", i, err)
		}
	}
	return nil
}

func (m *toolMsg) Validate() error {
	if err := m.textMsg.Validate(); err != nil {
		return err
	}
	if m.toolCallID == "" {
		return errors.New("tool message: tool_call_id is required")
	}
	return nil
}

// --- MessageToolCall ---

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

func (m *Messages) Add(all ...Message) *Messages {
	for _, msg := range all {
		if msg == nil {
			continue
		}
	}
	*m = append(*m, all...)
	return m
}

func (m *Messages) System(content string) *Messages {
	return m.Add(System(content))
}

func (m *Messages) User(content string) *Messages {
	return m.Add(User(content))
}

func (m *Messages) ToolErr(toolCallID, output string) *Messages {
	return m.Add(ToolErr(toolCallID, output))
}

func (m *Messages) Tool(toolCallID, output string) *Messages {
	return m.Add(Tool(toolCallID, output))
}

func (m *Messages) Assistant(content string, toolCalls ...tool.Call) *Messages {
	return m.Add(Assistant(content, toolCalls...))
}
