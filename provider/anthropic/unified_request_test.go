package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/unified"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestUnified_Parity(t *testing.T) {
	opts := llm.Request{
		Model:        "claude-sonnet-4-6",
		MaxTokens:    256,
		Temperature:  0.2,
		TopP:         0.9,
		TopK:         20,
		OutputFormat: llm.OutputFormatJSON,
		Effort:       llm.EffortHigh,
		Thinking:     llm.ThinkingOn,
		Messages: llm.Messages{
			llm.System("system"),
			llm.User("hello"),
		},
	}

	uReq, err := unified.RequestFromLLM(opts)
	require.NoError(t, err)
	wireReq, err := unified.BuildMessagesRequest(uReq)
	require.NoError(t, err)
	unifiedBody, err := json.Marshal(wireReq)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(unifiedBody, &got))
	assert.Equal(t, 0.2, got["temperature"])
	assert.Equal(t, 0.9, got["top_p"])
	assert.Equal(t, 20.0, got["top_k"])
	require.Equal(t, "json_schema", got["output_config"].(map[string]any)["format"].(map[string]any)["type"])
	assert.Equal(t, "high", got["output_config"].(map[string]any)["effort"])
	assert.Equal(t, "adaptive", got["thinking"].(map[string]any)["type"])
	require.Len(t, got["system"].([]any), 1)
}
