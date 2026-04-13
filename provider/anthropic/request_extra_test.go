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

func TestBuildRequest_Thinking_Adaptive(t *testing.T) {
	cases := []struct {
		name               string
		model              string
		thinking           llm.ThinkingMode
		effort             llm.Effort
		expectThinkingType string
		expectBudget       bool
		expectEffort       string // expected output_config.effort
	}{
		{
			name:               "sonnet-4-6 auto unspecified",
			model:              "claude-sonnet-4-6-20251120",
			thinking:           llm.ThinkingAuto,
			effort:             llm.EffortUnspecified,
			expectThinkingType: "adaptive",
			expectEffort:       "medium",
		},
		{
			name:               "sonnet-4-6 auto high",
			model:              "claude-sonnet-4-6-20251120",
			thinking:           llm.ThinkingAuto,
			effort:             llm.EffortHigh,
			expectThinkingType: "adaptive",
			expectEffort:       "high",
		},
		{
			name:               "sonnet-4-6 auto max",
			model:              "claude-sonnet-4-6-20251120",
			thinking:           llm.ThinkingAuto,
			effort:             llm.EffortMax,
			expectThinkingType: "adaptive",
			expectEffort:       "max",
		},
		{
			name:               "sonnet-4-6 off high",
			model:              "claude-sonnet-4-6-20251120",
			thinking:           llm.ThinkingOff,
			effort:             llm.EffortHigh,
			expectThinkingType: "disabled",
			expectEffort:       "high",
		},
		{
			name:               "sonnet-4-6 off unspecified",
			model:              "claude-sonnet-4-6-20251120",
			thinking:           llm.ThinkingOff,
			effort:             llm.EffortUnspecified,
			expectThinkingType: "disabled",
			expectEffort:       "medium",
		},
		{
			name:               "sonnet-4-6 on unspecified",
			model:              "claude-sonnet-4-6-20251120",
			thinking:           llm.ThinkingOn,
			effort:             llm.EffortUnspecified,
			expectThinkingType: "adaptive",
			expectEffort:       "medium",
		},
		{
			name:               "opus-4-6 auto max",
			model:              "claude-opus-4-6-20251120",
			thinking:           llm.ThinkingAuto,
			effort:             llm.EffortMax,
			expectThinkingType: "adaptive",
			expectEffort:       "max",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := buildRequestMap(t, RequestOptions{
				LLMRequest: llm.Request{
					Model:    tc.model,
					Messages: llm.Messages{llm.User("hi")},
					Effort:   tc.effort,
					Thinking: tc.thinking,
				},
			})

			thinking, ok := m["thinking"].(map[string]any)
			require.True(t, ok, "thinking block should be present")
			assert.Equal(t, tc.expectThinkingType, thinking["type"], "thinking.type")

			_, hasBudget := thinking["budget_tokens"]
			assert.False(t, hasBudget, "adaptive models must NEVER have budget_tokens")

			oc, ok := m["output_config"].(map[string]any)
			require.True(t, ok, "output_config should be present")
			assert.Equal(t, tc.expectEffort, oc["effort"], "output_config.effort")
		})
	}
}

func TestBuildRequest_Thinking_NonAdaptive(t *testing.T) {
	cases := []struct {
		name               string
		model              string
		thinking           llm.ThinkingMode
		effort             llm.Effort
		expectThinkingType string
		expectBudget       int    // 0 means absent
		expectEffort       string // "" means output_config absent or no effort
	}{
		{
			name:               "haiku auto unspecified",
			model:              "claude-haiku-4-5-20251001",
			thinking:           llm.ThinkingAuto,
			effort:             llm.EffortUnspecified,
			expectThinkingType: "enabled",
			expectBudget:       thinkingBudgetHigh,
		},
		{
			name:               "haiku auto low",
			model:              "claude-haiku-4-5-20251001",
			thinking:           llm.ThinkingAuto,
			effort:             llm.EffortLow,
			expectThinkingType: "enabled",
			expectBudget:       thinkingBudgetLow,
		},
		{
			name:               "haiku auto high",
			model:              "claude-haiku-4-5-20251001",
			thinking:           llm.ThinkingAuto,
			effort:             llm.EffortHigh,
			expectThinkingType: "enabled",
			expectBudget:       thinkingBudgetHigh,
		},
		{
			name:               "haiku off high",
			model:              "claude-haiku-4-5-20251001",
			thinking:           llm.ThinkingOff,
			effort:             llm.EffortHigh,
			expectThinkingType: "disabled",
			expectBudget:       0,
		},
		{
			name:               "haiku on medium",
			model:              "claude-haiku-4-5-20251001",
			thinking:           llm.ThinkingOn,
			effort:             llm.EffortMedium,
			expectThinkingType: "enabled",
			expectBudget:       16511,
		},
		{
			name:               "opus-4-5 off high",
			model:              "claude-opus-4-5-20250514",
			thinking:           llm.ThinkingOff,
			effort:             llm.EffortHigh,
			expectThinkingType: "disabled",
			expectBudget:       0,
			expectEffort:       "high",
		},
		{
			name:               "opus-4-5 auto high",
			model:              "claude-opus-4-5-20250514",
			thinking:           llm.ThinkingAuto,
			effort:             llm.EffortHigh,
			expectThinkingType: "enabled",
			expectBudget:       thinkingBudgetHigh,
			expectEffort:       "high",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := buildRequestMap(t, RequestOptions{
				LLMRequest: llm.Request{
					Model:    tc.model,
					Messages: llm.Messages{llm.User("hi")},
					Effort:   tc.effort,
					Thinking: tc.thinking,
				},
			})

			thinking, ok := m["thinking"].(map[string]any)
			require.True(t, ok, "thinking block should be present")
			assert.Equal(t, tc.expectThinkingType, thinking["type"], "thinking.type")

			if tc.expectBudget > 0 {
				assert.InDelta(t, float64(tc.expectBudget), thinking["budget_tokens"], 0, "budget_tokens")
			} else {
				_, hasBudget := thinking["budget_tokens"]
				assert.False(t, hasBudget, "budget_tokens should be absent")
			}

			if tc.expectEffort != "" {
				oc, ok := m["output_config"].(map[string]any)
				require.True(t, ok, "output_config should be present")
				assert.Equal(t, tc.expectEffort, oc["effort"], "output_config.effort")
			} else {
				// output_config should be absent or have no effort
				if oc, ok := m["output_config"].(map[string]any); ok {
					assert.Empty(t, oc["effort"], "output_config.effort should not be set")
				}
			}
		})
	}
}

func TestBuildRequest_OutputConfig(t *testing.T) {
	cases := []struct {
		name         string
		model        string
		effort       llm.Effort
		expectEffort string // "" means absent
	}{
		{"haiku high", "claude-haiku-4-5-20251001", llm.EffortHigh, ""},
		{"sonnet-4-5 high", "claude-sonnet-4-5-20250514", llm.EffortHigh, ""},
		{"opus-4-5 high", "claude-opus-4-5-20250514", llm.EffortHigh, "high"},
		{"opus-4-5 max", "claude-opus-4-5-20250514", llm.EffortMax, "high"}, // max downgraded to high
		{"sonnet-4-6 max", "claude-sonnet-4-6-20251120", llm.EffortMax, "max"},
		{"opus-4-6 max", "claude-opus-4-6-20251120", llm.EffortMax, "max"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := buildRequestMap(t, RequestOptions{
				LLMRequest: llm.Request{
					Model:    tc.model,
					Messages: llm.Messages{llm.User("hi")},
					Effort:   tc.effort,
				},
			})
			oc, hasConfig := m["output_config"].(map[string]any)
			if tc.expectEffort != "" {
				require.True(t, hasConfig, "output_config should be present")
				assert.Equal(t, tc.expectEffort, oc["effort"], "output_config.effort")
			} else {
				if hasConfig {
					assert.Empty(t, oc["effort"], "output_config.effort should NOT be set for %s", tc.model)
				}
			}
		})
	}
}

func TestBuildRequest_EffortAndFormat(t *testing.T) {
	// Both Effort and OutputFormat should coexist in output_config
	m := buildRequestMap(t, RequestOptions{
		LLMRequest: llm.Request{
			Model:        "claude-sonnet-4-6-20251120",
			Messages:     llm.Messages{llm.User("hi")},
			Effort:       llm.EffortHigh,
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

func TestBuildRequest_Effort_ForcedToolChoiceDowngrade(t *testing.T) {
	m := buildRequestMap(t, RequestOptions{
		LLMRequest: llm.Request{
			Model:    "claude-sonnet-4-5",
			Messages: llm.Messages{llm.User("hi")},
			Effort:   llm.EffortHigh,
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
		{"claude-sonnet-4-5-20250514", false},
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
		{"claude-sonnet-4-6-20251120", true},
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

func TestRequest_ControlParams(t *testing.T) {
	t.Run("haiku default (enabled with high budget)", func(t *testing.T) {
		req, err := BuildRequest(RequestOptions{
			LLMRequest: llm.Request{
				Model:    "claude-haiku-4-5-20251001",
				Messages: llm.Messages{llm.User("hi")},
			},
		})
		require.NoError(t, err)
		p := req.ControlParams()

		assert.Equal(t, "claude-haiku-4-5-20251001", p["model"])
		assert.Equal(t, float64(32000), p["max_tokens"])
		assert.Equal(t, true, p["stream"])

		thinking, ok := p["thinking"].(map[string]any)
		require.True(t, ok, "thinking should be a map")
		assert.Equal(t, "enabled", thinking["type"])
		assert.Equal(t, float64(thinkingBudgetHigh), thinking["budget_tokens"])
	})

	t.Run("excludes messages, system, tools, metadata, cache_control", func(t *testing.T) {
		req, err := BuildRequest(RequestOptions{
			LLMRequest: llm.Request{
				Model:    "claude-sonnet-4-5",
				Messages: llm.Messages{llm.User("hi")},
				Tools: []tool.Definition{
					{Name: "search", Description: "search", Parameters: map[string]any{"type": "object"}},
				},
			},
			SystemBlocks: SystemBlocks{Text("you are helpful")},
		})
		require.NoError(t, err)
		p := req.ControlParams()

		_, hasMessages := p["messages"]
		_, hasSystem := p["system"]
		_, hasTools := p["tools"]
		_, hasMeta := p["metadata"]
		assert.False(t, hasMessages, "messages excluded")
		assert.False(t, hasSystem, "system excluded")
		assert.False(t, hasTools, "tools excluded")
		assert.False(t, hasMeta, "metadata excluded")

		// model and thinking should still be there
		assert.Equal(t, "claude-sonnet-4-5", p["model"])
		_, hasThinking := p["thinking"]
		assert.True(t, hasThinking)
	})

	t.Run("sonnet 4.6 default (adaptive, no budget)", func(t *testing.T) {
		req, err := BuildRequest(RequestOptions{
			LLMRequest: llm.Request{
				Model:    "claude-sonnet-4-6-20251120",
				Messages: llm.Messages{llm.User("hi")},
			},
		})
		require.NoError(t, err)
		p := req.ControlParams()

		thinking, ok := p["thinking"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "adaptive", thinking["type"])
		_, hasBudget := thinking["budget_tokens"]
		assert.False(t, hasBudget, "no budget_tokens for bare adaptive")
	})

	t.Run("sonnet 4.6 with Effort promotes to output_config", func(t *testing.T) {
		req, err := BuildRequest(RequestOptions{
			LLMRequest: llm.Request{
				Model:    "claude-sonnet-4-6-20251120",
				Messages: llm.Messages{llm.User("hi")},
				Effort:   llm.EffortHigh,
			},
		})
		require.NoError(t, err)
		p := req.ControlParams()

		thinking, ok := p["thinking"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "adaptive", thinking["type"])
		_, hasBudget := thinking["budget_tokens"]
		assert.False(t, hasBudget, "no budget_tokens on adaptive")

		oc, ok := p["output_config"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "high", oc["effort"])
	})

	t.Run("output_config with effort", func(t *testing.T) {
		req, err := BuildRequest(RequestOptions{
			LLMRequest: llm.Request{
				Model:    "claude-sonnet-4-6-20251120",
				Messages: llm.Messages{llm.User("hi")},
				Effort:   llm.EffortHigh,
			},
		})
		require.NoError(t, err)
		p := req.ControlParams()

		oc, ok := p["output_config"].(map[string]any)
		require.True(t, ok, "output_config should be present")
		assert.Equal(t, "high", oc["effort"])
	})

	t.Run("tool_choice included when set", func(t *testing.T) {
		req, err := BuildRequest(RequestOptions{
			LLMRequest: llm.Request{
				Model:    "claude-sonnet-4-5",
				Messages: llm.Messages{llm.User("hi")},
				Tools: []tool.Definition{
					{Name: "search", Description: "search", Parameters: map[string]any{"type": "object"}},
				},
				ToolChoice: llm.ToolChoiceAuto{},
			},
		})
		require.NoError(t, err)
		p := req.ControlParams()

		_, hasToolChoice := p["tool_choice"]
		assert.True(t, hasToolChoice, "tool_choice should be present")
	})
}
