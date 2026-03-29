package llm

import "fmt"

type (
	ToolChoice interface {
		toolChoice()
		String() string
	}

	ToolChoiceAuto     struct{}
	ToolChoiceRequired struct{}
	ToolChoiceNone     struct{}
	ToolChoiceTool     struct {
		Name string
	}
)

// === ToolChoice NONE ===

func (ToolChoiceNone) toolChoice()      {}
func (t ToolChoiceNone) String() string { return "ToolChoice(none)" }

// === ToolChoice TOOL ===

func (ToolChoiceTool) toolChoice()      {}
func (t ToolChoiceTool) String() string { return fmt.Sprintf("ToolChoice(tool=%s)", t.Name) }

func (ToolChoiceRequired) toolChoice() {}
func (ToolChoiceAuto) toolChoice()     {}

func (t ToolChoiceRequired) String() string { return "ToolChoice(required)" }
func (t ToolChoiceAuto) String() string     { return "ToolChoice(auto)" }
