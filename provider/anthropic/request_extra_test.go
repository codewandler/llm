package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
)

func buildRequestMap(t *testing.T, opts RequestOptions) map[string]any {
	t.Helper()
	b, err := BuildRequestBytes(opts)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

func TestBuildRequest_ThinkingEffort_Defaults(t *testing.T) {
	cases := []struct {
		model          string
		thinkingEffort llm.ThinkingEffort
		expectedType   string
	}{
		// Sonnet 4.6 / Opus 4.6: default to adaptive
		{"claude-sonnet-4-6-20251120", "", "adaptive"},
		{"claude-opus-4-6-20251120", "", "adaptive"},
		{"claude-sonnet-4-6-20251120", llm.ThinkingEffortNone, "disabled"},
		{"claude-opus-4-6-20251120", llm.ThinkingEffortNone, "disabled"},
		// Haiku / older models: default to enabled ThinkingConfig with high budget_tokens
		{"claude-haiku-4-5-20251001", "", "enabled"},
		{"claude-haiku-4-5-20251001", llm.ThinkingEffortNone, "disabled"},
		// Sonnet 4.5: default to enabled ThinkingConfig (like Haiku)
		{"claude-sonnet-4-5-20250514", "", "enabled"},
		{"claude-sonnet-4-5-20250514", llm.ThinkingEffortNone, "disabled"},
	}
	for _, tc := range cases {
		t.Run(tc.model+"_"+string(tc.thinkingEffort), func(t *testing.T) {
			m := buildRequestMap(t, RequestOptions{
				LLMRequest: llm.Request{
					Model:          tc.model,
					Messages:       llm.Messages{llm.User("hi")},
					ThinkingEffort: tc.thinkingEffort,
				},
			})
			thinking, ok := m["thinking"].(map[string]any)
			require.True(t, ok, "thinking block should be present")
			assert.Equal(t, tc.expectedType, thinking["type"], "thinking.type")
		})
	}
}

func TestBuildRequest_ThinkingEffort(t *testing.T) {
	cases := []struct {
		effort llm.ThinkingEffort
		budget int
	}{
		{llm.ThinkingEffortLow, 1024},
		{llm.ThinkingEffortMedium, 5000},
		{llm.ThinkingEffortHigh, 16000},
	}
	for _, tc := range cases {
		t.Run(string(tc.effort), func(t *testing.T) {
			m := buildRequestMap(t, RequestOptions{
				LLMRequest: llm.Request{
					Model:          "claude-sonnet-4-5",
					Messages:       llm.Messages{llm.User("hi")},
					ThinkingEffort: tc.effort,
				},
			})
			thinking, ok := m["thinking"].(map[string]any)
			require.True(t, ok, "thinking block should be present")
			assert.Equal(t, "enabled", thinking["type"])
			assert.InDelta(t, float64(tc.budget), thinking["budget_tokens"], 0)
		})
	}
}

func TestBuildRequest_ThinkingEffort_ForcedToolChoiceDowngrade(t *testing.T) {
	m := buildRequestMap(t, RequestOptions{
		LLMRequest: llm.Request{
			Model:          "claude-sonnet-4-5",
			Messages:       llm.Messages{llm.User("hi")},
			ThinkingEffort: llm.ThinkingEffortHigh,
			Tools: []tool.Definition{
				{Name: "my_tool", Description: "a tool", Parameters: map[string]any{"type": "object"}},
			},
			ToolChoice: llm.ToolChoiceTool{Name: "my_tool"},
		},
	})
	tc, ok := m["tool_choice"].(map[string]any)
	require.True(t, ok, "tool_choice should be present")
	assert.Equal(t, "auto", tc["type"], "forced tool_choice should be downgraded to auto when reasoning is enabled")
}

func TestBuildRequest_OutputFormatJSON(t *testing.T) {
	m := buildRequestMap(t, RequestOptions{
		LLMRequest: llm.Request{
			Model:        "claude-sonnet-4-5",
			Messages:     llm.Messages{llm.User("hi")},
			OutputFormat: llm.OutputFormatJSON,
		},
	})
	oc, ok := m["output_config"].(map[string]any)
	require.True(t, ok, "output_config should be present")
	format, ok := oc["format"].(map[string]any)
	require.True(t, ok, "output_config.format should be present")
	assert.Equal(t, "json_schema", format["type"])
}

func TestBuildRequest_MaxTokensFallback(t *testing.T) {
	baseReq := llm.Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 1000,
		Messages:  llm.Messages{llm.User("hi")},
	}

	t.Run("RequestOptions.MaxTokens used", func(t *testing.T) {
		m := buildRequestMap(t, RequestOptions{
			LLMRequest: baseReq,
		})
		assert.InDelta(t, float64(1000), m["max_tokens"], 0)
	})

	t.Run("StreamOptions.MaxTokens used when RequestOptions is zero", func(t *testing.T) {
		r := baseReq
		r.MaxTokens = 777
		m := buildRequestMap(t, RequestOptions{
			LLMRequest: r,
		})
		assert.InDelta(t, float64(777), m["max_tokens"], 0)
	})

	t.Run("default 32000 when MaxTokens is zero", func(t *testing.T) {
		r := baseReq
		r.MaxTokens = 0
		m := buildRequestMap(t, RequestOptions{
			LLMRequest: r,
		})
		assert.InDelta(t, float64(32000), m["max_tokens"], 0)
	})
}

func TestIsEffortSupported(t *testing.T) {
	cases := []struct {
		model     string
		supported bool
	}{
		{"claude-haiku-4-5-20251001", false},
		{"claude-haiku-4-6-20251120", false},
		{"claude-sonnet-4-5-20250514", false}, // Sonnet 4.5 does NOT support effort
		{"claude-sonnet-4-6-20251120", true},
		{"claude-opus-4-5-20250514", true},
		{"claude-opus-4-6-20251120", true},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			got := isEffortSupported(tc.model)
			assert.Equal(t, tc.supported, got, "isEffortSupported(%s)", tc.model)
		})
	}
}

func TestIsMaxEffortSupported(t *testing.T) {
	cases := []struct {
		model string
		maxOK bool
	}{
		{"claude-haiku-4-5-20251001", false},
		{"claude-sonnet-4-5-20250514", false},
		{"claude-sonnet-4-6-20251120", false},
		{"claude-opus-4-5-20250514", false},
		{"claude-opus-4-6-20251120", true},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			got := isMaxEffortSupported(tc.model)
			assert.Equal(t, tc.maxOK, got, "isMaxEffortSupported(%s)", tc.model)
		})
	}
}

func TestIsAdaptiveThinkingSupported(t *testing.T) {
	cases := []struct {
		model    string
		adaptive bool
	}{
		{"claude-haiku-4-5-20251001", false},
		{"claude-haiku-4-6-20251120", false},
		{"claude-sonnet-4-5-20250514", false},
		{"claude-sonnet-4-6-20251120", true},
		{"claude-opus-4-5-20250514", false},
		{"claude-opus-4-6-20251120", true},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			got := isAdaptiveThinkingSupported(tc.model)
			assert.Equal(t, tc.adaptive, got, "isAdaptiveThinkingSupported(%s)", tc.model)
		})
	}
}

func TestBuildRequest_OutputEffort(t *testing.T) {
	cases := []struct {
		model           string
		outputEffort    llm.OutputEffort
		expectEffortVal string // expected value if present, empty if not
	}{
		// Unsupported models: Haiku
		{"claude-haiku-4-5-20251001", llm.OutputEffortLow, ""},
		{"claude-haiku-4-5-20251001", llm.OutputEffortHigh, ""},
		{"claude-haiku-4-6-20251120", llm.OutputEffortLow, ""},
		// Unsupported: Sonnet 4.5 does NOT support effort
		{"claude-sonnet-4-5-20250514", llm.OutputEffortLow, ""},
		{"claude-sonnet-4-5-20250514", llm.OutputEffortHigh, ""},
		// Supported: Sonnet 4.6
		{"claude-sonnet-4-6-20251120", llm.OutputEffortLow, "low"},
		{"claude-sonnet-4-6-20251120", llm.OutputEffortMedium, "medium"},
		{"claude-sonnet-4-6-20251120", llm.OutputEffortHigh, "high"},
		{"claude-sonnet-4-6-20251120", llm.OutputEffortMax, ""}, // max only on Opus 4.6
		// Supported: Opus 4.5
		{"claude-opus-4-5-20250514", llm.OutputEffortLow, "low"},
		{"claude-opus-4-5-20250514", llm.OutputEffortHigh, "high"},
		{"claude-opus-4-5-20250514", llm.OutputEffortMax, ""}, // max only on Opus 4.6
		// Supported: Opus 4.6 (all effort levels including max)
		{"claude-opus-4-6-20251120", llm.OutputEffortLow, "low"},
		{"claude-opus-4-6-20251120", llm.OutputEffortHigh, "high"},
		{"claude-opus-4-6-20251120", llm.OutputEffortMax, "max"},
	}
	for _, tc := range cases {
		t.Run(tc.model+"_"+string(tc.outputEffort), func(t *testing.T) {
			m := buildRequestMap(t, RequestOptions{
				LLMRequest: llm.Request{
					Model:        tc.model,
					Messages:     llm.Messages{llm.User("hi")},
					OutputEffort: tc.outputEffort,
				},
			})
			oc, hasConfig := m["output_config"].(map[string]any)
			if tc.expectEffortVal != "" {
				// Expect effort to be set
				require.True(t, hasConfig, "output_config should be present")
				assert.Equal(t, tc.expectEffortVal, oc["effort"], "output_config.effort value")
			} else {
				// Expect effort to NOT be set
				if hasConfig {
					assert.Empty(t, oc["effort"], "output_config.effort should NOT be set for %s", tc.model)
				}
			}
		})
	}
}

func TestBuildRequest_OutputEffort_DefaultMedium(t *testing.T) {
	// When OutputEffort is not set, it should default to "medium" on supported models
	m := buildRequestMap(t, RequestOptions{
		LLMRequest: llm.Request{
			Model:    "claude-sonnet-4-6-20251120",
			Messages: llm.Messages{llm.User("hi")},
			// OutputEffort not set
		},
	})
	oc, ok := m["output_config"].(map[string]any)
	require.True(t, ok, "output_config should be present on supported model")
	assert.Equal(t, "medium", oc["effort"], "default effort should be medium")
}

func TestBuildRequest_OutputEffortAndFormat(t *testing.T) {
	// Both OutputEffort and OutputFormat should coexist in output_config
	m := buildRequestMap(t, RequestOptions{
		LLMRequest: llm.Request{
			Model:        "claude-sonnet-4-6-20251120",
			Messages:     llm.Messages{llm.User("hi")},
			OutputEffort: llm.OutputEffortHigh,
			OutputFormat: llm.OutputFormatJSON,
		},
	})
	oc, ok := m["output_config"].(map[string]any)
	require.True(t, ok, "output_config should be present")
	assert.Equal(t, "high", oc["effort"], "output_config.effort")
	format, ok := oc["format"].(map[string]any)
	require.True(t, ok, "output_config.format should be present")
	assert.Equal(t, "json_schema", format["type"], "output_config.format.type")
}
