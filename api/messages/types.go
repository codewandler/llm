package messages

import "encoding/json"

// Request is the wire body for POST /v1/messages.
// Ref: https://docs.anthropic.com/en/api/messages#body
type Request struct {
	Model        string           `json:"model"`
	MaxTokens    int              `json:"max_tokens"`
	Stream       bool             `json:"stream"`
	System       SystemBlocks     `json:"system,omitempty"`
	Messages     []Message        `json:"messages"`
	Tools        []ToolDefinition `json:"tools,omitempty"`
	ToolChoice   any              `json:"tool_choice,omitempty"`
	Thinking     *ThinkingConfig  `json:"thinking,omitempty"`
	Metadata     *Metadata        `json:"metadata,omitempty"`
	TopK         int              `json:"top_k,omitempty"`
	TopP         float64          `json:"top_p,omitempty"`
	OutputConfig *OutputConfig    `json:"output_config,omitempty"`
}

type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type OutputConfig struct {
	Format *JSONOutputFormat `json:"format,omitempty"`
	Effort string            `json:"effort,omitempty"`
}

type JSONOutputFormat struct {
	Type   string `json:"type"`
	Schema any    `json:"schema,omitempty"`
}

type Metadata struct {
	UserID string `json:"user_id,omitempty"`
}

type SystemBlocks []*TextBlock

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type TextBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type ImageBlock struct {
	Type   string      `json:"type"`
	Source ImageSource `json:"source"`
}

type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type ToolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type ToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type ThinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature,omitempty"`
}

type ToolDefinition struct {
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	InputSchema  any           `json:"input_schema"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type CacheControl struct {
	Type string `json:"type"`
}

// SSE: message_start
type MessageStartEvent struct {
	Message MessageStartPayload `json:"message"`
}

type MessageStartPayload struct {
	ID    string       `json:"id"`
	Model string       `json:"model"`
	Usage MessageUsage `json:"usage"`
}

type MessageUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// SSE: content_block_start
type ContentBlockStartEvent struct {
	Index        int             `json:"index"`
	ContentBlock json.RawMessage `json:"content_block"`
}

// StartBlockView is an optional helper for callers/tests that need typed views
// over ContentBlockStartEvent.ContentBlock.
type StartBlockView struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// SSE: content_block_delta
type ContentBlockDeltaEvent struct {
	Index int   `json:"index"`
	Delta Delta `json:"delta"`
}

type Delta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

// SSE: content_block_stop
type ContentBlockStopEvent struct {
	Index int `json:"index"`
}

// Parser-synthesized completion events (from content_block_stop)
type TextCompleteEvent struct {
	Index int
	Text  string
}

type ThinkingCompleteEvent struct {
	Index     int
	Thinking  string
	Signature string
}

type ToolCompleteEvent struct {
	Index int
	ID    string
	Name  string
	Args  map[string]any
}

// SSE: message_delta
type MessageDeltaEvent struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// SSE: message_stop
type MessageStopEvent struct{}

// SSE: error
type StreamErrorEvent struct {
	Type string `json:"type"`
	Err  struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (e *StreamErrorEvent) Error() string {
	if e.Err.Type != "" {
		return "messages stream error " + e.Err.Type + ": " + e.Err.Message
	}
	return "messages stream error: " + e.Err.Message
}

// SSE: ping
type PingEvent struct{}
