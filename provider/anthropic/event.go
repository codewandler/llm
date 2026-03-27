package anthropic

// MessageStartEvent is emitted at the beginning of a streaming response.
type MessageStartEvent struct {
	Message MessageStartPayload `json:"message"`
}

// MessageStartPayload is the payload of a MessageStartEvent.
type MessageStartPayload struct {
	ID    string       `json:"id"`
	Model string       `json:"model"`
	Usage MessageUsage `json:"usage"`
}

// MessageUsage carries input token counts from the message_start event.
type MessageUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// MessageDeltaEvent carries the stop reason and final output token count.
type MessageDeltaEvent struct {
	Delta MessageDelta `json:"delta"`
	Usage OutputUsage  `json:"usage"`
}

// MessageDelta is the delta payload of a MessageDeltaEvent.
type MessageDelta struct {
	StopReason string `json:"stop_reason"`
}

// OutputUsage carries output token counts from the message_delta event.
type OutputUsage struct {
	OutputTokens int `json:"output_tokens"`
}

// MessageStopEvent signals that the message stream is complete. It carries no payload.
type MessageStopEvent struct{}

// ContentBlockStartEvent marks the beginning of a content block (text, tool_use, thinking).
type ContentBlockStartEvent struct {
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

// ContentBlock describes the type and initial state of a content block.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// ContentBlockDeltaEvent carries an incremental delta within a content block.
type ContentBlockDeltaEvent struct {
	Index int               `json:"index"`
	Delta ContentBlockDelta `json:"delta"`
}

// ContentBlockDelta is the delta payload within a ContentBlockDeltaEvent.
type ContentBlockDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
}

// ContentBlockStopEvent signals the end of a content block.
type ContentBlockStopEvent struct {
	Index int `json:"index"`
}

// StreamErrorEvent is sent when the API returns a stream-level error.
// Named StreamErrorEvent to avoid collision with llm.ErrorEvent.
type StreamErrorEvent struct {
	Error StreamErrorPayload `json:"error"`
}

// StreamErrorPayload carries the error message from a StreamErrorEvent.
type StreamErrorPayload struct {
	Message string `json:"message"`
}
