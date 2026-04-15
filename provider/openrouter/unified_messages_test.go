package openrouter

import (
	"encoding/json"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMessagesRequestUnified_Fields(t *testing.T) {
	opts := llm.Request{
		Model:      "anthropic/claude-opus-4-5",
		MaxTokens:  128,
		Effort:     llm.EffortHigh,
		ToolChoice: llm.ToolChoiceRequired{},
		Tools:      []tool.Definition{{Name: "search", Description: "Search", Parameters: map[string]any{"type": "object"}}},
		Messages:   llm.Messages{llm.System("sys"), llm.User("hello")},
	}

	body, err := buildOpenRouterMessagesBodyUnified(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))

	// Anthropic-specific: strip the "anthropic/" prefix in the wire body
	assert.Equal(t, "claude-opus-4-5", req["model"],
		"messages path must strip the anthropic/ prefix")
	assert.Equal(t, true, req["stream"])
	assert.NotNil(t, req["tools"])
	assert.Equal(t, 128, int(req["max_tokens"].(float64)))
}
