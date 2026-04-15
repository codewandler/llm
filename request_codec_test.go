package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThinkingMode_TextRoundtrip(t *testing.T) {
	tests := []struct {
		input   string
		want    ThinkingMode
		wantStr string
	}{
		{"auto", ThinkingAuto, "auto"},
		{"on", ThinkingOn, "on"},
		{"off", ThinkingOff, "off"},
		{"", ThinkingAuto, "auto"}, // empty = auto
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var m ThinkingMode
			require.NoError(t, m.UnmarshalText([]byte(tt.input)))
			assert.Equal(t, tt.want, m)

			b, err := m.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.wantStr, string(b))
		})
	}
}

func TestThinkingMode_UnmarshalText_Invalid(t *testing.T) {
	var m ThinkingMode
	err := m.UnmarshalText([]byte("turbo"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid thinking mode")
}

func TestEffort_TextRoundtrip(t *testing.T) {
	tests := []struct {
		input string
		want  Effort
	}{
		{"", EffortUnspecified},
		{"low", EffortLow},
		{"medium", EffortMedium},
		{"high", EffortHigh},
		{"max", EffortMax},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var e Effort
			require.NoError(t, e.UnmarshalText([]byte(tt.input)))
			assert.Equal(t, tt.want, e)

			b, err := e.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.input, string(b))
		})
	}
}

func TestEffort_UnmarshalText_Invalid(t *testing.T) {
	var e Effort
	err := e.UnmarshalText([]byte("extreme"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid effort")
}

func TestOutputFormat_TextRoundtrip(t *testing.T) {
	tests := []struct {
		input string
		want  OutputFormat
	}{
		{"", OutputFormat("")},
		{"text", OutputFormatText},
		{"json", OutputFormatJSON},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var f OutputFormat
			require.NoError(t, f.UnmarshalText([]byte(tt.input)))
			assert.Equal(t, tt.want, f)

			b, err := f.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.input, string(b))
		})
	}
}

func TestOutputFormat_UnmarshalText_Invalid(t *testing.T) {
	var f OutputFormat
	err := f.UnmarshalText([]byte("xml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid output-format")
}

func TestParseToolChoice(t *testing.T) {
	tests := []struct {
		input string
		want  ToolChoice
	}{
		{"", ToolChoiceAuto{}},
		{"auto", ToolChoiceAuto{}},
		{"none", ToolChoiceNone{}},
		{"required", ToolChoiceRequired{}},
		{"tool:get_weather", ToolChoiceTool{Name: "get_weather"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseToolChoice(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseToolChoice_Invalid(t *testing.T) {
	for _, input := range []string{"maybe", "tool:", "Tool:x"} {
		_, err := ParseToolChoice(input)
		require.Error(t, err, "input %q should fail", input)
	}
}

func TestToolChoiceFlag_TextRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		flag ToolChoiceFlag
		text string
	}{
		{"nil", ToolChoiceFlag{}, ""},
		{"auto", ToolChoiceFlag{ToolChoiceAuto{}}, "auto"},
		{"none", ToolChoiceFlag{ToolChoiceNone{}}, "none"},
		{"required", ToolChoiceFlag{ToolChoiceRequired{}}, "required"},
		{"tool", ToolChoiceFlag{ToolChoiceTool{Name: "search"}}, "tool:search"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := tt.flag.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.text, string(b))

			var got ToolChoiceFlag
			require.NoError(t, got.UnmarshalText(b))
			assert.Equal(t, tt.flag, got)
		})
	}
}

func TestToolChoiceFlag_UnmarshalText_Invalid(t *testing.T) {
	var f ToolChoiceFlag
	err := f.UnmarshalText([]byte("maybe"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tool-choice")
}

func TestApiType_TextRoundtrip(t *testing.T) {
	// This file is package llm (internal), so types are unqualified.
	tests := []struct {
		input   string
		want    ApiType
		wantStr string
	}{
		{"auto", ApiTypeAuto, "auto"},
		{"", ApiTypeAuto, "auto"}, // empty → auto
		{"openai-chat", ApiTypeOpenAIChatCompletion, "openai-chat"},
		{"openai-responses", ApiTypeOpenAIResponses, "openai-responses"},
		{"anthropic-messages", ApiTypeAnthropicMessages, "anthropic-messages"},
		// Shortforms
		{"chat", ApiTypeOpenAIChatCompletion, "openai-chat"},         // shortform → full name
		{"responses", ApiTypeOpenAIResponses, "openai-responses"},    // shortform → full name
		{"messages", ApiTypeAnthropicMessages, "anthropic-messages"}, // shortform → full name
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var a ApiType
			require.NoError(t, a.UnmarshalText([]byte(tt.input)))
			assert.Equal(t, tt.want, a)

			b, err := a.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.wantStr, string(b))
		})
	}
}

func TestApiType_UnmarshalText_Invalid(t *testing.T) {
	var a ApiType
	err := a.UnmarshalText([]byte("bogus"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid api type")
}

func TestRequest_Validate_ApiTypeHint(t *testing.T) {
	// Valid hint: validation passes
	r := Request{Model: "m", ApiTypeHint: ApiTypeOpenAIResponses}
	require.NoError(t, r.Validate())

	// Unknown hint: validation fails with ApiTypeHint in message
	r2 := Request{Model: "m", ApiTypeHint: ApiType("not-a-valid-type")}
	err2 := r2.Validate()
	require.Error(t, err2)
	assert.Contains(t, err2.Error(), "ApiTypeHint")
}
