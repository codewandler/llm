package llm

import (
	"errors"
	"fmt"
)

// --- ReasoningEffort ---

// ReasoningEffort controls the amount of reasoning for reasoning models.
// Lower values result in faster responses with fewer reasoning tokens.
type ReasoningEffort string

const (
	// ReasoningEffortNone disables reasoning (GPT-5.1+ only).
	ReasoningEffortNone ReasoningEffort = "none"
	// ReasoningEffortMinimal uses minimal reasoning effort.
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	// ReasoningEffortLow uses low reasoning effort.
	ReasoningEffortLow ReasoningEffort = "low"
	// ReasoningEffortMedium uses medium reasoning effort (default for most models before GPT-5.1).
	ReasoningEffortMedium ReasoningEffort = "medium"
	// ReasoningEffortHigh uses high reasoning effort.
	ReasoningEffortHigh ReasoningEffort = "high"
	// ReasoningEffortXHigh uses extra high reasoning effort (codex-max+ only).
	ReasoningEffortXHigh ReasoningEffort = "xhigh"
)

// Valid returns true if the ReasoningEffort is a known valid value or empty.
func (r ReasoningEffort) Valid() bool {
	switch r {
	case "", ReasoningEffortNone, ReasoningEffortMinimal, ReasoningEffortLow,
		ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh:
		return true
	default:
		return false
	}
}

// OutputFormat specifies the desired output format for the model response.
type OutputFormat string

const (
	// OutputFormatText requests plain text output (default for most providers).
	OutputFormatText OutputFormat = "text"
	// OutputFormatJSON requests JSON output. The model will be constrained
	// to output valid JSON. Not all providers support this.
	OutputFormatJSON OutputFormat = "json"
)

type StreamRequest = Request

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
	Tools []ToolDefinition `json:"tools,omitempty"`

	// ToolChoice controls how the model selects tools. Defaults to Auto when Tools are provided.
	ToolChoice ToolChoice `json:"tool_choice,omitempty"`

	// ReasoningEffort controls the depth of reasoning for models that support it (e.g. OpenAI o-series).
	ReasoningEffort ReasoningEffort `json:"reasoning_effort,omitempty"`

	// CacheHint is a top-level prompt caching hint. Behaviour is provider-specific:
	// Anthropic auto mode, Bedrock trailing cachePoint, OpenAI extended retention.
	CacheHint *CacheHint `json:"cache_hint,omitempty"`
}

// Validate checks that the options are valid.
func (o Request) Validate() error {
	// Validate Model
	if o.Model == "" {
		return errors.New("model is required")
	}

	// Validate ReasoningEffort
	if !o.ReasoningEffort.Valid() {
		return fmt.Errorf("invalid ReasoningEffort %q", o.ReasoningEffort)
	}

	// Validate Tools
	for i, tool := range o.Tools {
		if err := tool.Validate(); err != nil {
			return fmt.Errorf("tools[%d]: %w", i, err)
		}
	}

	// Validate messages
	for i, msg := range o.Messages {
		if err := msg.Validate(); err != nil {
			return fmt.Errorf("messages[%d]: %w", i, err)
		}
	}

	// Validate MaxTokens
	if o.MaxTokens < 0 {
		return errors.New("MaxTokens must be non-negative")
	}

	// Validate Temperature
	if o.Temperature < 0 || o.Temperature > 2.0 {
		return errors.New("Temperature must be between 0.0 and 2.0")
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
			return errors.New("ToolChoiceTool.Name is required")
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
