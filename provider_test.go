package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamOptions_Validate(t *testing.T) {
	validTools := []ToolDefinition{
		{Name: "get_weather", Description: "Get weather", Parameters: map[string]any{"type": "object"}},
		{Name: "send_email", Description: "Send email", Parameters: map[string]any{"type": "object"}},
	}

	tests := []struct {
		name    string
		opts    StreamOptions
		wantErr string
	}{
		{
			name: "valid - no tools, no tool choice",
			opts: StreamOptions{
				Model:    "gpt-4",
				Messages: Messages{&UserMsg{Content: "Hello"}},
			},
			wantErr: "",
		},
		{
			name: "invalid - empty model",
			opts: StreamOptions{
				Model:    "",
				Messages: Messages{&UserMsg{Content: "Hello"}},
			},
			wantErr: "Model is required",
		},
		{
			name: "invalid - bad ReasoningEffort",
			opts: StreamOptions{
				Model:           "gpt-4",
				Messages:        Messages{&UserMsg{Content: "Hello"}},
				ReasoningEffort: "invalid_value",
			},
			wantErr: `invalid ReasoningEffort "invalid_value"`,
		},
		{
			name: "valid - ReasoningEffort empty (default)",
			opts: StreamOptions{
				Model:           "gpt-4",
				Messages:        Messages{&UserMsg{Content: "Hello"}},
				ReasoningEffort: "",
			},
			wantErr: "",
		},
		{
			name: "invalid - tool without name",
			opts: StreamOptions{
				Model:    "gpt-4",
				Messages: Messages{&UserMsg{Content: "Hello"}},
				Tools:    []ToolDefinition{{Name: "", Description: "No name"}},
			},
			wantErr: "tools[0]: tool definition: name is required",
		},
		{
			name: "invalid - tool parameters not object type",
			opts: StreamOptions{
				Model:    "gpt-4",
				Messages: Messages{&UserMsg{Content: "Hello"}},
				Tools:    []ToolDefinition{{Name: "bad_tool", Parameters: map[string]any{"type": "string"}}},
			},
			wantErr: `tool definition "bad_tool": parameters type must be "object"`,
		},
		{
			name: "valid - tool with nil parameters",
			opts: StreamOptions{
				Model:    "gpt-4",
				Messages: Messages{&UserMsg{Content: "Hello"}},
				Tools:    []ToolDefinition{{Name: "simple_tool", Description: "No params"}},
			},
			wantErr: "",
		},
		{
			name: "valid - tools with nil tool choice (defaults to auto)",
			opts: StreamOptions{
				Model:      "gpt-4",
				Messages:   Messages{&UserMsg{Content: "Hello"}},
				Tools:      validTools,
				ToolChoice: nil,
			},
			wantErr: "",
		},
		{
			name: "valid - tools with ToolChoiceAuto",
			opts: StreamOptions{
				Model:      "gpt-4",
				Messages:   Messages{&UserMsg{Content: "Hello"}},
				Tools:      validTools,
				ToolChoice: ToolChoiceAuto{},
			},
			wantErr: "",
		},
		{
			name: "valid - tools with ToolChoiceRequired",
			opts: StreamOptions{
				Model:      "gpt-4",
				Messages:   Messages{&UserMsg{Content: "Hello"}},
				Tools:      validTools,
				ToolChoice: ToolChoiceRequired{},
			},
			wantErr: "",
		},
		{
			name: "valid - tools with ToolChoiceNone",
			opts: StreamOptions{
				Model:      "gpt-4",
				Messages:   Messages{&UserMsg{Content: "Hello"}},
				Tools:      validTools,
				ToolChoice: ToolChoiceNone{},
			},
			wantErr: "",
		},
		{
			name: "valid - tools with ToolChoiceTool referencing existing tool",
			opts: StreamOptions{
				Model:      "gpt-4",
				Messages:   Messages{&UserMsg{Content: "Hello"}},
				Tools:      validTools,
				ToolChoice: ToolChoiceTool{Name: "get_weather"},
			},
			wantErr: "",
		},
		{
			name: "invalid - ToolChoice set but no tools",
			opts: StreamOptions{
				Model:      "gpt-4",
				Messages:   Messages{&UserMsg{Content: "Hello"}},
				Tools:      nil,
				ToolChoice: ToolChoiceRequired{},
			},
			wantErr: "ToolChoice set but no Tools provided",
		},
		{
			name: "invalid - ToolChoiceTool with empty name",
			opts: StreamOptions{
				Model:      "gpt-4",
				Messages:   Messages{&UserMsg{Content: "Hello"}},
				Tools:      validTools,
				ToolChoice: ToolChoiceTool{Name: ""},
			},
			wantErr: "ToolChoiceTool.Name is required",
		},
		{
			name: "invalid - ToolChoiceTool references unknown tool",
			opts: StreamOptions{
				Model:      "gpt-4",
				Messages:   Messages{&UserMsg{Content: "Hello"}},
				Tools:      validTools,
				ToolChoice: ToolChoiceTool{Name: "unknown_tool"},
			},
			wantErr: `ToolChoiceTool references unknown tool "unknown_tool"`,
		},
		{
			name: "invalid - message validation fails",
			opts: StreamOptions{
				Model:    "gpt-4",
				Messages: Messages{&UserMsg{Content: ""}}, // empty content is invalid
			},
			wantErr: "messages[0]:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestToolChoice_Interface(t *testing.T) {
	// Verify all types implement ToolChoice interface
	var _ ToolChoice = ToolChoiceAuto{}
	var _ ToolChoice = ToolChoiceRequired{}
	var _ ToolChoice = ToolChoiceNone{}
	var _ ToolChoice = ToolChoiceTool{Name: "test"}
}

func TestReasoningEffort_Constants(t *testing.T) {
	// Verify constant values match OpenAI API expectations
	assert.Equal(t, ReasoningEffort("none"), ReasoningEffortNone)
	assert.Equal(t, ReasoningEffort("minimal"), ReasoningEffortMinimal)
	assert.Equal(t, ReasoningEffort("low"), ReasoningEffortLow)
	assert.Equal(t, ReasoningEffort("medium"), ReasoningEffortMedium)
	assert.Equal(t, ReasoningEffort("high"), ReasoningEffortHigh)
	assert.Equal(t, ReasoningEffort("xhigh"), ReasoningEffortXHigh)
}

func TestStreamOptions_WithReasoningEffort(t *testing.T) {
	opts := StreamOptions{
		Model:           "gpt-5",
		Messages:        Messages{&UserMsg{Content: "Hello"}},
		ReasoningEffort: ReasoningEffortLow,
	}

	err := opts.Validate()
	require.NoError(t, err)
	assert.Equal(t, ReasoningEffortLow, opts.ReasoningEffort)
}

func TestReasoningEffort_Valid(t *testing.T) {
	tests := []struct {
		effort ReasoningEffort
		want   bool
	}{
		{"", true},
		{ReasoningEffortNone, true},
		{ReasoningEffortMinimal, true},
		{ReasoningEffortLow, true},
		{ReasoningEffortMedium, true},
		{ReasoningEffortHigh, true},
		{ReasoningEffortXHigh, true},
		{"invalid", false},
		{"MEDIUM", false}, // case sensitive
		{"max", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.effort), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.effort.Valid())
		})
	}
}

func TestToolDefinition_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tool    ToolDefinition
		wantErr string
	}{
		{
			name:    "valid - with parameters",
			tool:    ToolDefinition{Name: "get_weather", Parameters: map[string]any{"type": "object"}},
			wantErr: "",
		},
		{
			name:    "valid - nil parameters",
			tool:    ToolDefinition{Name: "simple_tool"},
			wantErr: "",
		},
		{
			name:    "valid - empty parameters map",
			tool:    ToolDefinition{Name: "empty_params", Parameters: map[string]any{}},
			wantErr: "",
		},
		{
			name:    "invalid - empty name",
			tool:    ToolDefinition{Name: ""},
			wantErr: "name is required",
		},
		{
			name:    "invalid - parameters type not object",
			tool:    ToolDefinition{Name: "bad_tool", Parameters: map[string]any{"type": "array"}},
			wantErr: `parameters type must be "object"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tool.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}
