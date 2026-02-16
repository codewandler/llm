package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamOptions_Validate(t *testing.T) {
	validTools := []ToolDefinition{
		{Name: "get_weather", Description: "Get weather", Parameters: map[string]any{}},
		{Name: "send_email", Description: "Send email", Parameters: map[string]any{}},
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
