package llm

// DeltaKind identifies the kind of incremental content carried by a DeltaEvent.
type DeltaKind string

const (
	DeltaKindText      DeltaKind = "text"
	DeltaKindReasoning DeltaKind = "reasoning"
	DeltaTypeTool      DeltaKind = "tool"
)

type ToolDeltaPart struct {
	// ToolID, ToolName, and ToolArgs are populated for DeltaTypeTool.
	// ToolArgs is a raw partial JSON fragment — not yet a complete object.
	ToolID   string `json:"tool_id,omitempty"`
	ToolName string `json:"tool_name,omitempty"`
	ToolArgs string `json:"tool_args,omitempty"`
}

// DeltaEvent carries one incremental content chunk from the model eventPub.
// Exactly one payload field is populated, indicated by EventType.
type DeltaEvent struct {
	// Type identifies which payload field is set.
	Kind DeltaKind `json:"kind"`

	// Index is the position of this content block in the model's output array.
	// nil when the provider does not supply block-level indexing.
	//
	// Index is meaningful because a single HTTP response can contain multiple
	// blocks of the same type. Add Anthropic's interleaved-thinking beta a
	// single response may produce: thinking(0) → text(1) → tool(2) → thinking(3) → text(4).
	// Without Index a consumer cannot tell which thinking or text block a delta
	// belongs to.
	//
	// Provider semantics:
	//   Anthropic          — content_block index, all block types
	//   Bedrock            — ContentBlockIndex, all block types
	//   OpenAI Responses   — output_index, all output types
	//   OpenAI Completions — tool_calls[].index, tool calls only; text=nil
	//   OpenRouter         — tool_calls[].index, tool calls only; text=nil
	//   Ollama             — nil (complete tool calls only, no streaming fragments)
	Index *uint32 `json:"index,omitempty"`

	// Text is populated for DeltaKindText.
	Text string `json:"text,omitempty"`

	// Reasoning is populated for DeltaKindReasoning.
	Reasoning string `json:"reasoning,omitempty"`

	ToolDeltaPart
}

func (e *DeltaEvent) Type() EventType { return StreamEventDelta }
func (e *DeltaEvent) WithIndex(idx uint32) *DeltaEvent {
	e.Index = &idx
	return e
}

func TextDelta(text string) *DeltaEvent { return &DeltaEvent{Kind: DeltaKindText, Text: text} }
func ReasoningDelta(text string) *DeltaEvent {
	return &DeltaEvent{Kind: DeltaKindReasoning, Reasoning: text}
}
func ToolDelta(id, name, argsFragment string) *DeltaEvent {
	return &DeltaEvent{
		Kind: DeltaTypeTool,
		ToolDeltaPart: ToolDeltaPart{
			ToolID:   id,
			ToolName: name,
			ToolArgs: argsFragment,
		},
	}
}
