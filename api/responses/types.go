package responses

// Request is the wire body for POST /v1/responses.
// Ref: https://platform.openai.com/docs/api-reference/responses/create
type Request struct {
	Model                string          `json:"model"`
	Input                []Input         `json:"input"`
	Instructions         string          `json:"instructions,omitempty"`
	Tools                []Tool          `json:"tools,omitempty"`
	ToolChoice           any             `json:"tool_choice,omitempty"`
	Reasoning            *Reasoning      `json:"reasoning,omitempty"`
	MaxOutputTokens      int             `json:"max_output_tokens,omitempty"`
	Temperature          float64         `json:"temperature,omitempty"`
	TopP                 float64         `json:"top_p,omitempty"`
	TopK                 int             `json:"top_k,omitempty"`
	ResponseFormat       *ResponseFormat `json:"response_format,omitempty"`
	PromptCacheRetention string          `json:"prompt_cache_retention,omitempty"`
	Stream               bool            `json:"stream"`
}

// Reasoning controls reasoning/thinking for supported models.
type Reasoning struct {
	Effort  string `json:"effort,omitempty"`  // "low", "medium", "high"
	Summary string `json:"summary,omitempty"` // "auto", "concise", "detailed"
}

// ResponseFormat controls structured output.
type ResponseFormat struct {
	Type string `json:"type"` // "json_object", "text"
}

// Input is a polymorphic item in the "input" array.
type Input struct {
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	Type      string `json:"type,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

// Tool is a function definition for the Responses API.
// Unlike Chat Completions, name/description/parameters sit at the top level.
type Tool struct {
	Type        string `json:"type"` // "function"
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
	Strict      bool   `json:"strict,omitempty"`
}

// ResponseCreatedEvent is the first event in every stream.
// SSE event: "response.created"
type ResponseCreatedEvent struct {
	Response struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"response"`
}

// OutputItemAddedEvent marks the start of a new output item.
// SSE event: "response.output_item.added"
type OutputItemAddedEvent struct {
	OutputIndex int `json:"output_index"`
	Item        struct {
		Type   string `json:"type"`    // "message" or "function_call"
		ID     string `json:"id"`      // internal item ID
		CallID string `json:"call_id"` // external call ID used in tool result messages
		Name   string `json:"name"`    // function name for function_call items
	} `json:"item"`
}

// ReasoningDeltaEvent carries an incremental reasoning/thinking chunk.
// SSE event: "response.reasoning_summary_text.delta"
type ReasoningDeltaEvent struct {
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

// TextDeltaEvent carries an incremental text chunk.
// SSE event: "response.output_text.delta"
type TextDeltaEvent struct {
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

// FuncArgsDeltaEvent carries an incremental function-call argument fragment.
// SSE event: "response.function_call_arguments.delta"
type FuncArgsDeltaEvent struct {
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

// OutputItemDoneEvent is emitted when a non-function-call output item finishes.
// SSE event: "response.output_item.done" (for message items)
// For function_call items, the parser synthesises a ToolCompleteEvent instead.
type OutputItemDoneEvent struct {
	OutputIndex int `json:"output_index"`
	Item        struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // full JSON; non-empty for function_call
	} `json:"item"`
}

// ToolCompleteEvent is synthesised by the parser from OutputItemDoneEvent
// when Item.Type == "function_call".
type ToolCompleteEvent struct {
	ID   string         // call_id
	Name string         // function name
	Args map[string]any // fully decoded JSON arguments
}

// ResponseCompletedEvent is the terminal event.
// SSE events: "response.completed" and "response.failed".
type ResponseCompletedEvent struct {
	Response struct {
		ID                string `json:"id"`
		Model             string `json:"model"`
		Status            string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details,omitempty"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
		Usage *ResponseUsage `json:"usage,omitempty"`
	} `json:"response"`
}

// ResponseUsage carries token counts from ResponseCompletedEvent.
type ResponseUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details,omitempty"`
}

// APIErrorEvent is emitted on stream-level errors.
// SSE event: "error"
// Wire format: {"error": {"message": "...", "code": "..."}}
type APIErrorEvent struct {
	Err struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (e *APIErrorEvent) Error() string {
	if e.Err.Code != "" {
		return "responses API error " + e.Err.Code + ": " + e.Err.Message
	}
	return "responses API error: " + e.Err.Message
}
