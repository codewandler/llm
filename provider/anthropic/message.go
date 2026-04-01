package anthropic

import (
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

type (
	Message struct {
		Role    string         `json:"role"`
		Content MessageContent `json:"content"`
	}

	MessageContent []MessageContentBlock

	MessageContentBlock interface {
		isMessageContentBlock()
		setCacheControl(control *CacheControl)
	}

	baseContentBlock struct {
		Type         string        `json:"type"`
		CacheControl *CacheControl `json:"cache_control,omitempty"`
	}

	ToolUseBlock struct {
		baseContentBlock
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	}

	ToolResultBlock struct {
		baseContentBlock
		ToolUseID string `json:"tool_use_id"`
		Content   string `json:"content"`
		IsError   bool   `json:"is_error"`
	}

	TextBlock struct {
		baseContentBlock
		Text string `json:"text"`
	}

	ThinkingBlock struct {
		baseContentBlock
		Thinking  string `json:"thinking"`
		Signature string `json:"signature"`
	}
)

type MessageContentBlockX struct {
	Type string `json:"type"`

	// === Text ===

	Text string `json:"text,omitempty"`

	// === ToolUse Request ===
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"` // No omitempty — Anthropic requires this field

	// === ToolUse Result ===

	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`

	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

func (b *baseContentBlock) setCacheControl(cc *CacheControl) {
	b.CacheControl = cc
}

func (b ToolUseBlock) isMessageContentBlock()            {}
func (b *ToolUseBlock) setCacheControl(cc *CacheControl) { b.baseContentBlock.setCacheControl(cc) }

func (b ToolResultBlock) isMessageContentBlock()            {}
func (b *ToolResultBlock) setCacheControl(cc *CacheControl) { b.baseContentBlock.setCacheControl(cc) }

// === Constructors ===

func ToolUse(id, name string, input map[string]any) ToolUseBlock {
	b := ToolUseBlock{
		baseContentBlock: baseContentBlock{Type: "tool_use"},
		Name:             name,
		ID:               id,
		Input:            input,
	}
	if b.Input == nil {
		b.Input = map[string]any{}
	}
	return b
}

func ToolResult(id, content string, isError bool) ToolResultBlock {
	return ToolResultBlock{
		baseContentBlock: baseContentBlock{Type: "tool_result"},
		ToolUseID:        id,
		Content:          content,
		IsError:          isError,
	}
}

func (b TextBlock) isMessageContentBlock()                        {}
func (b *TextBlock) WithCacheControl(cc *CacheControl) *TextBlock { b.setCacheControl(cc); return b }

func Text(text string) *TextBlock {
	return &TextBlock{
		baseContentBlock: baseContentBlock{Type: "text"},
		Text:             text,
	}
}

func (b *ThinkingBlock) isMessageContentBlock() {}
func (b *ThinkingBlock) WithCacheControl(cc *CacheControl) *ThinkingBlock {
	b.setCacheControl(cc)
	return b
}
func Thinking(thinking, signature string) *ThinkingBlock {
	return &ThinkingBlock{
		baseContentBlock: baseContentBlock{Type: "thinking"},
		Thinking:         thinking,
		Signature:        signature,
	}
}

func convertMessages(messages msg.Messages) (systemBlocks []*TextBlock, out []Message) {

	for i := 0; i < len(messages); i++ {
		m := messages[i]
		switch m.Role {
		case llm.RoleSystem:
			content := strings.TrimSpace(m.Parts.Text())
			if content != "" {
				tb := Text(content)
				tb.setCacheControl(buildCacheControl(m.CacheHint))
				systemBlocks = append(systemBlocks, tb)
			}

		case llm.RoleUser:
			mm := Message{
				Role: "user",
				Content: []MessageContentBlock{
					Text(m.Text()),
				},
			}
			mm.setCacheControl(m.CacheHint)
			out = append(out, mm)
		case llm.RoleAssistant:
			var blocks []MessageContentBlock
			// 1. Thinking
			for _, t := range m.Parts.ByType(msg.PartTypeThinking) {
				blocks = append(blocks, Thinking(t.Thinking.Text, t.Thinking.Signature))
			}
			// 2. Text
			for _, p := range m.Parts.ByType(msg.PartTypeText) {
				blocks = append(blocks, Text(p.Text))
			}
			// 3. Tool calls
			for _, p := range m.Parts.ByType(msg.PartTypeToolCall) {
				tc := p.ToolCall
				tub := ToolUse(tc.ID, tc.Name, tc.Args)
				blocks = append(blocks, &tub)
			}
			mm := Message{Role: "assistant", Content: blocks}
			mm.setCacheControl(m.CacheHint)
			out = append(out, mm)
		case llm.RoleTool:
			var blocks []MessageContentBlock
			for _, p := range m.Parts {
				switch p.Type {
				case msg.PartTypeToolResult:
					tr := p.ToolResult
					trb := ToolResult(tr.ToolCallID, tr.ToolOutput, tr.IsError)
					blocks = append(blocks, &trb)
				}
			}
			// TODO: add additional user messages if present
			mm := Message{Role: "user", Content: blocks}
			mm.setCacheControl(m.CacheHint)
			out = append(out, mm)
		}
	}

	return systemBlocks, out
}
