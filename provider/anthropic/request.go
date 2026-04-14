package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/sortmap"
)

// Request types for Anthropic API

type ThinkingConfig struct {
	Type         string `json:"type"` // "enabled", "adaptive", or "disabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type SystemBlocks []*TextBlock

type Request struct {
	Model        string           `json:"model"`
	MaxTokens    int              `json:"max_tokens"`
	Stream       bool             `json:"stream"`
	System       SystemBlocks     `json:"system,omitempty"`
	Messages     []Message        `json:"messages"`
	Tools        []ToolDefinition `json:"tools,omitempty"`
	ToolChoice   any              `json:"tool_choice,omitempty"`
	Thinking     *ThinkingConfig  `json:"thinking,omitempty"`
	Metadata     *metadata        `json:"metadata,omitempty"`
	CacheControl *CacheControl    `json:"cache_control,omitempty"`
	TopK         int              `json:"top_k,omitempty"`
	TopP         float64          `json:"top_p,omitempty"`
	OutputConfig *outputConfig    `json:"output_config,omitempty"`
}

// ControlParams returns the request as a map[string]any with verbose fields
// (system, messages, full tool definitions) stripped out. What remains are the
// control knobs actually sent to the Anthropic API — useful for observability.
func (r Request) ControlParams() map[string]any {
	b, err := json.Marshal(r)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	delete(m, "system")
	delete(m, "messages")
	delete(m, "tools")
	delete(m, "metadata")
	return m
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

type ToolDefinition struct {
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	InputSchema  any           `json:"input_schema"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// RequestOptions contains options for building an Anthropic Request.
type RequestOptions struct {
	SystemBlocks SystemBlocks
	UserID       string
	LLMRequest   llm.Request
}

const (
	// thinkingBudgetLow is the minimum thinking budget (tokens).
	thinkingBudgetLow = 1024
	// thinkingBudgetHigh is the maximum thinking budget — must be < max_tokens (32000).
	thinkingBudgetHigh = 31999
)

// isAdaptiveThinkingSupported returns true if the model supports adaptive ThinkingConfig (Sonnet 4.6 / Opus 4.6).
func isAdaptiveThinkingSupported(model string) bool {
	return strings.Contains(model, "claude-sonnet-4-6") ||
		strings.Contains(model, "claude-opus-4-6")
}

// isMaxEffortSupported returns true if the model supports max effort (Sonnet 4.6, Opus 4.6).
func isMaxEffortSupported(model string) bool {
	return strings.Contains(model, "claude-sonnet-4-6") ||
		strings.Contains(model, "claude-opus-4-6")
}

// isEffortSupported returns true if the model supports output effort.
// Effort is supported on Sonnet 4.6, Opus 4.5, and Opus 4.6.
// Note: Sonnet 4.5 does NOT support effort.
func isEffortSupported(model string) bool {
	return strings.Contains(model, "claude-sonnet-4-6") ||
		strings.Contains(model, "claude-opus-4-5") ||
		strings.Contains(model, "claude-opus-4-6")
}

// hasPerMessageCacheHints returns true if any message carries an enabled CacheHint.
func hasPerMessageCacheHints(msgs llm.Messages) bool {
	for _, m := range msgs {
		if h := m.CacheHint; h != nil && h.Enabled {
			return true
		}
	}
	return false
}

// BuildRequestBytes builds a JSON Request body for the Anthropic API.
func BuildRequestBytes(reqOpts RequestOptions) ([]byte, error) {
	req, err := BuildRequest(reqOpts)
	if err != nil {
		return nil, err
	}

	return json.MarshalIndent(req, "", "  ")
}

// BuildRequest builds a JSON Request body for the Anthropic API.
func BuildRequest(reqOpts RequestOptions) (Request, error) {
	llmRequest := reqOpts.LLMRequest
	maxTokens := llmRequest.MaxTokens
	if maxTokens == 0 {
		maxTokens = 32000
	}

	req := Request{
		Model:     llmRequest.Model,
		MaxTokens: maxTokens,
		Stream:    true,
		System:    reqOpts.SystemBlocks,
		Messages:  []Message{},
	}

	// Generation parameters
	if llmRequest.TopK > 0 {
		req.TopK = llmRequest.TopK
	}
	if llmRequest.TopP > 0 {
		req.TopP = llmRequest.TopP
	}
	// Output config: format
	if llmRequest.OutputFormat == llm.OutputFormatJSON {
		if req.OutputConfig == nil {
			req.OutputConfig = &outputConfig{}
		}
		req.OutputConfig.Format = &jsonOutputFormat{Type: "json_schema"}
	}

	userSystemBlocks, messages := convertMessages(llmRequest.Messages)
	if len(userSystemBlocks) > 0 {
		req.System = append(req.System, userSystemBlocks...)
	}
	req.Messages = messages
	if req.Messages == nil {
		req.Messages = make([]Message, 0)
	}

	if reqOpts.UserID != "" {
		req.Metadata = &metadata{UserID: reqOpts.UserID}
	}

	for _, t := range llmRequest.Tools {
		req.Tools = append(req.Tools, ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: sortmap.NewSortedMap(t.Parameters),
			// TODO: cache_control?
		})
	}

	// 1. Thinking mode — independent of effort
	switch {
	case llmRequest.Thinking.IsOff():
		req.Thinking = &ThinkingConfig{Type: "disabled"}
	case isAdaptiveThinkingSupported(req.Model):
		req.Thinking = &ThinkingConfig{Type: "adaptive"}
	default:
		// Older models: enabled with budget derived from Effort
		budget := thinkingBudgetHigh // generous default when EffortUnspecified
		if b, ok := llmRequest.Effort.ToBudget(thinkingBudgetLow, thinkingBudgetHigh); ok {
			budget = b
		}
		req.Thinking = &ThinkingConfig{Type: "enabled", BudgetTokens: budget}
	}

	// Anthropic API restriction: thinking cannot be combined with forced
	// tool_choice. Silently downgrade to auto so the request succeeds.
	if req.Thinking != nil && req.Thinking.Type != "disabled" {
		switch llmRequest.ToolChoice.(type) {
		case llm.ToolChoiceRequired, llm.ToolChoiceTool:
			llmRequest.ToolChoice = llm.ToolChoiceAuto{}
		}
	}

	// 2. Output effort — independent of thinking mode, set on supported models
	if isEffortSupported(req.Model) {
		effort := llmRequest.Effort
		if effort == llm.EffortMax && !isMaxEffortSupported(req.Model) {
			effort = llm.EffortHigh // silent downgrade
		}
		if effort.IsEmpty() {
			effort = llm.EffortMedium // default
		}
		if req.OutputConfig == nil {
			req.OutputConfig = &outputConfig{}
		}
		req.OutputConfig.Effort = string(effort)
	}

	if len(llmRequest.Tools) > 0 {
		switch tc := llmRequest.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			req.ToolChoice = map[string]string{"type": "auto"}
		case llm.ToolChoiceRequired:
			req.ToolChoice = map[string]string{"type": "any"}
		case llm.ToolChoiceNone:
		case llm.ToolChoiceTool:
			req.ToolChoice = map[string]any{"type": "tool", "name": tc.Name}
		}
	}

	// Apply top-level (automatic) cache hint when no per-message hints exist.
	// This emits cache_control at the Request level, instructing Anthropic to
	// automatically place a breakpoint at the last cacheable content block.
	if llmRequest.CacheHint != nil && llmRequest.CacheHint.Enabled && !hasPerMessageCacheHints(llmRequest.Messages) {
		req.CacheControl = buildCacheControl(llmRequest.CacheHint)
	}

	return req, nil
}
