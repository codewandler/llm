package openai

import (
	"encoding/json"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCompletionsBodyUnified_Parity(t *testing.T) {
	opts := llm.Request{
		Model:        "gpt-4o",
		MaxTokens:    128,
		Temperature:  0.2,
		TopP:         0.9,
		TopK:         20,
		OutputFormat: llm.OutputFormatJSON,
		Tools: []tool.Definition{{
			Name:        "search",
			Description: "Search docs",
			Parameters: map[string]any{
				"type": "object",
			},
		}},
		ToolChoice: llm.ToolChoiceRequired{},
		Effort:     llm.EffortMedium,
		CacheHint:  &msg.CacheHint{Enabled: true, TTL: "1h"},
		Messages: llm.Messages{
			llm.System("system"),
			llm.User("hello"),
		},
	}

	legacyBody, err := ccBuildRequest(opts)
	require.NoError(t, err)

	unifiedBody, err := buildCompletionsBodyUnified(opts)
	require.NoError(t, err)

	assertJSONEq(t, legacyBody, unifiedBody)
}

func TestBuildResponsesBodyUnified_Parity(t *testing.T) {
	opts := llm.Request{
		Model:        "gpt-5.4",
		MaxTokens:    256,
		Temperature:  0.1,
		TopP:         0.8,
		TopK:         10,
		OutputFormat: llm.OutputFormatJSON,
		Tools: []tool.Definition{{
			Name:        "search",
			Description: "Search docs",
			Parameters: map[string]any{
				"type": "object",
			},
		}},
		ToolChoice: llm.ToolChoiceTool{Name: "search"},
		Effort:     llm.EffortHigh,
		CacheHint:  &msg.CacheHint{Enabled: true, TTL: "1h"},
		Messages: llm.Messages{
			llm.System("sys1"),
			llm.User("hello"),
		},
	}

	legacyBody, err := respBuildRequest(opts)
	require.NoError(t, err)

	unifiedBody, err := buildResponsesBodyUnified(opts)
	require.NoError(t, err)

	assertJSONEq(t, legacyBody, unifiedBody)
}

func TestBuildResponsesBodyUnified_ErrorsOnMultipleSystemMessages(t *testing.T) {
	_, err := buildResponsesBodyUnified(llm.Request{
		Model: "gpt-5.4",
		Messages: llm.Messages{
			llm.System("sys1"),
			msg.System("sys2").Build(),
			llm.User("hello"),
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple system messages")
}

func assertJSONEq(t *testing.T, a, b []byte) {
	t.Helper()
	var ma map[string]any
	var mb map[string]any
	require.NoError(t, json.Unmarshal(a, &ma))
	require.NoError(t, json.Unmarshal(b, &mb))
	assert.Equal(t, ma, mb)
}
