package unified

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/sortmap"
)

// BuildMessagesRequest converts a canonical unified request to an Anthropic
// Messages wire request.
func BuildMessagesRequest(r Request, opts ...MessagesOption) (*messages.Request, error) {
	mopts := &messagesOptions{modelCaps: DefaultAnthropicMessagesModelCaps}
	for _, o := range opts {
		o(mopts)
	}
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("validate unified request: %w", err)
	}

	maxTokens := r.MaxTokens
	if maxTokens == 0 {
		maxTokens = 32000
	}

	out := &messages.Request{
		Model:     r.Model,
		MaxTokens: maxTokens,
		Stream:    true,
		Messages:  make([]messages.Message, 0, len(r.Messages)),
	}

	if r.TopK > 0 {
		out.TopK = r.TopK
	}
	if r.TopP > 0 {
		out.TopP = r.TopP
	}

	if r.OutputFormat == llm.OutputFormatJSON {
		out.OutputConfig = &messages.OutputConfig{Format: &messages.JSONOutputFormat{Type: "json_schema"}}
	}

	if r.UserID != "" {
		out.Metadata = &messages.Metadata{UserID: r.UserID}
	}

	for _, t := range r.Tools {
		out.Tools = append(out.Tools, messages.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: sortmap.NewSortedMap(t.Parameters),
		})
	}

	if len(r.Tools) > 0 {
		switch tc := r.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			out.ToolChoice = map[string]string{"type": "auto"}
		case llm.ToolChoiceRequired:
			out.ToolChoice = map[string]string{"type": "any"}
		case llm.ToolChoiceNone:
			// omit
		case llm.ToolChoiceTool:
			out.ToolChoice = map[string]any{"type": "tool", "name": tc.Name}
		default:
			return nil, fmt.Errorf("unsupported tool choice type %T", r.ToolChoice)
		}
	}

	caps := mopts.modelCaps(r.Model)
	if r.Thinking == llm.ThinkingOff {
		out.Thinking = &messages.ThinkingConfig{Type: "disabled"}
	} else if caps.SupportsAdaptiveThinking {
		out.Thinking = &messages.ThinkingConfig{Type: "adaptive", Display: caps.DefaultThinkingDisplay}
	} else {
		out.Thinking = &messages.ThinkingConfig{Type: "enabled", BudgetTokens: effortToBudget(r.Effort), Display: caps.DefaultThinkingDisplay}
	}

	if out.Thinking != nil && out.Thinking.Type != "disabled" {
		switch r.ToolChoice.(type) {
		case llm.ToolChoiceRequired, llm.ToolChoiceTool:
			out.ToolChoice = map[string]string{"type": "auto"}
		}
	}
	if caps.SupportsEffort {
		if out.OutputConfig == nil {
			out.OutputConfig = &messages.OutputConfig{}
		}
		e := r.Effort
		if e.IsEmpty() {
			e = llm.EffortMedium
		}
		if e == llm.EffortMax && !caps.SupportsMaxEffort {
			e = llm.EffortHigh
		}
		out.OutputConfig.Effort = string(e)
	}

	for _, m := range r.Messages {
		if Role(m.Role) == RoleSystem {
			text := strings.TrimSpace(partsText(m.Parts))
			if text != "" {
				out.System = append(out.System, &messages.TextBlock{
					Type:         messages.BlockTypeText,
					Text:         text,
					CacheControl: cacheHintToMessages(m.CacheHint),
				})
			}
			continue
		}

		wire, err := messageToMessages(m)
		if err != nil {
			return nil, err
		}
		out.Messages = append(out.Messages, *wire)
	}

	if out.Messages == nil {
		out.Messages = make([]messages.Message, 0)
	}

	if r.CacheHint != nil && r.CacheHint.Enabled && !hasPerMessageCacheHints(r.Messages) {
		out.CacheControl = cacheHintToMessages(r.CacheHint)
	}

	return out, nil
}

// RequestFromMessages converts an Anthropic Messages wire request to unified.
func RequestFromMessages(r messages.Request) (Request, error) {
	u := Request{
		Model:        r.Model,
		MaxTokens:    r.MaxTokens,
		TopK:         r.TopK,
		TopP:         r.TopP,
		OutputFormat: llm.OutputFormatText,
		Messages:     make([]Message, 0, len(r.Messages)+len(r.System)),
	}
	if r.OutputConfig != nil && r.OutputConfig.Format != nil && r.OutputConfig.Format.Type == "json_schema" {
		u.OutputFormat = llm.OutputFormatJSON
	}
	if r.Metadata != nil {
		u.UserID = r.Metadata.UserID
	}
	if r.CacheControl != nil {
		u.CacheHint = cacheHintFromMessages(r.CacheControl)
	}
	if r.Thinking != nil {
		switch r.Thinking.Type {
		case "disabled":
			u.Thinking = llm.ThinkingOff
		default:
			u.Thinking = llm.ThinkingOn
		}
	}
	for _, t := range r.Tools {
		u.Tools = append(u.Tools, Tool{Name: t.Name, Description: t.Description, Parameters: toMap(t.InputSchema)})
	}
	for _, s := range r.System {
		if s == nil {
			continue
		}
		u.Messages = append(u.Messages, Message{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: s.Text}}, CacheHint: cacheHintFromMessages(s.CacheControl)})
	}
	for _, m := range r.Messages {
		x, err := messageFromMessages(m)
		if err != nil {
			return Request{}, err
		}
		u.Messages = append(u.Messages, x)
	}
	if err := u.Validate(); err != nil {
		return Request{}, err
	}
	return u, nil
}

type MessagesOption func(*messagesOptions)

type messagesOptions struct {
	modelCaps ModelCapsFunc
}

// WithMessagesModelCaps injects a custom model capability resolver into
// BuildMessagesRequest.
func WithMessagesModelCaps(fn ModelCapsFunc) MessagesOption {
	return func(o *messagesOptions) { o.modelCaps = fn }
}

func messageToMessages(m Message) (*messages.Message, error) {
	wire := &messages.Message{}
	switch Role(m.Role) {
	case RoleUser:
		wire.Role = string(msg.RoleUser)
	case RoleAssistant:
		wire.Role = string(msg.RoleAssistant)
	case RoleTool:
		wire.Role = string(msg.RoleUser)
	case RoleDeveloper:
		wire.Role = string(msg.RoleUser)
	default:
		wire.Role = string(m.Role)
	}

	blocks := make([]any, 0, len(m.Parts))
	for _, p := range m.Parts {
		switch p.Type {
		case PartTypeText:
			blocks = append(blocks, &messages.TextBlock{Type: messages.BlockTypeText, Text: p.Text})
		case PartTypeThinking:
			if p.Thinking != nil {
				blocks = append(blocks, &messages.ThinkingBlock{Type: messages.BlockTypeThinking, Thinking: p.Thinking.Text, Signature: p.Thinking.Signature})
			}
		case PartTypeToolCall:
			if p.ToolCall != nil {
				argRaw, _ := json.Marshal(p.ToolCall.Args)
				blocks = append(blocks, &messages.ToolUseBlock{Type: messages.BlockTypeToolUse, ID: p.ToolCall.ID, Name: p.ToolCall.Name, Input: argRaw})
			}
		case PartTypeToolResult:
			if p.ToolResult != nil {
				blocks = append(blocks, &messages.ToolResultBlock{Type: "tool_result", ToolUseID: p.ToolResult.ToolCallID, Content: p.ToolResult.ToolOutput, IsError: p.ToolResult.IsError})
			}
		}
	}
	if len(blocks) == 0 {
		blocks = append(blocks, &messages.TextBlock{Type: messages.BlockTypeText, Text: ""})
	}
	wire.Content = blocks

	if h := cacheHintToMessages(m.CacheHint); h != nil {
		if len(blocks) > 0 {
			switch tb := blocks[len(blocks)-1].(type) {
			case *messages.TextBlock:
				tb.CacheControl = h
			case *messages.ToolUseBlock:
				tb.CacheControl = h
			case *messages.ToolResultBlock:
				tb.CacheControl = h
			case *messages.ThinkingBlock:
				tb.CacheControl = h
			}
		}
	}

	return wire, nil
}

func messageFromMessages(m messages.Message) (Message, error) {
	um := Message{Parts: make([]Part, 0)}
	switch m.Role {
	case string(msg.RoleUser):
		um.Role = RoleUser
	case string(msg.RoleAssistant):
		um.Role = RoleAssistant
	default:
		um.Role = Role(m.Role)
	}

	switch c := m.Content.(type) {
	case string:
		if c != "" {
			um.Parts = append(um.Parts, Part{Type: PartTypeText, Text: c})
		}
	case []any:
		for _, item := range c {
			part, hint, err := partFromMessagesRaw(item)
			if err != nil {
				return Message{}, err
			}
			if part != nil {
				um.Parts = append(um.Parts, *part)
			}
			if hint != nil {
				um.CacheHint = hint
			}
		}
	}

	if len(um.Parts) == 0 {
		um.Parts = []Part{{Type: PartTypeText, Text: ""}}
	}
	return um, nil
}

func partFromMessagesRaw(v any) (*Part, *msg.CacheHint, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, nil, nil
	}
	typ, _ := m["type"].(string)
	hint := cacheHintFromRaw(m)
	switch typ {
	case messages.BlockTypeText:
		text, _ := m["text"].(string)
		return &Part{Type: PartTypeText, Text: text}, hint, nil
	case messages.BlockTypeThinking:
		thinking, _ := m["thinking"].(string)
		sig, _ := m["signature"].(string)
		return &Part{Type: PartTypeThinking, Thinking: &ThinkingPart{Text: thinking, Signature: sig}}, hint, nil
	case messages.BlockTypeToolUse:
		id, _ := m["id"].(string)
		name, _ := m["name"].(string)
		args, _ := m["input"].(map[string]any)
		return &Part{Type: PartTypeToolCall, ToolCall: &ToolCall{ID: id, Name: name, Args: args}}, hint, nil
	case "tool_result":
		toolID, _ := m["tool_use_id"].(string)
		content, _ := m["content"].(string)
		isErr, _ := m["is_error"].(bool)
		return &Part{Type: PartTypeToolResult, ToolResult: &ToolResult{ToolCallID: toolID, ToolOutput: content, IsError: isErr}}, hint, nil
	default:
		return &Part{Native: m}, hint, nil
	}
}

func cacheHintToMessages(h *msg.CacheHint) *messages.CacheControl {
	if h == nil || !h.Enabled {
		return nil
	}
	return &messages.CacheControl{Type: "ephemeral"}
}

func cacheHintFromMessages(cc *messages.CacheControl) *msg.CacheHint {
	if cc == nil {
		return nil
	}
	return &msg.CacheHint{Enabled: true, TTL: msg.CacheTTL5m.String()}
}

func cacheHintFromRaw(m map[string]any) *msg.CacheHint {
	raw, ok := m["cache_control"].(map[string]any)
	if !ok {
		return nil
	}
	_, _ = raw["type"].(string)
	return &msg.CacheHint{Enabled: true, TTL: msg.CacheTTL5m.String()}
}

func effortToBudget(e llm.Effort) int {
	if e == llm.EffortUnspecified {
		return 31999
	}
	if b, ok := e.ToBudget(1024, 31999); ok {
		return b
	}
	return 31999
}

func hasPerMessageCacheHints(msgs []Message) bool {
	for _, m := range msgs {
		if m.CacheHint != nil && m.CacheHint.Enabled {
			return true
		}
	}
	return false
}
