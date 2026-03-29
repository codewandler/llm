package msg

type ToolArgs map[string]any

type ToolCall struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Args ToolArgs `json:"args"`
}

// NewToolCall creates a ToolCall with a non-nil Args map.
func NewToolCall(id, name string, args ToolArgs) ToolCall {
	if args == nil {
		args = ToolArgs{}
	}
	return ToolCall{ID: id, Name: name, Args: args}
}

func (t ToolCall) IntoPart() Part {
	return Part{
		Type:     PartTypeToolCall,
		ToolCall: &t,
	}
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	ToolOutput string `json:"output"`
	IsError    bool   `json:"is_error,omitempty"`
}

func (t ToolResult) IntoMessages() []Message      { return []Message{t.IntoMessage()} }
func (t ToolResult) IntoToolResults() ToolResults { return ToolResults{t} }

type ToolResults []ToolResult

func (t ToolResults) IntoToolResults() ToolResults { return t }
func (t ToolResults) IntoParts() []Part {
	parts := make([]Part, len(t))
	for i, tr := range t {
		parts[i] = tr.IntoPart()
	}
	return parts
}

type IntoToolResults interface {
	IntoToolResults() ToolResults
}

func (t ToolResult) IntoPart() Part {
	return Part{
		Type:       PartTypeToolResult,
		ToolResult: &t,
	}
}

func (t ToolResult) IntoMessage() Message { return buildMsg(RoleTool).Part(t).Build() }

type ToolCalls []ToolCall

func (ts ToolCalls) IntoParts() []Part {
	parts := make([]Part, len(ts))
	for i, tc := range ts {
		parts[i] = tc.IntoPart()
	}
	return parts
}
