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
	if r.Output != nil {
		switch r.Output.Mode {
		case OutputModeText:
			// omit
		case OutputModeJSONObject:
			out.ResponseFormat = &completions.ResponseFormat{Type: "json_object"}
		case OutputModeJSONSchema:
			return nil, fmt.Errorf("chat completions request does not support output mode %q", r.Output.Mode)
		default:
			return nil, fmt.Errorf("unsupported output mode %q", r.Output.Mode)
		}
	}

	if !r.Effort.IsEmpty() {
		out.ReasoningEffort = string(r.Effort)
	}

	cextras := r.Extras.Completions
	if cextras != nil {
		out.Stop = append([]string(nil), cextras.Stop...)
		out.N = cextras.N
		out.PresencePenalty = cextras.PresencePenalty
		out.FrequencyPenalty = cextras.FrequencyPenalty
		out.LogProbs = cextras.LogProbs
		out.TopLogProbs = cextras.TopLogProbs
		out.Store = cextras.Store
		out.ParallelToolCalls = cextras.ParallelToolCalls
		out.ServiceTier = cextras.ServiceTier
		out.PromptCacheRetention = cextras.PromptCacheRetention
	}
	if retention := promptCacheRetentionFromHint(r.CacheHint); retention != "" {
		out.PromptCacheRetention = retention
	}
	out.User, out.Metadata = metadataToOpenAI(r.Metadata, nil)
	if cextras != nil {
		_, out.Metadata = metadataToOpenAI(r.Metadata, cextras.ExtraMetadata)
		out.User, _ = metadataToOpenAI(r.Metadata, nil)
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
		content, err := buildCompletionsContent(m)
		if err != nil {
			return nil, err
		}
		wire.Content = content
		switch Role(m.Role) {
		case RoleSystem, RoleDeveloper:
			wire.Role = string(msg.RoleSystem)
		case RoleUser:
			wire.Role = string(msg.RoleUser)
		case RoleAssistant:
			wire.Role = string(msg.RoleAssistant)
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
		}
		out.Messages = append(out.Messages, wire)
	}

	return out, nil
}

// RequestFromCompletions converts a Chat Completions wire request to unified.
func RequestFromCompletions(r completions.Request) (Request, error) {
	u := Request{
		Model:       r.Model,
		MaxTokens:   r.MaxTokens,
		Temperature: r.Temperature,
		TopP:        r.TopP,
		TopK:        r.TopK,
		Messages:    make([]Message, 0, len(r.Messages)),
	}

	if r.ResponseFormat != nil {
		switch r.ResponseFormat.Type {
		case "json_object":
			u.Output = &OutputSpec{Mode: OutputModeJSONObject}
		case "text":
			u.Output = &OutputSpec{Mode: OutputModeText}
		}
	}
	if r.ReasoningEffort != "" {
		u.Effort = llm.Effort(r.ReasoningEffort)
	}
	if hint := cacheHintFromPromptCacheRetention(r.PromptCacheRetention); hint != nil {
		u.CacheHint = hint
	}
	if meta, extra := metadataFromOpenAI(r.User, r.Metadata); meta != nil {
		u.Metadata = meta
		if extra != nil {
			ensureCompletionsExtras(&u).ExtraMetadata = extra
		}
	} else if extra != nil {
		ensureCompletionsExtras(&u).ExtraMetadata = extra
	}
	if r.PromptCacheRetention != "" {
		ensureCompletionsExtras(&u).PromptCacheRetention = r.PromptCacheRetention
	}
	if len(r.Stop) > 0 {
		ensureCompletionsExtras(&u).Stop = append([]string(nil), r.Stop...)
	}
	if r.N > 0 || r.PresencePenalty != 0 || r.FrequencyPenalty != 0 || r.LogProbs || r.TopLogProbs > 0 || r.Store || r.ParallelToolCalls || r.ServiceTier != "" {
		extras := ensureCompletionsExtras(&u)
		extras.N = r.N
		extras.PresencePenalty = r.PresencePenalty
		extras.FrequencyPenalty = r.FrequencyPenalty
		extras.LogProbs = r.LogProbs
		extras.TopLogProbs = r.TopLogProbs
		extras.Store = r.Store
		extras.ParallelToolCalls = r.ParallelToolCalls
		extras.ServiceTier = r.ServiceTier
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
		} else if m.Content != nil {
			um.Parts = append(um.Parts, Part{Native: m.Content})
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

func buildCompletionsContent(m Message) (any, error) {
	var native any
	hasNative := false
	hasCanonical := false

	for _, p := range m.Parts {
		if p.Native != nil {
			if hasNative {
				return nil, fmt.Errorf("chat completions message cannot project multiple native content parts")
			}
			if partHasCanonicalFields(p) {
				return nil, fmt.Errorf("chat completions native content part must not also carry canonical fields")
			}
			native = p.Native
			hasNative = true
			continue
		}
		if partContributesCanonicalContent(p) {
			hasCanonical = true
		}
	}

	if hasNative && hasCanonical {
		return nil, fmt.Errorf("chat completions message cannot mix native content with canonical parts")
	}
	if hasNative {
		return native, nil
	}
	return partsText(m.Parts), nil
}

func partHasCanonicalFields(p Part) bool {
	return p.Type != "" || p.Text != "" || p.Thinking != nil || p.ToolCall != nil || p.ToolResult != nil
}

func partContributesCanonicalContent(p Part) bool {
	if p.Native != nil {
		return false
	}
	if p.Thinking != nil || p.ToolCall != nil || p.ToolResult != nil {
		return true
	}
	return p.Type == PartTypeText && p.Text != ""
}
