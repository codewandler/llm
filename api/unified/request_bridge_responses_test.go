package unified

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildResponsesRequest(t *testing.T) {
	uReq := Request{
		Model:      "gpt-5.4",
		MaxTokens:  256,
		Effort:     EffortHigh,
		ToolChoice: llm.ToolChoiceTool{Name: "search"},
		Tools:      []Tool{{Name: "search", Description: "Search", Parameters: map[string]any{"type": "object"}}},
		Messages: []Message{
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "primary system"}}},
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "secondary system"}}},
			{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}},
			{Role: RoleAssistant, Parts: []Part{{Type: PartTypeText, Text: "ok"}, {Type: PartTypeToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "search", Args: map[string]any{"q": "golang"}}}}},
			{Role: RoleTool, Parts: []Part{{Type: PartTypeToolResult, ToolResult: &ToolResult{ToolCallID: "call-1", ToolOutput: "result"}}}},
		},
	}

	wire, err := BuildResponsesRequest(uReq)
	require.NoError(t, err)

	assert.Equal(t, "gpt-5.4", wire.Model)
	assert.True(t, wire.Stream)
	assert.Equal(t, "primary system", wire.Instructions)
	require.NotEmpty(t, wire.Input)
	assert.Equal(t, "developer", wire.Input[0].Role)
	assert.Equal(t, "secondary system", wire.Input[0].Content)
	require.NotNil(t, wire.Reasoning)
	assert.Equal(t, "high", wire.Reasoning.Effort)
	require.Len(t, wire.Tools, 1)
	require.NotNil(t, wire.ToolChoice)
}
