package unified

import (
	"encoding/json"
	"fmt"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/completions"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/sortmap"
)

// BuildCompletionsRequest converts a canonical unified request to a Chat
// Completions wire request.
func BuildCompletionsRequest(r Request, _ ...CompletionsOption) (*completions.Request, error) {
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("validate unified request: %w", err)
	}

	out := &completions.Request{
		Model:         r.Model,
		Stream:        true,
		StreamOptions: &completions.StreamOptions{IncludeUsage: true},
		Messages:      make([]completions.Message, 0, len(r.Messages)),
	}

	if r.MaxTokens > 0 {
		out.MaxTokens = r.MaxTokens
	}
	if r.Temperature > 0 {
		out.Temperature = r.Temperature
	}
	if r.TopP > 0 {
		out.TopP = r.TopP
	}
	if r.TopK > 0 {
		out.TopK = r.TopK
	}
	if r.OutputFormat == llm.OutputFormatJSON {
		out.ResponseFormat = &completions.ResponseFormat{Type: "json_object"}
	}

	if !r.Effort.IsEmpty() {
		out.ReasoningEffort = string(r.Effort)
	}
	if r.CacheHint != nil && r.CacheHint.Enabled && r.CacheHint.TTL == "1h" {
		out.PromptCacheRetention = "24h"
	}

	for _, t := range r.Tools {
		out.Tools = append(out.Tools, completions.Tool{
			Type: "function",
			Function: completions.FuncPayload{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  sortmap.NewSortedMap(t.Parameters),
				Strict:      t.Strict,
			},
		})
	}

	if len(r.Tools) > 0 {
		switch tc := r.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			out.ToolChoice = "auto"
		case llm.ToolChoiceRequired:
			out.ToolChoice = "required"
		case llm.ToolChoiceNone:
			out.ToolChoice = "none"
		case llm.ToolChoiceTool:
			out.ToolChoice = map[string]any{
				"type":     "function",
				"function": map[string]string{"name": tc.Name},
			}
		default:
			return nil, fmt.Errorf("unsupported tool choice type %T", r.ToolChoice)
		}
	}

	for _, m := range r.Messages {
		wire := completions.Message{}
		switch Role(m.Role) {
		case RoleSystem:
			wire.Role = string(msg.RoleSystem)
			wire.Content = partsText(m.Parts)
		case RoleUser:
			wire.Role = string(msg.RoleUser)
			wire.Content = partsText(m.Parts)
		case RoleAssistant:
			wire.Role = string(msg.RoleAssistant)
			wire.Content = partsText(m.Parts)
			for _, p := range m.Parts {
				if p.Type != PartTypeToolCall || p.ToolCall == nil {
					continue
				}
				argRaw, _ := json.Marshal(p.ToolCall.Args)
				wire.ToolCalls = append(wire.ToolCalls, completions.ToolCall{
					ID:   p.ToolCall.ID,
					Type: "function",
					Function: completions.FuncCall{
						Name:      p.ToolCall.Name,
						Arguments: string(argRaw),
					},
				})
			}
		case RoleTool:
			for _, p := range m.Parts {
				if p.Type != PartTypeToolResult || p.ToolResult == nil {
					continue
				}
				out.Messages = append(out.Messages, completions.Message{
					Role:       string(msg.RoleTool),
					Content:    p.ToolResult.ToolOutput,
					ToolCallID: p.ToolResult.ToolCallID,
				})
			}
			continue
		default:
			wire.Role = string(m.Role)
			wire.Content = partsText(m.Parts)
		}
		out.Messages = append(out.Messages, wire)
	}

	return out, nil
}

// RequestFromCompletions converts a Chat Completions wire request to unified.
func RequestFromCompletions(r completions.Request) (Request, error) {
	u := Request{
		Model:        r.Model,
		MaxTokens:    r.MaxTokens,
		Temperature:  r.Temperature,
		TopP:         r.TopP,
		TopK:         r.TopK,
		OutputFormat: llm.OutputFormatText,
		Messages:     make([]Message, 0, len(r.Messages)),
	}

	if r.ResponseFormat != nil && r.ResponseFormat.Type == "json_object" {
		u.OutputFormat = llm.OutputFormatJSON
	}
	if r.ReasoningEffort != "" {
		u.Effort = llm.Effort(r.ReasoningEffort)
	}
	if r.PromptCacheRetention != "" {
		u.Extras.Completions = &CompletionsExtras{PromptCacheRetention: r.PromptCacheRetention}
		u.CacheHint = &msg.CacheHint{Enabled: true, TTL: "1h"}
	}

	for _, t := range r.Tools {
		u.Tools = append(u.Tools, Tool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  toMap(t.Function.Parameters),
			Strict:      t.Function.Strict,
		})
	}
	u.ToolChoice = toolChoiceFromCompletions(r.ToolChoice)

	for _, m := range r.Messages {
		um := Message{Role: Role(m.Role), Parts: make([]Part, 0, 2)}
		switch m.Role {
		case string(msg.RoleSystem):
			um.Role = RoleSystem
		case string(msg.RoleUser):
			um.Role = RoleUser
		case string(msg.RoleAssistant):
			um.Role = RoleAssistant
		case string(msg.RoleTool):
			um.Role = RoleTool
		}

		if text, ok := m.Content.(string); ok && text != "" {
			um.Parts = append(um.Parts, Part{Type: PartTypeText, Text: text})
		}
		for _, tc := range m.ToolCalls {
			var args map[string]any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			um.Parts = append(um.Parts, Part{Type: PartTypeToolCall, ToolCall: &ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: args}})
		}
		if m.ToolCallID != "" {
			um.Parts = append(um.Parts, Part{Type: PartTypeToolResult, ToolResult: &ToolResult{ToolCallID: m.ToolCallID, ToolOutput: contentString(m.Content)}})
		}
		if len(um.Parts) == 0 {
			um.Parts = []Part{{Type: PartTypeText, Text: ""}}
		}
		u.Messages = append(u.Messages, um)
	}

	if err := u.Validate(); err != nil {
		return Request{}, err
	}
	return u, nil
}

type CompletionsOption func(*completionsOptions)

type completionsOptions struct{}

func toolChoiceFromCompletions(v any) llm.ToolChoice {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		switch t {
		case "auto":
			return llm.ToolChoiceAuto{}
		case "required":
			return llm.ToolChoiceRequired{}
		case "none":
			return llm.ToolChoiceNone{}
		}
	case map[string]any:
		if typ, _ := t["type"].(string); typ == "function" {
			if fn, ok := t["function"].(map[string]any); ok {
				if name, _ := fn["name"].(string); name != "" {
					return llm.ToolChoiceTool{Name: name}
				}
			}
		}
	}
	return nil
}
