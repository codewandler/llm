package llm

import (
	"fmt"
	"strings"
)

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

// ParseToolChoice parses a CLI string into a ToolChoice.
// Accepted values: "auto" or "", "none", "required", "tool:<name>".
// An empty string returns ToolChoiceAuto (not nil); use ToolChoiceFlag for
// flag parsing where empty means "not specified" (nil).
func ParseToolChoice(s string) (ToolChoice, error) {
	switch s {
	case "", "auto":
		return ToolChoiceAuto{}, nil
	case "none":
		return ToolChoiceNone{}, nil
	case "required":
		return ToolChoiceRequired{}, nil
	default:
		if name, ok := strings.CutPrefix(s, "tool:"); ok && name != "" {
			return ToolChoiceTool{Name: name}, nil
		}
		return nil, fmt.Errorf(
			"invalid tool-choice %q: must be auto, none, required, or tool:<name>", s)
	}
}

// ToolChoiceFlag is a pflag-compatible holder for a ToolChoice value that
// implements encoding.TextMarshaler and encoding.TextUnmarshaler.
// A zero-value ToolChoiceFlag (Value == nil) means "not specified by the caller";
// contrast with ParseToolChoice("") which returns ToolChoiceAuto.
type ToolChoiceFlag struct{ Value ToolChoice }

func (f ToolChoiceFlag) MarshalText() ([]byte, error) {
	if f.Value == nil {
		return []byte(""), nil
	}
	switch tc := f.Value.(type) {
	case ToolChoiceAuto:
		return []byte("auto"), nil
	case ToolChoiceNone:
		return []byte("none"), nil
	case ToolChoiceRequired:
		return []byte("required"), nil
	case ToolChoiceTool:
		return []byte("tool:" + tc.Name), nil
	}
	return nil, fmt.Errorf("unknown ToolChoice type %T", f.Value)
}

func (f *ToolChoiceFlag) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		f.Value = nil
		return nil
	}
	tc, err := ParseToolChoice(string(b))
	if err != nil {
		return err
	}
	f.Value = tc
	return nil
}
