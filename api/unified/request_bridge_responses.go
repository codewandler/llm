package unified

import (
	"encoding/json"
	"fmt"
	"strings"

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

	rextras := r.Extras.Responses
	usedMaxField := "max_output_tokens"
	if rextras != nil && rextras.UsedMaxTokenField != "" {
		usedMaxField = rextras.UsedMaxTokenField
	}
	if r.MaxTokens > 0 {
		if usedMaxField == "max_tokens" {
			out.MaxTokens = r.MaxTokens
		} else {
			out.MaxOutputTokens = r.MaxTokens
		}
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
			out.ResponseFormat = &responses.ResponseFormat{Type: "json_object"}
		case OutputModeJSONSchema:
			return nil, fmt.Errorf("responses request does not support output mode %q", r.Output.Mode)
		default:
			return nil, fmt.Errorf("unsupported output mode %q", r.Output.Mode)
		}
	}
	if !r.Effort.IsEmpty() || (rextras != nil && rextras.ReasoningSummary != "") {
		out.Reasoning = &responses.Reasoning{Effort: string(r.Effort)}
		if rextras != nil {
			out.Reasoning.Summary = rextras.ReasoningSummary
		}
	}
	if rextras != nil {
		out.PromptCacheRetention = rextras.PromptCacheRetention
		out.PreviousResponseID = rextras.PreviousResponseID
		out.Store = rextras.Store
		out.ParallelToolCalls = rextras.ParallelToolCalls
	}
	if retention := promptCacheRetentionFromHint(r.CacheHint); retention != "" {
		out.PromptCacheRetention = retention
	}
	out.User, out.Metadata = metadataToOpenAI(r.Metadata, nil)
	if rextras != nil {
		_, out.Metadata = metadataToOpenAI(r.Metadata, rextras.ExtraMetadata)
		out.User, _ = metadataToOpenAI(r.Metadata, nil)
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

	useInstructions := true
	if rextras != nil && rextras.UseInstructions != nil {
		useInstructions = *rextras.UseInstructions
	}
	instructions, remaining, err := consumeResponsesInstruction(r.Messages, useInstructions)
	if err != nil {
		return nil, err
	}
	out.Instructions = instructions
	for _, m := range remaining {
		switch Role(m.Role) {
		case RoleSystem:
			return nil, fmt.Errorf("responses request cannot project additional system messages")
		case RoleDeveloper:
			out.Input = append(out.Input, responses.Input{Role: string(msg.RoleDeveloper), Content: partsText(m.Parts)})
		case RoleUser:
			out.Input = append(out.Input, responses.Input{Role: string(msg.RoleUser), Content: partsText(m.Parts)})
		case RoleAssistant:
			inputs, err := buildResponsesAssistantInputs(m)
			if err != nil {
				return nil, err
			}
			out.Input = append(out.Input, inputs...)
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
		Model:       r.Model,
		Temperature: r.Temperature,
		TopP:        r.TopP,
		TopK:        r.TopK,
		Messages:    make([]Message, 0, len(r.Input)+1),
	}
	if r.MaxOutputTokens > 0 {
		u.MaxTokens = r.MaxOutputTokens
		ensureResponsesExtras(&u).UsedMaxTokenField = "max_output_tokens"
	} else if r.MaxTokens > 0 {
		u.MaxTokens = r.MaxTokens
		ensureResponsesExtras(&u).UsedMaxTokenField = "max_tokens"
	}
	if r.ResponseFormat != nil {
		switch r.ResponseFormat.Type {
		case "json_object":
			u.Output = &OutputSpec{Mode: OutputModeJSONObject}
		case "text":
			u.Output = &OutputSpec{Mode: OutputModeText}
		}
	}
	if r.Reasoning != nil {
		if r.Reasoning.Effort != "" {
			u.Effort = llm.Effort(r.Reasoning.Effort)
		}
		if r.Reasoning.Summary != "" {
			ensureResponsesExtras(&u).ReasoningSummary = r.Reasoning.Summary
		}
	}
	if hint := cacheHintFromPromptCacheRetention(r.PromptCacheRetention); hint != nil {
		u.CacheHint = hint
	}
	if meta, extra := metadataFromOpenAI(r.User, r.Metadata); meta != nil {
		u.Metadata = meta
		if extra != nil {
			ensureResponsesExtras(&u).ExtraMetadata = extra
		}
	} else if extra != nil {
		ensureResponsesExtras(&u).ExtraMetadata = extra
	}
	if r.PromptCacheRetention != "" {
		ensureResponsesExtras(&u).PromptCacheRetention = r.PromptCacheRetention
	}
	if r.PreviousResponseID != "" {
		ensureResponsesExtras(&u).PreviousResponseID = r.PreviousResponseID
	}
	if r.Store || r.ParallelToolCalls {
		extras := ensureResponsesExtras(&u)
		extras.Store = r.Store
		extras.ParallelToolCalls = r.ParallelToolCalls
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
		useInstructions := true
		ensureResponsesExtras(&u).UseInstructions = &useInstructions
		u.Messages = append(u.Messages, Message{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: r.Instructions}}})
	} else {
		useInstructions := false
		ensureResponsesExtras(&u).UseInstructions = &useInstructions
	}

	var assistantTurn responsesAssistantTurn
	flushAssistantTurn := func() {
		if assistantTurn.empty() {
			return
		}
		u.Messages = append(u.Messages, Message{Role: RoleAssistant, Parts: assistantTurn.parts})
		assistantTurn.parts = nil
	}

	for _, in := range r.Input {
		switch {
		case in.Role == string(msg.RoleDeveloper):
			flushAssistantTurn()
			u.Messages = append(u.Messages, Message{Role: RoleDeveloper, Parts: []Part{{Type: PartTypeText, Text: in.Content}}})
		case in.Role == string(msg.RoleUser):
			flushAssistantTurn()
			u.Messages = append(u.Messages, Message{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: in.Content}}})
		case in.Role == string(msg.RoleAssistant):
			if in.Content != "" {
				assistantTurn.parts = append(assistantTurn.parts, Part{Type: PartTypeText, Text: in.Content})
			}
		case in.Type == "function_call":
			var args map[string]any
			if in.Arguments != "" {
				_ = json.Unmarshal([]byte(in.Arguments), &args)
			}
			assistantTurn.parts = append(assistantTurn.parts, Part{Type: PartTypeToolCall, ToolCall: &ToolCall{ID: in.CallID, Name: in.Name, Args: args}})
		case in.Type == "function_call_output":
			flushAssistantTurn()
			u.Messages = append(u.Messages, Message{Role: RoleTool, Parts: []Part{{Type: PartTypeToolResult, ToolResult: &ToolResult{ToolCallID: in.CallID, ToolOutput: in.Output}}}})
		}
	}
	flushAssistantTurn()

	if err := u.Validate(); err != nil {
		return Request{}, err
	}
	return u, nil
}

type ResponsesOption func(*responsesOptions)

type responsesOptions struct{}

func consumeResponsesInstruction(messages []Message, useInstructions bool) (string, []Message, error) {
	if len(messages) == 0 {
		return "", messages, nil
	}
	if Role(messages[0].Role) == RoleSystem {
		if !useInstructions {
			return "", nil, fmt.Errorf("responses request cannot project system messages when UseInstructions=false")
		}
		if !isTextOnlyMessage(messages[0]) {
			return "", nil, fmt.Errorf("responses request requires leading system message to be text-only")
		}
		for i := 1; i < len(messages); i++ {
			if Role(messages[i].Role) == RoleSystem {
				return "", nil, fmt.Errorf("responses request cannot project multiple system messages")
			}
		}
		return partsText(messages[0].Parts), messages[1:], nil
	}
	for _, m := range messages {
		if Role(m.Role) == RoleSystem {
			if !useInstructions {
				return "", nil, fmt.Errorf("responses request cannot project system messages when UseInstructions=false")
			}
			return "", nil, fmt.Errorf("responses request requires system message to be first")
		}
	}
	return "", messages, nil
}

func buildResponsesAssistantInputs(m Message) ([]responses.Input, error) {
	inputs := make([]responses.Input, 0, len(m.Parts)+1)
	var text strings.Builder
	seenToolCall := false

	for _, p := range m.Parts {
		switch {
		case p.Native != nil:
			return nil, fmt.Errorf("responses assistant message does not support native parts")
		case p.Type == PartTypeText:
			if seenToolCall && p.Text != "" {
				return nil, fmt.Errorf("responses assistant message cannot contain text after tool calls")
			}
			text.WriteString(p.Text)
		case p.Type == PartTypeToolCall:
			if p.ToolCall == nil {
				return nil, fmt.Errorf("responses assistant message has invalid tool call part")
			}
			seenToolCall = true
			argRaw, _ := json.Marshal(p.ToolCall.Args)
			inputs = append(inputs, responses.Input{
				Type:      "function_call",
				CallID:    p.ToolCall.ID,
				Name:      p.ToolCall.Name,
				Arguments: string(argRaw),
			})
		case p.Type == PartTypeThinking:
			return nil, fmt.Errorf("responses assistant message does not support thinking parts")
		case p.Type == PartTypeToolResult:
			return nil, fmt.Errorf("responses assistant message does not support tool result parts")
		default:
			return nil, fmt.Errorf("responses assistant message does not support part type %q", p.Type)
		}
	}

	if text.Len() > 0 {
		inputs = append([]responses.Input{{Role: string(msg.RoleAssistant), Content: text.String()}}, inputs...)
	}
	return inputs, nil
}

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

func isTextOnlyMessage(m Message) bool {
	if len(m.Parts) == 0 {
		return false
	}
	for _, p := range m.Parts {
		if p.Type != PartTypeText {
			return false
		}
	}
	return true
}

type responsesAssistantTurn struct {
	parts []Part
}

func (t responsesAssistantTurn) empty() bool {
	return len(t.parts) == 0
}
