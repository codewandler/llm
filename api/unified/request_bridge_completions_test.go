package unified

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCompletionsRequest(t *testing.T) {
	uReq := Request{
		Model:      "gpt-4o",
		MaxTokens:  128,
		Effort:     EffortMedium,
		CacheHint:  &msg.CacheHint{Enabled: true, TTL: "1h"},
		ToolChoice: llm.ToolChoiceRequired{},
		Tools:      []Tool{{Name: "search", Description: "Search", Parameters: map[string]any{"type": "object"}}},
		Messages: []Message{
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "sys"}}},
			{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}},
			{Role: RoleAssistant, Parts: []Part{{Type: PartTypeText, Text: "working"}, {Type: PartTypeToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "search", Args: map[string]any{"q": "golang"}}}}},
			{Role: RoleTool, Parts: []Part{{Type: PartTypeToolResult, ToolResult: &ToolResult{ToolCallID: "call-1", ToolOutput: "done"}}}},
		},
	}

	wire, err := BuildCompletionsRequest(uReq)
	require.NoError(t, err)

	assert.Equal(t, "gpt-4o", wire.Model)
	assert.True(t, wire.Stream)
	require.NotNil(t, wire.StreamOptions)
	assert.True(t, wire.StreamOptions.IncludeUsage)
	assert.Equal(t, "required", wire.ToolChoice)
	assert.Equal(t, "24h", wire.PromptCacheRetention)
	require.Len(t, wire.Messages, 4)
}
