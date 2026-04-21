package llm

import (
	"errors"
	"fmt"

	llmtool "github.com/codewandler/llm/tool"
)

// --- Effort ---

// Effort controls how thoroughly the model works on the response.
// Affects thinking depth, response length, and tool call count.
// Universal across all providers.
type Effort string

const (
	// EffortUnspecified is the zero value — provider picks its default.
	EffortUnspecified Effort = ""
	// EffortLow produces fast, cheap, less thorough responses.
	EffortLow Effort = "low"
	// EffortMedium produces balanced responses.
	EffortMedium Effort = "medium"
	// EffortHigh produces thorough, slower responses.
	EffortHigh Effort = "high"
	// EffortMax produces maximum-capability responses.
	// Silently downgrades to EffortHigh on models that don't support it.
	EffortMax Effort = "max"
)

// Valid returns true if the Effort is a known valid value or empty.
func (e Effort) Valid() bool {
	switch e {
	case EffortUnspecified, EffortLow, EffortMedium, EffortHigh, EffortMax:
		return true
	default:
		return false
	}
}

// IsEmpty returns true when no effort has been specified.
func (e Effort) IsEmpty() bool { return e == EffortUnspecified }

// ToBudget maps this effort to a token budget in [low, high].
// Used by providers that need budget_tokens (Anthropic < 4.6, Bedrock).
// EffortMax maps to the same budget as EffortHigh.
// Returns (0, false) for EffortUnspecified.
func (e Effort) ToBudget(low, high int) (int, bool) {
	switch e {
	case EffortLow:
		return low, true
	case EffortMedium:
		return low + (high-low)/2, true
	case EffortHigh, EffortMax:
		return high, true
	default:
		return 0, false
	}
}

// --- ThinkingMode ---

// ThinkingMode controls whether the model uses extended/chain-of-thought reasoning.
// This is a mode selector (on/off/auto), not a depth control — depth is
// controlled by Effort.
type ThinkingMode string

const (
	// ThinkingAuto lets the provider/model decide whether to think.
	ThinkingAuto ThinkingMode = ""
	// ThinkingOn forces extended thinking on.
	ThinkingOn ThinkingMode = "on"
	// ThinkingOff forces extended thinking off.
	ThinkingOff ThinkingMode = "off"
)

// Valid returns true if the ThinkingMode is a known valid value.
func (m ThinkingMode) Valid() bool {
	switch m {
	case ThinkingAuto, ThinkingOn, ThinkingOff:
		return true
	default:
		return false
	}
}

// IsOff returns true when thinking is explicitly disabled.
func (m ThinkingMode) IsOff() bool { return m == ThinkingOff }

// IsOn returns true when thinking is explicitly enabled.
func (m ThinkingMode) IsOn() bool { return m == ThinkingOn }

// OutputFormat specifies the desired output format for the model response.
type OutputFormat string

const (
	// OutputFormatText requests plain text output (default for most providers).
	OutputFormatText OutputFormat = "text"
	// OutputFormatJSON requests JSON output. The model will be constrained
	// to output valid JSON. Not all providers support this.
	OutputFormatJSON OutputFormat = "json"
)

// ApiType identifies a wire protocol for LLM API requests.
// Used as a hint on Request.ApiTypeHint and as the resolved value on RequestEvent.ResolvedApiType.
type ApiType string

const (
	// ApiTypeAuto is the zero value. The provider selects the best API.
	ApiTypeAuto ApiType = ""
	// ApiTypeOpenAIChatCompletion selects the OpenAI Chat Completions API (/v1/chat/completions).
	ApiTypeOpenAIChatCompletion ApiType = "openai-chat"
	// ApiTypeOpenAIResponses selects the OpenAI Responses API (/v1/responses).
	// Required for models that use the phase field (gpt-5.3-codex, gpt-5.4-*).
	ApiTypeOpenAIResponses ApiType = "openai-responses"
	// ApiTypeAnthropicMessages selects the Anthropic Messages API (/v1/messages).
	// Provides native cache_control, thinking blocks, and anthropic-beta headers.
	ApiTypeAnthropicMessages ApiType = "anthropic-messages"
)

// Valid returns true if t is a known constant or the zero value (auto).
func (t ApiType) Valid() bool {
	switch t {
	case ApiTypeAuto, ApiTypeOpenAIChatCompletion, ApiTypeOpenAIResponses, ApiTypeAnthropicMessages:
		return true
	default:
		return false
	}
}

type StreamRequest = Request

// RequestMeta carries provider-specific request attribution metadata used by
// OpenAI-compatible APIs such as OpenAI and OpenRouter.
type RequestMeta struct {
	User     string         `json:"user,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (m *RequestMeta) Clone() *RequestMeta {
	if m == nil {
		return nil
	}
	return &RequestMeta{User: m.User, Metadata: cloneRequestMetaMap(m.Metadata)}
}

func cloneRequestMetaMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func ensureRequestMeta(r *Request) *RequestMeta {
	if r.RequestMeta == nil {
		r.RequestMeta = &RequestMeta{}
	}
	return r.RequestMeta
}

func SynthesizeRequestCacheHint(messages Messages) *CacheHint {
	for _, m := range messages {
		if m.CacheHint != nil && m.CacheHint.Enabled {
			return &CacheHint{Enabled: true, TTL: m.CacheHint.TTL}
		}
	}
	return nil
}

// Request configures a provider CreateStream call.
type Request struct {
	// Model is the model identifier or alias to use, e.g. "fast", "anthropic/claude-sonnet-4-5".
	Model string `json:"model"`

	// Messages is the conversation history to send to the model.
	Messages Messages `json:"messages"`

	// MaxTokens limits the maximum number of tokens in the response.
	// When 0, the provider's default is used.
	MaxTokens int `json:"max_tokens,omitempty"`

	// Temperature controls randomness in sampling. Higher values produce
	// more diverse outputs (0.0-2.0 for most providers). Not supported by
	// Anthropic.
	Temperature float64 `json:"temperature,omitempty"`

	// TopP is the nucleus sampling threshold. The model considers only tokens
	// comprising the top P probability mass. Not supported by Anthropic.
	TopP float64 `json:"top_p,omitempty"`

	// TopK restricts token selection to the K most likely tokens. Higher values
	// increase diversity. Not supported by Anthropic.
	TopK int `json:"top_k,omitempty"`

	// OutputFormat specifies the desired output format.
	// Supported by OpenAI and Anthropic. When set to JSON, the model will
	// be constrained to output valid JSON.
	OutputFormat OutputFormat `json:"output_format,omitempty"`

	// Tools is the set of tools the model may call during the response.
	Tools []llmtool.Definition `json:"tools,omitempty"`

	// ToolChoice controls how the model selects tools. Defaults to Auto when Tools are provided.
	ToolChoice ToolChoice `json:"tool_choice,omitempty"`

	// Effort controls how thoroughly the model works on the response.
	Effort Effort `json:"effort,omitempty"`

	// Thinking controls whether extended/chain-of-thought reasoning is used.
	// This is a mode selector (on/off/auto), not a depth control.
	Thinking ThinkingMode `json:"thinking,omitempty"`

	// RequestMeta carries OpenAI-compatible request attribution metadata.
	RequestMeta *RequestMeta `json:"request_meta,omitempty"`

	// CacheHint is a top-level prompt caching hint. Behaviour is provider-specific:
	// Anthropic auto mode, Bedrock trailing cachePoint, OpenAI extended retention.
	CacheHint *CacheHint `json:"cache_hint,omitempty"`

	// ApiTypeHint expresses a preferred wire protocol. Providers honour it when
	// they support the requested API; otherwise they fall back to their default.
	// The actual API used is always reported in RequestEvent.ResolvedApiType.
	ApiTypeHint ApiType `json:"api_type_hint,omitempty"`
}

// Validate checks that the options are valid.
func (o Request) Validate() error {
	// Validate Model
	if o.Model == "" {
		return errors.New("model is required")
	}

	// Validate Effort
	if !o.Effort.Valid() {
		return fmt.Errorf("invalid Effort %q", o.Effort)
	}

	// Validate Thinking
	if !o.Thinking.Valid() {
		return fmt.Errorf("invalid Thinking %q", o.Thinking)
	}

	if !o.ApiTypeHint.Valid() {
		return fmt.Errorf("invalid ApiTypeHint %q; valid values: auto, openai-chat, openai-responses, anthropic-messages", o.ApiTypeHint)
	}

	// Validate Tools
	for i, tool := range o.Tools {
		if err := tool.Validate(); err != nil {
			return fmt.Errorf("tools[%d]: %w", i, err)
		}
	}

	// Validate messages
	for i, m := range o.Messages {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("messages[%d]: %w", i, err)
		}
	}

	// Validate MaxTokens
	if o.MaxTokens < 0 {
		return errors.New("MaxTokens must be non-negative")
	}

	// Validate Temperature
	if o.Temperature < 0 || o.Temperature > 2.0 {
		return errors.New("temperature must be between 0.0 and 2.0")
	}

	// Validate TopP
	if o.TopP < 0 || o.TopP > 1.0 {
		return errors.New("TopP must be between 0.0 and 1.0")
	}

	// Validate TopK
	if o.TopK < 0 {
		return errors.New("TopK must be non-negative")
	}

	// Validate OutputFormat
	if o.OutputFormat != "" && o.OutputFormat != OutputFormatText && o.OutputFormat != OutputFormatJSON {
		return fmt.Errorf("invalid OutputFormat %q; must be one of: text, json", o.OutputFormat)
	}

	// Validate ToolChoice
	if o.ToolChoice != nil && len(o.Tools) == 0 {
		return errors.New("ToolChoice set but no Tools provided")
	}

	if tc, ok := o.ToolChoice.(ToolChoiceTool); ok {
		if tc.Name == "" {
			return errors.New("ToolChoiceTool.ToolName is required")
		}
		found := false
		for _, t := range o.Tools {
			if t.Name == tc.Name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("ToolChoiceTool references unknown tool %q", tc.Name)
		}
	}

	return nil
}
