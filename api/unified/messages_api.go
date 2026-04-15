package unified

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/sortmap"
	"github.com/codewandler/llm/usage"
)

// RequestToMessages converts a canonical unified request to an Anthropic
// Messages wire request.
func RequestToMessages(r Request, opts ...MessagesOption) (*messages.Request, error) {
	mopts := &messagesOptions{modelCaps: DefaultAnthropicModelCaps}
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

func messageToMessages(m Message) (*messages.Message, error) {
	wire := &messages.Message{}
	switch Role(m.Role) {
	case RoleUser:
		wire.Role = string(msg.RoleUser)
	case RoleAssistant:
		wire.Role = string(msg.RoleAssistant)
	case RoleTool:
		// tool results are represented as user content blocks in Anthropic wire
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

func partsText(parts []Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == PartTypeText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func toMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	raw, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

// EventFromMessages converts a messages native parser event into unified StreamEvent.
// Returns ignored=true for explicit no-op events.
func EventFromMessages(ev any) (StreamEvent, bool, error) {
	switch e := ev.(type) {
	case *messages.MessageStartEvent:
		// Emit Started + partial Usage (input tokens available at message_start).
		// Output tokens and stop reason come later in MessageDeltaEvent.
		tokens := usage.TokenItems{
			{Kind: usage.KindInput, Count: e.Message.Usage.InputTokens},
			{Kind: usage.KindCacheWrite, Count: e.Message.Usage.CacheCreationInputTokens},
			{Kind: usage.KindCacheRead, Count: e.Message.Usage.CacheReadInputTokens},
		}.NonZero()
		return StreamEvent{
			Type:    StreamEventStarted,
			Started: &Started{RequestID: e.Message.ID, Model: e.Message.Model},
			Usage:   &Usage{Tokens: tokens},
		}, false, nil

	case *messages.ContentBlockDeltaEvent:
		idx := uint32(e.Index)
		switch e.Delta.Type {
		case messages.DeltaTypeText:
			return StreamEvent{Type: StreamEventDelta, Delta: &Delta{Kind: llm.DeltaKindText, Index: &idx, Text: e.Delta.Text}}, false, nil
		case messages.DeltaTypeThinking:
			return StreamEvent{Type: StreamEventDelta, Delta: &Delta{Kind: llm.DeltaKindThinking, Index: &idx, Thinking: e.Delta.Thinking}}, false, nil
		case messages.DeltaTypeInputJSON:
			return StreamEvent{Type: StreamEventDelta, Delta: &Delta{Kind: llm.DeltaKindTool, Index: &idx, ToolArgs: e.Delta.PartialJSON}}, false, nil
		case messages.DeltaTypeSignature:
			return StreamEvent{}, true, nil
		default:
			return StreamEvent{Type: StreamEventUnknown, Extras: EventExtras{RawEventName: messages.EventContentBlockDelta}}, false, nil
		}

	case *messages.TextCompleteEvent:
		return StreamEvent{Type: StreamEventContent, Content: &ContentPart{Part: msg.Text(e.Text), Index: e.Index}}, false, nil
	case *messages.ThinkingCompleteEvent:
		return StreamEvent{Type: StreamEventContent, Content: &ContentPart{Part: msg.Thinking(e.Thinking, e.Signature), Index: e.Index}}, false, nil
	case *messages.ToolCompleteEvent:
		return StreamEvent{Type: StreamEventToolCall, ToolCall: &ToolCall{ID: e.ID, Name: e.Name, Args: e.Args}}, false, nil

	case *messages.MessageDeltaEvent:
		// Emit Completed + partial Usage (output tokens available at message_delta).
		// Input tokens were emitted earlier from MessageStartEvent.
		tokens := usage.TokenItems{
			{Kind: usage.KindOutput, Count: e.Usage.OutputTokens},
		}.NonZero()
		return StreamEvent{
			Type:      StreamEventCompleted,
			Completed: &Completed{StopReason: mapMessagesStopReason(e.Delta.StopReason)},
			Usage:     &Usage{Tokens: tokens},
		}, false, nil
	case *messages.StreamErrorEvent:
		return StreamEvent{Type: StreamEventError, Error: &StreamError{Err: e}}, false, nil

	case *messages.PingEvent, *messages.ContentBlockStartEvent, *messages.ContentBlockStopEvent, *messages.MessageStopEvent:
		return StreamEvent{}, true, nil

	default:
		return StreamEvent{Type: StreamEventUnknown}, false, nil
	}
}

// mapMessagesStopReason maps an Anthropic Messages API stop_reason string to
// the canonical llm.StopReason.
func mapMessagesStopReason(s string) llm.StopReason {
	switch s {
	case "end_turn":
		return llm.StopReasonEndTurn
	case "tool_use":
		return llm.StopReasonToolUse
	case "max_tokens":
		return llm.StopReasonMaxTokens
	default:
		return llm.StopReason(s)
	}
}

// MessagesOption configures RequestToMessages conversion.
type MessagesOption func(*messagesOptions)

type messagesOptions struct {
	modelCaps ModelCapsFunc
}

// WithModelCaps injects a custom model capability resolver into RequestToMessages.
// Use this when you have a live model registry that is more accurate than
// the built-in string-matching DefaultAnthropicModelCaps.
func WithModelCaps(fn ModelCapsFunc) MessagesOption {
	return func(o *messagesOptions) { o.modelCaps = fn }
}
