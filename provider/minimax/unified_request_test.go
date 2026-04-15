package minimax

import (
	"encoding/json"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/unified"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMiniMaxRequestUnified_ThinkingOmitted(t *testing.T) {
	opts := llm.Request{
		Model:    "MiniMax-M2.7",
		Thinking: llm.ThinkingOn,
		Messages: llm.Messages{llm.User("hello")},
	}

	legacyReq, err := anthropic.BuildRequest(anthropic.RequestOptions{LLMRequest: opts})
	require.NoError(t, err)
	legacyReq = adjustThinkingForMiniMax(legacyReq)
	legacyBody, err := json.Marshal(legacyReq)
	require.NoError(t, err)

	uReq, err := unified.RequestFromLLM(opts)
	require.NoError(t, err)

	wireReq, err := unified.RequestToMessages(uReq)
	require.NoError(t, err)
	// MiniMax-specific behavior: always omit thinking field
	wireReq.Thinking = nil
	unifiedBody, err := json.Marshal(wireReq)
	require.NoError(t, err)

	var got map[string]any
	var want map[string]any
	require.NoError(t, json.Unmarshal(unifiedBody, &got))
	require.NoError(t, json.Unmarshal(legacyBody, &want))
	assert.Equal(t, want, got)
	_, hasThinking := got["thinking"]
	assert.False(t, hasThinking)
}
