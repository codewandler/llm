package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildRequestMap(t *testing.T, opts RequestOptions) map[string]any {
	t.Helper()
	b, err := BuildRequest(opts)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

func TestBuildRequest_ReasoningEffort(t *testing.T) {
	cases := []struct {
		effort llm.ReasoningEffort
		budget int
	}{
		{llm.ReasoningEffortLow, 1024},
		{llm.ReasoningEffortMedium, 5000},
		{llm.ReasoningEffortHigh, 16000},
	}
	for _, tc := range cases {
		t.Run(string(tc.effort), func(t *testing.T) {
			m := buildRequestMap(t, RequestOptions{
				Model: "claude-sonnet-4-5",
				StreamOptions: llm.Request{
					Model:           "claude-sonnet-4-5",
					Messages:        llm.Messages{llm.User("hi")},
					ReasoningEffort: tc.effort,
				},
			})
			thinking, ok := m["thinking"].(map[string]any)
			require.True(t, ok, "thinking block should be present")
			assert.Equal(t, "enabled", thinking["type"])
			assert.InDelta(t, float64(tc.budget), thinking["budget_tokens"], 0)
		})
	}
}

func TestBuildRequest_ReasoningEffort_ForcedToolChoiceDowngrade(t *testing.T) {
	m := buildRequestMap(t, RequestOptions{
		Model: "claude-sonnet-4-5",
		StreamOptions: llm.Request{
			Model:           "claude-sonnet-4-5",
			Messages:        llm.Messages{llm.User("hi")},
			ReasoningEffort: llm.ReasoningEffortHigh,
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
		Model: "claude-sonnet-4-5",
		StreamOptions: llm.Request{
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
		Model:    "claude-sonnet-4-5",
		Messages: llm.Messages{llm.User("hi")},
	}

	t.Run("RequestOptions.MaxTokens used", func(t *testing.T) {
		m := buildRequestMap(t, RequestOptions{
			Model:         "claude-sonnet-4-5",
			MaxTokens:     999,
			StreamOptions: baseReq,
		})
		assert.InDelta(t, float64(999), m["max_tokens"], 0)
	})

	t.Run("StreamOptions.MaxTokens used when RequestOptions is zero", func(t *testing.T) {
		r := baseReq
		r.MaxTokens = 777
		m := buildRequestMap(t, RequestOptions{
			Model:         "claude-sonnet-4-5",
			StreamOptions: r,
		})
		assert.InDelta(t, float64(777), m["max_tokens"], 0)
	})

	t.Run("default 16384 when both are zero", func(t *testing.T) {
		m := buildRequestMap(t, RequestOptions{
			Model:         "claude-sonnet-4-5",
			StreamOptions: baseReq,
		})
		assert.InDelta(t, float64(16384), m["max_tokens"], 0)
	})
}

func TestPrependSystemBlocks(t *testing.T) {
	prefix := []SystemBlock{{Type: "text", Text: "prefix"}}
	user := []SystemBlock{{Type: "text", Text: "user"}}
	result := PrependSystemBlocks(prefix, user)
	require.Len(t, result, 2)
	assert.Equal(t, "prefix", result[0].Text)
	assert.Equal(t, "user", result[1].Text)
}

func TestPrependSystemBlocks_EmptyPrefix(t *testing.T) {
	user := []SystemBlock{{Type: "text", Text: "only"}}
	result := PrependSystemBlocks(nil, user)
	require.Len(t, result, 1)
	assert.Equal(t, "only", result[0].Text)
}

func TestNewSystemBlock(t *testing.T) {
	sb := NewSystemBlock("hello world")
	assert.Equal(t, "text", sb.Type)
	assert.Equal(t, "hello world", sb.Text)
	assert.Nil(t, sb.CacheControl)
}

func TestFindPrecedingAssistant_NoPreceding(t *testing.T) {
	msgs := llm.Messages{
		llm.User("hi"),
		llm.User("bye"),
	}
	result := FindPrecedingAssistant(msgs, 1)
	assert.Nil(t, result)
}

func TestFindPrecedingAssistant_WithPreceding(t *testing.T) {
	am := llm.Assistant("I am the assistant")
	msgs := llm.Messages{
		llm.User("hi"),
		am,
		llm.User("bye"),
	}
	result := FindPrecedingAssistant(msgs, 2)
	require.NotNil(t, result)
	assert.Equal(t, "I am the assistant", result.Content())
}
