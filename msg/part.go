package msg

import "strings"

type PartType string

const (
	PartTypeText       PartType = "text"
	PartTypeThinking   PartType = "thinking"
	PartTypeToolCall   PartType = "tool_call"
	PartTypeToolResult PartType = "tool_result"
)

type (
	IntoPart  interface{ IntoPart() Part }
	IntoParts interface{ IntoParts() Parts }
)

type Part struct {
	Type       PartType      `json:"type"`
	Text       string        `json:"text,omitempty"`
	ToolCall   *ToolCall     `json:"tool_call,omitempty"`
	ToolResult *ToolResult   `json:"tool_result,omitempty"`
	Thinking   *ThinkingPart `json:"thinking,omitempty"`
}

func (p Part) IntoPart() Part { return p }

type Parts []Part

func (p Parts) ByType(t PartType) Parts {
	var parts Parts
	for _, part := range p {
		if part.Type == t {
			parts = append(parts, part)
		}
	}
	return parts
}

func (p Parts) Text() string {
	if len(p) == 0 {
		return ""
	}
	sb := strings.Builder{}
	for _, part := range p {
		if part.Type == PartTypeText {
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}

func (p Parts) ToolCalls() ToolCalls {
	if len(p) == 0 {
		return nil
	}
	var calls ToolCalls
	for _, part := range p {
		if part.Type == PartTypeToolCall && part.ToolCall != nil {
			calls = append(calls, *part.ToolCall)
		}
	}
	return calls
}

func (p Parts) ToolResults() ToolResults {
	if len(p) == 0 {
		return nil
	}
	var results ToolResults
	for _, part := range p {
		if part.Type == PartTypeToolResult && part.ToolResult != nil {
			results = append(results, *part.ToolResult)
		}
	}
	return results
}

var _ []Part = Parts{}

func (p Parts) Append(parts ...IntoPart) Parts {
	newParts := make(Parts, 0, len(p)+len(parts))
	for _, part := range parts {
		newParts = append(newParts, part.IntoPart())
	}
	return append(p, newParts...)
}

func Text(text string) Part { return Part{Type: PartTypeText, Text: text} }
func Thinking(thought, signature string) Part {
	return Part{
		Type: PartTypeThinking,
		Thinking: &ThinkingPart{
			Text:      thought,
			Signature: signature,
		},
	}
}

type PartsBuilder struct {
	parts Parts
}

func BuildParts() *PartsBuilder      { return &PartsBuilder{} }
func (b *PartsBuilder) Build() Parts { return b.parts }
func (b *PartsBuilder) Part(part Part) *PartsBuilder {
	b.parts = append(b.parts, part)
	return b
}
func (b *PartsBuilder) Parts(parts Parts) *PartsBuilder {
	b.parts = append(b.parts, parts...)
	return b
}

func (b *PartsBuilder) PartsFrom(parts IntoParts) *PartsBuilder {
	b.parts = append(b.parts, parts.IntoParts()...)
	return b
}

func (b *PartsBuilder) IntoParts() Parts { return b.parts }

func (b *PartsBuilder) Text(text string) *PartsBuilder {
	b.parts.Append(Text(text))
	return b
}

func (b *PartsBuilder) Thinking(thought, signature string) *PartsBuilder {
	b.parts.Append(Thinking(thought, signature))
	return b
}

func (b *PartsBuilder) ToolCall(toolCall ToolCall) *PartsBuilder {
	b.parts.Append(toolCall)
	return b
}

func (b *PartsBuilder) ToolResult(toolResult ToolResult) Part {
	return Part{
		Type:       PartTypeToolResult,
		ToolResult: &toolResult,
	}
}
