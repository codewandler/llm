package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/codewandler/llm"
)

// Request types for Anthropic API

type request struct {
	Model      string           `json:"model"`
	MaxTokens  int              `json:"max_tokens"`
	Stream     bool             `json:"stream"`
	System     any              `json:"system,omitempty"`
	Messages   []messagePayload `json:"messages"`
	Tools      []toolPayload    `json:"tools,omitempty"`
	ToolChoice any              `json:"tool_choice,omitempty"`
	Metadata   *metadata        `json:"metadata,omitempty"`
}

type metadata struct {
	UserID string `json:"user_id"`
}

type messagePayload struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// SystemBlock represents a system message block in the Anthropic API.
type SystemBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type contentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

// toolUseBlock is a specialized content block for tool_use that always includes the input field.
// Anthropic API requires the "input" field to be present in tool_use blocks, even if empty.
type toolUseBlock struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"` // No omitempty - must always be present
}

type toolPayload struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// RequestOptions contains options for building an Anthropic request.
type RequestOptions struct {
	Model         string
	MaxTokens     int
	SystemBlocks  []SystemBlock
	UserID        string
	StreamOptions llm.StreamOptions
}

// BuildRequest builds a JSON request body for the Anthropic API.
func BuildRequest(reqOpts RequestOptions) ([]byte, error) {
	opts := reqOpts.StreamOptions
	maxTokens := reqOpts.MaxTokens
	if maxTokens == 0 {
		maxTokens = 16384
	}

	r := request{Model: reqOpts.Model, MaxTokens: maxTokens, Stream: true}

	// Use provided system blocks or collect from messages
	if len(reqOpts.SystemBlocks) > 0 {
		r.System = reqOpts.SystemBlocks
	} else if sysBlocks := CollectSystemBlocks(opts.Messages); len(sysBlocks) > 0 {
		r.System = sysBlocks
	}

	if reqOpts.UserID != "" {
		r.Metadata = &metadata{UserID: reqOpts.UserID}
	}

	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, toolPayload{Name: t.Name, Description: t.Description, InputSchema: t.Parameters})
	}

	if len(opts.Tools) > 0 {
		switch tc := opts.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			r.ToolChoice = map[string]string{"type": "auto"}
		case llm.ToolChoiceRequired:
			r.ToolChoice = map[string]string{"type": "any"}
		case llm.ToolChoiceNone:
		case llm.ToolChoiceTool:
			r.ToolChoice = map[string]any{"type": "tool", "name": tc.Name}
		}
	}

	for i := 0; i < len(opts.Messages); i++ {
		switch m := opts.Messages[i].(type) {
		case *llm.SystemMsg:
			// Handled by system blocks above
		case *llm.UserMsg:
			r.Messages = append(r.Messages, messagePayload{Role: "user", Content: m.Content})
		case *llm.AssistantMsg:
			if len(m.ToolCalls) == 0 {
				r.Messages = append(r.Messages, messagePayload{Role: "assistant", Content: m.Content})
				continue
			}
			var blocks []any
			if m.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, toolUseBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: ensureInputMap(tc.Arguments)})
			}
			r.Messages = append(r.Messages, messagePayload{Role: "assistant", Content: blocks})
		case *llm.ToolCallResult:
			var results []contentBlock
			prevAssistant := FindPrecedingAssistant(opts.Messages, i)
			toolIdx := 0
			for ; i < len(opts.Messages); i++ {
				tr, ok := opts.Messages[i].(*llm.ToolCallResult)
				if !ok {
					break
				}
				toolUseID := tr.ToolCallID
				if toolUseID == "" && prevAssistant != nil && toolIdx < len(prevAssistant.ToolCalls) {
					toolUseID = prevAssistant.ToolCalls[toolIdx].ID
				}
				results = append(results, contentBlock{Type: "tool_result", ToolUseID: toolUseID, Content: tr.Output, IsError: tr.IsError})
				toolIdx++
			}
			i--
			r.Messages = append(r.Messages, messagePayload{Role: "user", Content: results})
		}
	}

	return json.Marshal(r)
}

// FindPrecedingAssistant finds the assistant message preceding the given index.
func FindPrecedingAssistant(messages llm.Messages, toolIdx int) *llm.AssistantMsg {
	for j := toolIdx - 1; j >= 0; j-- {
		if am, ok := messages[j].(*llm.AssistantMsg); ok {
			return am
		}
	}
	return nil
}

// CollectSystemBlocks extracts all SystemMsg from messages and returns them as SystemBlocks.
// It filters out empty content. This allows multiple system messages to be accumulated
// into an array format as supported by the Anthropic API.
func CollectSystemBlocks(messages llm.Messages) []SystemBlock {
	var blocks []SystemBlock
	for _, msg := range messages {
		if sm, ok := msg.(*llm.SystemMsg); ok {
			if strings.TrimSpace(sm.Content) != "" {
				blocks = append(blocks, SystemBlock{Type: "text", Text: sm.Content})
			}
		}
	}
	return blocks
}

// ensureInputMap ensures the input map is never nil.
// Anthropic API requires the "input" field to always be present in tool_use blocks.
func ensureInputMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// PrependSystemBlocks prepends blocks to existing system blocks.
func PrependSystemBlocks(prefix []SystemBlock, userBlocks []SystemBlock) []SystemBlock {
	result := make([]SystemBlock, 0, len(prefix)+len(userBlocks))
	result = append(result, prefix...)
	result = append(result, userBlocks...)
	return result
}

// NewSystemBlock creates a new system block with text content.
func NewSystemBlock(text string) SystemBlock {
	return SystemBlock{Type: "text", Text: text}
}
