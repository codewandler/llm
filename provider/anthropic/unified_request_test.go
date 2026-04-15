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

	legacyReq, err := BuildRequest(RequestOptions{LLMRequest: opts})
	require.NoError(t, err)
	legacyBody, err := json.Marshal(legacyReq)
	require.NoError(t, err)

	uReq, err := unified.RequestFromLLM(opts)
	require.NoError(t, err)
	wireReq, err := unified.RequestToMessages(uReq)
	require.NoError(t, err)
	unifiedBody, err := json.Marshal(wireReq)
	require.NoError(t, err)

	var got map[string]any
	var want map[string]any
	require.NoError(t, json.Unmarshal(unifiedBody, &got))
	require.NoError(t, json.Unmarshal(legacyBody, &want))
	assert.Equal(t, want, got)
}
