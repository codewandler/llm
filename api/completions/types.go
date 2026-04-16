package completions

// Request is the wire body for POST /v1/chat/completions.
// Ref: https://platform.openai.com/docs/api-reference/chat/create
type Request struct {
	Model                string          `json:"model"`
	Messages             []Message       `json:"messages"`
	Tools                []Tool          `json:"tools,omitempty"`
	ToolChoice           any             `json:"tool_choice,omitempty"`
	ReasoningEffort      string          `json:"reasoning_effort,omitempty"`
	PromptCacheRetention string          `json:"prompt_cache_retention,omitempty"`
	MaxTokens            int             `json:"max_tokens,omitempty"`
	Temperature          float64         `json:"temperature,omitempty"`
	TopP                 float64         `json:"top_p,omitempty"`
	TopK                 int             `json:"top_k,omitempty"`
	Stop                 []string        `json:"stop,omitempty"`
	N                    int             `json:"n,omitempty"`
	PresencePenalty      float64         `json:"presence_penalty,omitempty"`
	FrequencyPenalty     float64         `json:"frequency_penalty,omitempty"`
	LogProbs             bool            `json:"logprobs,omitempty"`
	TopLogProbs          int             `json:"top_logprobs,omitempty"`
	ResponseFormat       *ResponseFormat `json:"response_format,omitempty"`
	Stream               bool            `json:"stream"`
	StreamOptions        *StreamOptions  `json:"stream_options,omitempty"`
	User                 string          `json:"user,omitempty"`
	Metadata             map[string]any  `json:"metadata,omitempty"`
	Store                bool            `json:"store,omitempty"`
	ParallelToolCalls    bool            `json:"parallel_tool_calls,omitempty"`
	ServiceTier          string          `json:"service_tier,omitempty"`
}

// Message is a chat message in the messages array.
//
// Content supports both string and rich content arrays (OpenAI wire supports
// either shape depending on model/features).
type Message struct {
	Role       string     `json:"role"` // "system", "user", "assistant", "tool"
	Content    any        `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a tool call stored in an assistant message.
type ToolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"` // "function"
	Function FuncCall `json:"function"`
}

// FuncCall is the function invocation inside a ToolCall.
type FuncCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded
}

// Tool is a tool definition in the request.
type Tool struct {
	Type     string      `json:"type"` // "function"
	Function FuncPayload `json:"function"`
}

// FuncPayload is the function spec in a Tool definition.
type FuncPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
	Strict      bool   `json:"strict,omitempty"`
}

// ResponseFormat controls structured output.
type ResponseFormat struct {
	Type string `json:"type"` // "json_object", "text"
}

// StreamOptions controls stream metadata.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// Chunk is one SSE payload in a Chat Completions stream.
type Chunk struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"` // final chunk when include_usage=true
}

// Choice is one completion choice inside a Chunk.
//
// FinishReason is nil for interim chunks (wire value null), set on terminal
// choice chunks (e.g. "stop", "tool_calls", "length").
type Choice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// Delta is the content delta in a Choice.
type Delta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []ToolCallDelta `json:"tool_calls,omitempty"`
}

// ToolCallDelta is an incremental tool call fragment in a streaming Delta.
type ToolCallDelta struct {
	Index    int           `json:"index"`
	ID       string        `json:"id,omitempty"`
	Type     string        `json:"type,omitempty"` // "function"
	Function FuncCallDelta `json:"function"`
}

// FuncCallDelta is an incremental function call fragment.
type FuncCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"` // accumulate across chunks
}

// Usage is the token usage in the final Chunk (when IncludeUsage=true).
type Usage struct {
	PromptTokens            int         `json:"prompt_tokens"`
	CompletionTokens        int         `json:"completion_tokens"`
	TotalTokens             int         `json:"total_tokens"`
	PromptTokensDetails     *TokDetails `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *TokDetails `json:"completion_tokens_details,omitempty"`
}

// TokDetails breaks down prompt or completion token counts.
type TokDetails struct {
	CachedTokens    int `json:"cached_tokens,omitempty"`
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}
