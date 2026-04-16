package unified

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMessagesRequest(t *testing.T) {
	uReq := Request{
		Model:        "claude-opus-4-5",
		MaxTokens:    256,
		Temperature:  0.1,
		TopP:         0.8,
		TopK:         10,
		OutputFormat: OutputFormatJSON,
		Tools: []Tool{{
			Name:        "search",
			Description: "Search docs",
			Parameters:  map[string]any{"type": "object"},
		}},
		ToolChoice: llm.ToolChoiceTool{Name: "search"},
		Effort:     EffortLow,
		Thinking:   ThinkingOn,
		UserID:     "user-123",
		Messages: []Message{
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "system"}}},
			{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hello"}}},
			{Role: RoleAssistant, Parts: []Part{{Type: PartTypeThinking, Thinking: &ThinkingPart{Text: "think", Signature: "sig"}}, {Type: PartTypeText, Text: "answer"}, {Type: PartTypeToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "search", Args: map[string]any{"q": "x"}}}}},
			{Role: RoleTool, Parts: []Part{{Type: PartTypeToolResult, ToolResult: &ToolResult{ToolCallID: "call-1", ToolOutput: "result"}}}},
		},
	}

	wire, err := BuildMessagesRequest(uReq)
	require.NoError(t, err)

	assert.Equal(t, uReq.Model, wire.Model)
	assert.True(t, wire.Stream)
	assert.Equal(t, 256, wire.MaxTokens)
	require.Len(t, wire.System, 1)
	require.Len(t, wire.Messages, 3)
	require.NotNil(t, wire.Thinking)
	assert.Equal(t, "enabled", wire.Thinking.Type)
	assert.Equal(t, 1024, wire.Thinking.BudgetTokens)
	require.NotNil(t, wire.Metadata)
	assert.Equal(t, "user-123", wire.Metadata.UserID)
	require.NotNil(t, wire.OutputConfig)
	require.NotNil(t, wire.OutputConfig.Format)
	assert.Equal(t, "json_schema", wire.OutputConfig.Format.Type)
	require.Len(t, wire.Tools, 1)
	require.NotNil(t, wire.ToolChoice)
}
