package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/sortmap"
)

// Request types for Anthropic API

type thinking struct {
	Type         string `json:"type"`           // "enabled", "adaptive", or "disabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type request struct {
	Model        string           `json:"model"`
	MaxTokens    int              `json:"max_tokens"`
	Stream       bool             `json:"stream"`
	System       any              `json:"system,omitempty"`
	Messages     []messagePayload `json:"messages"`
	Tools        []toolPayload    `json:"tools,omitempty"`
	ToolChoice   any              `json:"tool_choice,omitempty"`
	Thinking     *thinking        `json:"thinking,omitempty"`
	Metadata     *metadata        `json:"metadata,omitempty"`
	CacheControl *CacheControl    `json:"cache_control,omitempty"`
	TopK         int              `json:"top_k,omitempty"`
	TopP         float64          `json:"top_p,omitempty"`
	OutputConfig *outputConfig    `json:"output_config,omitempty"`
}

type outputConfig struct {
	Format *jsonOutputFormat `json:"format,omitempty"`
	Effort string            `json:"effort,omitempty"`
}

type jsonOutputFormat struct {
	Type   string `json:"type"`
	Schema any    `json:"schema,omitempty"`
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
	CacheControl *CacheControl `json:"cache_control,omitempty"`
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
	CacheControl *CacheControl  `json:"cache_control,omitempty"`
}

// toolUseBlock is a specialized content block for tool_use that always includes the input field.
// Anthropic API requires the "input" field to be present in tool_use blocks, even if empty.
type toolUseBlock struct {
	Type         string         `json:"type"`
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Input        map[string]any `json:"input"` // No omitempty - must always be present
	CacheControl *CacheControl  `json:"cache_control,omitempty"`
}

type toolPayload struct {
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	InputSchema  any           `json:"input_schema"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// CacheControl is the Anthropic API wire type for cache breakpoints.
type CacheControl struct {
	Type string `json:"type"`          // always "ephemeral"
	TTL  string `json:"ttl,omitempty"` // "1h" for extended TTL; omit for default 5m
}

// RequestOptions contains options for building an Anthropic request.
type RequestOptions struct {
	Model         string
	MaxTokens     int
	SystemBlocks  []SystemBlock
	UserID        string
	StreamOptions llm.Request
}

// isAdaptiveThinkingSupported returns true if the model supports adaptive thinking (Sonnet 4.6 / Opus 4.6).
func isAdaptiveThinkingSupported(model string) bool {
	return strings.Contains(model, "claude-sonnet-4-6") ||
		strings.Contains(model, "claude-opus-4-6")
}

// isMaxEffortSupported returns true if the model supports max effort (Opus 4.6 only).
func isMaxEffortSupported(model string) bool {
	return strings.Contains(model, "claude-opus-4-6")
}

// isEffortSupported returns true if the model supports output effort.
// Effort is supported on Sonnet 4.6, Opus 4.5, and Opus 4.6.
// Note: Sonnet 4.5 does NOT support effort.
func isEffortSupported(model string) bool {
	return strings.Contains(model, "claude-sonnet-4-6") ||
		strings.Contains(model, "claude-opus-4-5") ||
		strings.Contains(model, "claude-opus-4-6")
}

// contentBlockFromLLM converts an llm.ContentBlock to the appropriate Anthropic
// wire representation: a plain contentBlock for text, or a thinking map for
// thinking blocks (which require "type", "thinking", and "signature" fields).
func contentBlockFromLLM(cb llm.ContentBlock) any {
	switch cb.Kind {
	case llm.ContentBlockKindThinking:
		return map[string]string{
			"type":      "thinking",
			"thinking":  cb.Text,
			"signature": cb.Signature,
		}
	default:
		return contentBlock{Type: "text", Text: cb.Text}
	}
}

// buildCacheControl converts a CacheHint to the Anthropic wire type.
// Returns nil if hint is nil or not enabled.
func buildCacheControl(h *llm.CacheHint) *CacheControl {
	if h == nil || !h.Enabled {
		return nil
	}
	cc := &CacheControl{Type: "ephemeral"}
	if h.TTL == "1h" {
		cc.TTL = "1h"
	}
	return cc
}

// hasPerMessageCacheHints returns true if any message carries an enabled CacheHint.
func hasPerMessageCacheHints(msgs llm.Messages) bool {
	for _, msg := range msgs {
		if h := msg.CacheHint(); h != nil && h.Enabled {
			return true
		}
	}
	return false
}

// BuildRequest builds a JSON request body for the Anthropic API.
func BuildRequest(reqOpts RequestOptions) ([]byte, error) {
	opts := reqOpts.StreamOptions
	// Use MaxTokens from RequestOptions, fall back to StreamOptions, then default to 32000.
	maxTokens := reqOpts.MaxTokens
	if maxTokens == 0 {
		maxTokens = opts.MaxTokens
	}
	if maxTokens == 0 {
		maxTokens = 32000
	}

	r := request{Model: reqOpts.Model, MaxTokens: maxTokens, Stream: true}

	// Generation parameters
	if opts.TopK > 0 {
		r.TopK = opts.TopK
	}
	if opts.TopP > 0 {
		r.TopP = opts.TopP
	}
	// Output config: format and/or effort
	if opts.OutputFormat == llm.OutputFormatJSON || opts.OutputEffort != "" {
		if r.OutputConfig == nil {
			r.OutputConfig = &outputConfig{}
		}
		if opts.OutputFormat == llm.OutputFormatJSON {
			r.OutputConfig.Format = &jsonOutputFormat{Type: "json_schema"}
		}
	}

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
		r.Tools = append(r.Tools, toolPayload{Name: t.Name, Description: t.Description, InputSchema: sortmap.NewSortedMap(t.Parameters)})
	}

	// Wire thinking. Incompatible with forced tool_choice — downgrade to auto.
	// Sonnet 4.6 / Opus 4.6 use adaptive thinking by default (empty = let model decide).
	// ThinkingEffort "none" explicitly disables thinking.
	// Other ThinkingEffort values use budget_tokens.
	if opts.ThinkingEffort == llm.ThinkingEffortNone {
		// User explicitly disabled thinking
		r.Thinking = &thinking{Type: "disabled"}
	} else if isAdaptiveThinkingSupported(reqOpts.Model) {
		// Sonnet 4.6 / Opus 4.6: use adaptive by default (let model decide when to think)
		// Or if user set ThinkingEffort, still use adaptive (effort controls depth)
		r.Thinking = &thinking{Type: "adaptive"}
	} else if opts.ThinkingEffort != "" {
		// Older models with explicit ThinkingEffort: use enabled + budget_tokens
		budgetTokens := 5000
		switch opts.ThinkingEffort {
		case llm.ThinkingEffortLow:
			budgetTokens = 1024
		case llm.ThinkingEffortMedium:
			budgetTokens = 5000
		case llm.ThinkingEffortHigh:
			budgetTokens = 16000
		}
		r.Thinking = &thinking{Type: "enabled", BudgetTokens: budgetTokens}
	} else {
		// Haiku / older models: default to enabled thinking with high budget_tokens (like Claude Code)
		r.Thinking = &thinking{Type: "enabled", BudgetTokens: 31999}
	}
	if _, isForced := opts.ToolChoice.(llm.ToolChoiceTool); isForced {
		opts.ToolChoice = llm.ToolChoiceAuto{}
	}

	// Wire output effort (Anthropic only). Default to medium effort.
	// Only set max effort on Opus 4.6. Effort is only supported on Sonnet 4.6, Opus 4.5, Opus 4.6.
	if isEffortSupported(reqOpts.Model) {
		effort := opts.OutputEffort
		if effort == "" {
			effort = llm.OutputEffortMedium
		}
		effortStr := string(effort)
		if effort == llm.OutputEffortMax && !isMaxEffortSupported(reqOpts.Model) {
			// Skip max effort on unsupported models; API would reject it anyway
			effortStr = ""
		}
		if effortStr != "" {
			if r.OutputConfig == nil {
				r.OutputConfig = &outputConfig{}
			}
			r.OutputConfig.Effort = effortStr
		}
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
		case llm.SystemMessage:
			// Handled by system blocks above
		case llm.UserMessage:
			block := contentBlock{Type: "text", Text: m.Content(), CacheControl: buildCacheControl(m.CacheHint())}
			r.Messages = append(r.Messages, messagePayload{Role: "user", Content: []contentBlock{block}})
		case llm.AssistantMessage:
			if len(m.ToolCalls()) == 0 {
				// Block-aware path: emit text+thinking in original index order.
				// Every AssistantMessage now has ContentBlocks populated; apply cache hint to last block.
				var blocks []any
				for _, cb := range m.ContentBlocks() {
					blocks = append(blocks, contentBlockFromLLM(cb))
				}
				if len(blocks) > 0 {
					// Apply cache hint to the last content block if present.
					if m.CacheHint() != nil {
						if last, ok := blocks[len(blocks)-1].(contentBlock); ok {
							last.CacheControl = buildCacheControl(m.CacheHint())
							blocks[len(blocks)-1] = last
						}
					}
				}
				r.Messages = append(r.Messages, messagePayload{Role: "assistant", Content: blocks})
				continue
			}
			var blocks []any
			// Block-aware path: emit text+thinking in original index order, then tool_use follows.
			for _, cb := range m.ContentBlocks() {
				blocks = append(blocks, contentBlockFromLLM(cb))
			}
			for j, tc := range m.ToolCalls() {
				tub := toolUseBlock{Type: "tool_use", ID: tc.ToolCallID(), Name: tc.ToolName(), Input: ensureInputMap(tc.ToolArgs())}
				if m.CacheHint() != nil && m.CacheHint().Enabled && j == len(m.ToolCalls())-1 {
					tub.CacheControl = buildCacheControl(m.CacheHint())
				}
				blocks = append(blocks, tub)
			}
			r.Messages = append(r.Messages, messagePayload{Role: "assistant", Content: blocks})
		case llm.ToolMessage:
			var results []contentBlock
			prevAssistant := FindPrecedingAssistant(opts.Messages, i)
			toolIdx := 0
			startI := i
			for ; i < len(opts.Messages); i++ {
				tr, ok := opts.Messages[i].(llm.ToolMessage)
				if !ok {
					break
				}
				toolUseID := tr.ToolCallID()
				if toolUseID == "" && prevAssistant != nil && toolIdx < len(prevAssistant.ToolCalls()) {
					toolUseID = prevAssistant.ToolCalls()[toolIdx].ToolCallID()
				}
				results = append(results, contentBlock{Type: "tool_result", ToolUseID: toolUseID, Content: tr.ToolOutput(), IsError: tr.IsError()})
				toolIdx++
			}
			if i > startI {
				if lastTR, ok := opts.Messages[i-1].(llm.ToolMessage); ok {
					if cc := buildCacheControl(lastTR.CacheHint()); cc != nil {
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
func FindPrecedingAssistant(messages llm.Messages, toolIdx int) llm.AssistantMessage {
	for j := toolIdx - 1; j >= 0; j-- {
		if am, ok := messages[j].(llm.AssistantMessage); ok {
			return am
		}
	}
	return nil
}

// CollectSystemBlocks extracts all System from messages and returns them as SystemBlocks.
func CollectSystemBlocks(messages llm.Messages) []SystemBlock {
	var blocks []SystemBlock
	for _, msg := range messages {
		if sm, ok := msg.(llm.SystemMessage); ok {
			if strings.TrimSpace(sm.Content()) != "" {
				blocks = append(blocks, SystemBlock{
					Type:         "text",
					Text:         sm.Content(),
					CacheControl: buildCacheControl(sm.CacheHint()),
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
