package openrouter

import (
	"encoding/json"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMessagesRequestUnified_Parity(t *testing.T) {
	opts := llm.Request{
		Model:      "anthropic/claude-opus-4-5",
		MaxTokens:  128,
		Effort:     llm.EffortHigh,
		ToolChoice: llm.ToolChoiceRequired{},
		Tools:      []tool.Definition{{Name: "search", Description: "Search", Parameters: map[string]any{"type": "object"}}},
		Messages:   llm.Messages{llm.System("sys"), llm.User("hello")},
	}

	legacyBody, err := buildOpenRouterMessagesBodyLegacy(opts)
	require.NoError(t, err)

	unifiedBody, err := buildOpenRouterMessagesBodyUnified(opts)
	require.NoError(t, err)

	var got map[string]any
	var want map[string]any
	require.NoError(t, json.Unmarshal(unifiedBody, &got))
	require.NoError(t, json.Unmarshal(legacyBody, &want))
	assert.Equal(t, want, got)
}
