package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/tool"
)

func TestStreamOptions_Validate(t *testing.T) {
	validTools := []tool.Definition{
		{Name: "get_weather", Description: "Get weather", Parameters: map[string]any{"type": "object"}},
		{Name: "send_email", Description: "Publish email", Parameters: map[string]any{"type": "object"}},
	}

	tests := []struct {
		name    string
		opts    Request
		wantErr string
	}{
		{
			name: "valid - no tools, no tool choice",
			opts: Request{
				Model:    "gpt-4",
				Messages: Messages{User("Hello")},
			},
			wantErr: "",
		},
		{
			name: "invalid - empty model",
			opts: Request{
				Model:    "",
				Messages: Messages{User("Hello")},
			},
			wantErr: "model is required",
		},
		{
			name: "invalid - bad Effort",
			opts: Request{
				Model:    "gpt-4",
				Messages: Messages{User("Hello")},
				Effort:   "invalid_value",
			},
			wantErr: `invalid Effort "invalid_value"`,
		},
		{
			name: "invalid - bad Thinking",
			opts: Request{
				Model:    "gpt-4",
				Messages: Messages{User("Hello")},
				Thinking: "invalid_value",
			},
			wantErr: `invalid Thinking "invalid_value"`,
		},
		{
			name: "valid - Effort and Thinking empty (default)",
			opts: Request{
				Model:    "gpt-4",
				Messages: Messages{User("Hello")},
			},
			wantErr: "",
		},
		{
			name: "invalid - tool without name",
			opts: Request{
				Model:    "gpt-4",
				Messages: Messages{User("Hello")},
				Tools:    []tool.Definition{{Name: "", Description: "No name"}},
			},
			wantErr: "tools[0]: tool definition: name is required",
		},
		{
			name: "invalid - tool parameters not object type",
			opts: Request{
				Model:    "gpt-4",
				Messages: Messages{User("Hello")},
				Tools:    []tool.Definition{{Name: "bad_tool", Parameters: map[string]any{"type": "string"}}},
			},
			wantErr: `tool definition "bad_tool": parameters type must be "object"`,
		},
		{
			name: "valid - tool with nil parameters",
			opts: Request{
				Model:    "gpt-4",
				Messages: Messages{User("Hello")},
				Tools:    []tool.Definition{{Name: "simple_tool", Description: "No params"}},
			},
			wantErr: "",
		},
		{
			name: "valid - tools with nil tool choice (defaults to auto)",
			opts: Request{
				Model:      "gpt-4",
				Messages:   Messages{User("Hello")},
				Tools:      validTools,
				ToolChoice: nil,
			},
			wantErr: "",
		},
		{
			name: "valid - tools with ToolChoiceAuto",
			opts: Request{
				Model:      "gpt-4",
				Messages:   Messages{User("Hello")},
				Tools:      validTools,
				ToolChoice: ToolChoiceAuto{},
			},
			wantErr: "",
		},
		{
			name: "valid - tools with ToolChoiceRequired",
			opts: Request{
				Model:      "gpt-4",
				Messages:   Messages{User("Hello")},
				Tools:      validTools,
				ToolChoice: ToolChoiceRequired{},
			},
			wantErr: "",
		},
		{
			name: "valid - tools with ToolChoiceNone",
			opts: Request{
				Model:      "gpt-4",
				Messages:   Messages{User("Hello")},
				Tools:      validTools,
				ToolChoice: ToolChoiceNone{},
			},
			wantErr: "",
		},
		{
			name: "valid - tools with ToolChoiceTool referencing existing tool",
			opts: Request{
				Model:      "gpt-4",
				Messages:   Messages{User("Hello")},
				Tools:      validTools,
				ToolChoice: ToolChoiceTool{Name: "get_weather"},
			},
			wantErr: "",
		},
		{
			name: "invalid - ToolChoice set but no tools",
			opts: Request{
				Model:      "gpt-4",
				Messages:   Messages{User("Hello")},
				Tools:      nil,
				ToolChoice: ToolChoiceRequired{},
			},
			wantErr: "ToolChoice set but no Tools provided",
		},
		{
			name: "invalid - ToolChoiceTool with empty name",
			opts: Request{
				Model:      "gpt-4",
				Messages:   Messages{User("Hello")},
				Tools:      validTools,
				ToolChoice: ToolChoiceTool{Name: ""},
			},
			wantErr: "ToolChoiceTool.ToolName is required",
		},
		{
			name: "invalid - ToolChoiceTool references unknown tool",
			opts: Request{
				Model:      "gpt-4",
				Messages:   Messages{User("Hello")},
				Tools:      validTools,
				ToolChoice: ToolChoiceTool{Name: "unknown_tool"},
			},
			wantErr: `ToolChoiceTool references unknown tool "unknown_tool"`,
		},
		{
			name: "invalid - message validation fails",
			opts: Request{
				Model:    "gpt-4",
				Messages: Messages{User("")}, // empty content is invalid
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

func TestEffort_Constants(t *testing.T) {
	assert.Equal(t, Effort(""), EffortUnspecified)
	assert.Equal(t, Effort("low"), EffortLow)
	assert.Equal(t, Effort("medium"), EffortMedium)
	assert.Equal(t, Effort("high"), EffortHigh)
	assert.Equal(t, Effort("max"), EffortMax)
}

func TestEffort_Valid(t *testing.T) {
	tests := []struct {
		effort Effort
		want   bool
	}{
		{EffortUnspecified, true},
		{EffortLow, true},
		{EffortMedium, true},
		{EffortHigh, true},
		{EffortMax, true},
		{"invalid", false},
		{"HIGH", false},
		{"none", false},
		{"xhigh", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.effort), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.effort.Valid())
		})
	}
}

func TestEffort_ToBudget(t *testing.T) {
	low, high := 1024, 31999

	tests := []struct {
		effort Effort
		want   int
		ok     bool
	}{
		{EffortLow, 1024, true},
		{EffortMedium, 1024 + (31999-1024)/2, true},
		{EffortHigh, 31999, true},
		{EffortMax, 31999, true}, // same as high for budget
		{EffortUnspecified, 0, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.effort), func(t *testing.T) {
			got, ok := tt.effort.ToBudget(low, high)
			assert.Equal(t, tt.ok, ok)
			if ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestThinkingMode_Valid(t *testing.T) {
	tests := []struct {
		mode ThinkingMode
		want bool
	}{
		{ThinkingAuto, true},
		{ThinkingOn, true},
		{ThinkingOff, true},
		{"invalid", false},
		{"none", false},
		{"high", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			assert.Equal(t, tt.want, tt.mode.Valid())
		})
	}
}

func TestStreamOptions_WithEffortAndThinking(t *testing.T) {
	opts := Request{
		Model:    "gpt-5",
		Messages: Messages{User("Hello")},
		Effort:   EffortHigh,
		Thinking: ThinkingOn,
	}
	err := opts.Validate()
	require.NoError(t, err)
	assert.Equal(t, EffortHigh, opts.Effort)
	assert.Equal(t, ThinkingOn, opts.Thinking)
}

func TestToolDefinition_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tool    tool.Definition
		wantErr string
	}{
		{
			name:    "valid - with parameters",
			tool:    tool.Definition{Name: "get_weather", Parameters: map[string]any{"type": "object"}},
			wantErr: "",
		},
		{
			name:    "valid - nil parameters",
			tool:    tool.Definition{Name: "simple_tool"},
			wantErr: "",
		},
		{
			name:    "valid - empty parameters map",
			tool:    tool.Definition{Name: "empty_params", Parameters: map[string]any{}},
			wantErr: "",
		},
		{
			name:    "invalid - empty name",
			tool:    tool.Definition{Name: ""},
			wantErr: "name is required",
		},
		{
			name:    "invalid - parameters type not object",
			tool:    tool.Definition{Name: "bad_tool", Parameters: map[string]any{"type": "array"}},
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
