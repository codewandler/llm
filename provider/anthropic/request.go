package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/codewandler/llm"
)

// Request types for Anthropic API

type request struct {
	Model        string           `json:"model"`
	MaxTokens    int              `json:"max_tokens"`
	Stream       bool             `json:"stream"`
	System       any              `json:"system,omitempty"`
	Messages     []messagePayload `json:"messages"`
	Tools        []toolPayload    `json:"tools,omitempty"`
	ToolChoice   any              `json:"tool_choice,omitempty"`
	Metadata     *metadata        `json:"metadata,omitempty"`
	CacheControl *cacheControl    `json:"cache_control,omitempty"`
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
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type contentBlock struct {
	Type         string         `json:"type"`
	Text         string         `json:"text,omitempty"`
	ID           string         `json:"id,omitempty"`
	Name         string         `json:"name,omitempty"`
	Input        map[string]any `json:"input,omitempty"`
	ToolUseID    string         `json:"tool_use_id,omitempty"`
	Content      string         `json:"content,omitempty"`
	IsError      bool           `json:"is_error,omitempty"`
	CacheControl *cacheControl  `json:"cache_control,omitempty"`
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
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	InputSchema  any           `json:"input_schema"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

// cacheControl is the Anthropic API wire type for cache breakpoints.
type cacheControl struct {
	Type string `json:"type"`          // always "ephemeral"
	TTL  string `json:"ttl,omitempty"` // "1h" for extended TTL; omit for default 5m
}

// RequestOptions contains options for building an Anthropic request.
type RequestOptions struct {
	Model         string
	MaxTokens     int
	SystemBlocks  []SystemBlock
	UserID        string
	StreamOptions llm.StreamOptions
}

// buildCacheControl converts a CacheHint to the Anthropic wire type.
// Returns nil if hint is nil or not enabled.
func buildCacheControl(h *llm.CacheHint) *cacheControl {
	if h == nil || !h.Enabled {
		return nil
	}
	cc := &cacheControl{Type: "ephemeral"}
	if h.TTL == "1h" {
		cc.TTL = "1h"
	}
	return cc
}

// hasPerMessageCacheHints returns true if any message carries an enabled CacheHint.
func hasPerMessageCacheHints(msgs llm.Messages) bool {
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *llm.SystemMsg:
			if m.CacheHint != nil && m.CacheHint.Enabled {
				return true
			}
		case *llm.UserMsg:
			if m.CacheHint != nil && m.CacheHint.Enabled {
				return true
			}
		case *llm.AssistantMsg:
			if m.CacheHint != nil && m.CacheHint.Enabled {
				return true
			}
		case *llm.ToolCallResult:
			if m.CacheHint != nil && m.CacheHint.Enabled {
				return true
			}
		}
	}
	return false
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
		r.Tools = append(r.Tools, toolPayload{Name: t.Name, Description: t.Description, InputSchema: llm.NewSortedMap(t.Parameters)})
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

	// Apply top-level (automatic) cache hint when no per-message hints exist.
	// This emits cache_control at the request level, instructing Anthropic to
	// automatically place a breakpoint at the last cacheable content block.
	if opts.CacheHint != nil && opts.CacheHint.Enabled && !hasPerMessageCacheHints(opts.Messages) {
		r.CacheControl = buildCacheControl(opts.CacheHint)
	}

	for i := 0; i < len(opts.Messages); i++ {
		switch m := opts.Messages[i].(type) {
		case *llm.SystemMsg:
			// Handled by system blocks above
		case *llm.UserMsg:
			block := contentBlock{Type: "text", Text: m.Content, CacheControl: buildCacheControl(m.CacheHint)}
			r.Messages = append(r.Messages, messagePayload{Role: "user", Content: []contentBlock{block}})
		case *llm.AssistantMsg:
			if len(m.ToolCalls) == 0 {
				block := contentBlock{Type: "text", Text: m.Content, CacheControl: buildCacheControl(m.CacheHint)}
				r.Messages = append(r.Messages, messagePayload{Role: "assistant", Content: []contentBlock{block}})
				continue
			}
			var blocks []any
			if m.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: m.Content})
			}
			for j, tc := range m.ToolCalls {
				tub := toolUseBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: ensureInputMap(tc.Arguments)}
				// Attach cache_control to the last block in the assistant message
				if m.CacheHint != nil && m.CacheHint.Enabled && j == len(m.ToolCalls)-1 {
					// Use a contentBlock wrapper that supports cache_control
					blocks = append(blocks, struct {
						Type         string         `json:"type"`
						ID           string         `json:"id"`
						Name         string         `json:"name"`
						Input        map[string]any `json:"input"`
						CacheControl *cacheControl  `json:"cache_control,omitempty"`
					}{tub.Type, tub.ID, tub.Name, tub.Input, buildCacheControl(m.CacheHint)})
				} else {
					blocks = append(blocks, tub)
				}
			}
			r.Messages = append(r.Messages, messagePayload{Role: "assistant", Content: blocks})
		case *llm.ToolCallResult:
			var results []contentBlock
			prevAssistant := FindPrecedingAssistant(opts.Messages, i)
			toolIdx := 0
			startI := i
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
			// Apply cache hint from the last ToolCallResult in this batch
			if i > startI {
				if lastTR, ok := opts.Messages[i-1].(*llm.ToolCallResult); ok {
					if cc := buildCacheControl(lastTR.CacheHint); cc != nil {
						results[len(results)-1].CacheControl = cc
					}
				}
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
				blocks = append(blocks, SystemBlock{
					Type:         "text",
					Text:         sm.Content,
					CacheControl: buildCacheControl(sm.CacheHint),
				})
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
