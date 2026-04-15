package unified

import (
	"encoding/json"
	"fmt"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/sortmap"
)

// BuildResponsesRequest converts a canonical unified request to a Responses API
// wire request.
func BuildResponsesRequest(r Request, _ ...ResponsesOption) (*responses.Request, error) {
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("validate unified request: %w", err)
	}

	out := &responses.Request{
		Model:  r.Model,
		Stream: true,
		Input:  make([]responses.Input, 0, len(r.Messages)),
	}

	if r.MaxTokens > 0 {
		out.MaxOutputTokens = r.MaxTokens
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
		out.ResponseFormat = &responses.ResponseFormat{Type: "json_object"}
	}
	if !r.Effort.IsEmpty() {
		out.Reasoning = &responses.Reasoning{Effort: string(r.Effort)}
	}
	if r.CacheHint != nil && r.CacheHint.Enabled && r.CacheHint.TTL == "1h" {
		out.PromptCacheRetention = "24h"
	}

	for _, t := range r.Tools {
		out.Tools = append(out.Tools, responses.Tool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  sortmap.NewSortedMap(t.Parameters),
			Strict:      t.Strict,
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
			out.ToolChoice = map[string]any{"type": "function", "name": tc.Name}
		default:
			return nil, fmt.Errorf("unsupported tool choice type %T", r.ToolChoice)
		}
	}

	instructionsSet := false
	for _, m := range r.Messages {
		switch Role(m.Role) {
		case RoleSystem:
			text := partsText(m.Parts)
			if !instructionsSet {
				out.Instructions = text
				instructionsSet = true
			} else {
				out.Input = append(out.Input, responses.Input{Role: string(msg.RoleDeveloper), Content: text})
			}
		case RoleDeveloper:
			out.Input = append(out.Input, responses.Input{Role: string(msg.RoleDeveloper), Content: partsText(m.Parts)})
		case RoleUser:
			out.Input = append(out.Input, responses.Input{Role: string(msg.RoleUser), Content: partsText(m.Parts)})
		case RoleAssistant:
			text := partsText(m.Parts)
			if text != "" {
				out.Input = append(out.Input, responses.Input{Role: string(msg.RoleAssistant), Content: text})
			}
			for _, p := range m.Parts {
				if p.Type != PartTypeToolCall || p.ToolCall == nil {
					continue
				}
				argRaw, _ := json.Marshal(p.ToolCall.Args)
				out.Input = append(out.Input, responses.Input{
					Type:      "function_call",
					CallID:    p.ToolCall.ID,
					Name:      p.ToolCall.Name,
					Arguments: string(argRaw),
				})
			}
		case RoleTool:
			for _, p := range m.Parts {
				if p.Type != PartTypeToolResult || p.ToolResult == nil {
					continue
				}
				out.Input = append(out.Input, responses.Input{
					Type:   "function_call_output",
					CallID: p.ToolResult.ToolCallID,
					Output: p.ToolResult.ToolOutput,
				})
			}
		}
	}

	return out, nil
}

// RequestFromResponses converts a Responses wire request to unified.
func RequestFromResponses(r responses.Request) (Request, error) {
	u := Request{
		Model:        r.Model,
		MaxTokens:    r.MaxOutputTokens,
		Temperature:  r.Temperature,
		TopP:         r.TopP,
		TopK:         r.TopK,
		OutputFormat: llm.OutputFormatText,
		Messages:     make([]Message, 0, len(r.Input)+1),
	}
	if r.ResponseFormat != nil && r.ResponseFormat.Type == "json_object" {
		u.OutputFormat = llm.OutputFormatJSON
	}
	if r.Reasoning != nil && r.Reasoning.Effort != "" {
		u.Effort = llm.Effort(r.Reasoning.Effort)
	}
	if r.PromptCacheRetention != "" {
		u.Extras.Responses = &ResponsesExtras{PromptCacheRetention: r.PromptCacheRetention}
		u.CacheHint = &msg.CacheHint{Enabled: true, TTL: "1h"}
	}

	for _, t := range r.Tools {
		u.Tools = append(u.Tools, Tool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  toMap(t.Parameters),
			Strict:      t.Strict,
		})
	}
	u.ToolChoice = toolChoiceFromResponses(r.ToolChoice)

	if r.Instructions != "" {
		u.Messages = append(u.Messages, Message{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: r.Instructions}}})
	}

	for _, in := range r.Input {
		switch {
		case in.Role == string(msg.RoleDeveloper):
			u.Messages = append(u.Messages, Message{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: in.Content}}})
		case in.Role == string(msg.RoleUser):
			u.Messages = append(u.Messages, Message{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: in.Content}}})
		case in.Role == string(msg.RoleAssistant):
			u.Messages = append(u.Messages, Message{Role: RoleAssistant, Parts: []Part{{Type: PartTypeText, Text: in.Content}}})
		case in.Type == "function_call":
			var args map[string]any
			if in.Arguments != "" {
				_ = json.Unmarshal([]byte(in.Arguments), &args)
			}
			u.Messages = append(u.Messages, Message{Role: RoleAssistant, Parts: []Part{{Type: PartTypeToolCall, ToolCall: &ToolCall{ID: in.CallID, Name: in.Name, Args: args}}}})
		case in.Type == "function_call_output":
			u.Messages = append(u.Messages, Message{Role: RoleTool, Parts: []Part{{Type: PartTypeToolResult, ToolResult: &ToolResult{ToolCallID: in.CallID, ToolOutput: in.Output}}}})
		}
	}

	if err := u.Validate(); err != nil {
		return Request{}, err
	}
	return u, nil
}

type ResponsesOption func(*responsesOptions)

type responsesOptions struct{}

func toolChoiceFromResponses(v any) llm.ToolChoice {
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
			if name, _ := t["name"].(string); name != "" {
				return llm.ToolChoiceTool{Name: name}
			}
		}
	}
	return nil
}
