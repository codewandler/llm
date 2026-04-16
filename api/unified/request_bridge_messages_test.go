package unified

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/msg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildMessagesRequest(t *testing.T) {
	uReq := Request{
		Model:       "claude-opus-4-5",
		MaxTokens:   256,
		Temperature: 0.1,
		TopP:        0.8,
		TopK:        10,
		Output: &OutputSpec{
			Mode:   OutputModeJSONSchema,
			Schema: map[string]any{"type": "object"},
		},
		Metadata:  &RequestMetadata{EndUserID: "user-123"},
		CacheHint: &msg.CacheHint{Enabled: true, TTL: "1h"},
		Extras:    RequestExtras{Messages: &MessagesExtras{StopSequences: []string{"DONE"}}},
		Tools: []Tool{{
			Name:        "search",
			Description: "Search docs",
			Parameters:  map[string]any{"type": "object"},
		}},
		ToolChoice: llm.ToolChoiceTool{Name: "search"},
		Effort:     EffortLow,
		Thinking:   ThinkingOn,
		Messages: []Message{
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "system"}}},
			{Role: RoleDeveloper, Parts: []Part{{Type: PartTypeText, Text: "policy"}}},
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
	assert.Equal(t, 0.1, wire.Temperature)
	assert.Equal(t, []string{"DONE"}, wire.StopSequences)
	require.Len(t, wire.System, 2)
	require.Len(t, wire.Messages, 3)
	require.NotNil(t, wire.Thinking)
	assert.Equal(t, "enabled", wire.Thinking.Type)
	assert.Equal(t, 1024, wire.Thinking.BudgetTokens)
	require.NotNil(t, wire.Metadata)
	assert.Equal(t, "user-123", wire.Metadata.UserID)
	require.NotNil(t, wire.OutputConfig)
	require.NotNil(t, wire.OutputConfig.Format)
	assert.Equal(t, "json_schema", wire.OutputConfig.Format.Type)
	assert.Equal(t, map[string]any{"type": "object"}, wire.OutputConfig.Format.Schema)
	require.NotNil(t, wire.CacheControl)
	assert.Equal(t, "1h", wire.CacheControl.TTL)
	require.Len(t, wire.Tools, 1)
	require.NotNil(t, wire.ToolChoice)
}

func TestBuildMessagesRequest_BestEffortTrackingMetadata(t *testing.T) {
	wire, err := BuildMessagesRequest(Request{
		Model:    "claude-sonnet-4-6",
		Metadata: &RequestMetadata{EndUserID: "user-123", SessionID: "sess-1", TraceID: "trace-1", RequestID: "req-1"},
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}}},
	})
	require.NoError(t, err)
	require.NotNil(t, wire.Metadata)
	assert.Equal(t, "user-123", wire.Metadata.UserID)
}

func TestBuildMessagesRequest_PreservesCachedBlockPlacement(t *testing.T) {
	wire, err := BuildMessagesRequest(Request{
		Model: "claude-sonnet-4-6",
		Messages: []Message{{
			Role:      RoleAssistant,
			CacheHint: &msg.CacheHint{Enabled: true},
			Parts: []Part{
				{Type: PartTypeText, Text: "prefix"},
				{Type: PartTypeToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "search", Args: map[string]any{"q": "x"}}},
			},
		}},
		Extras: RequestExtras{Messages: &MessagesExtras{MessageCachePartIndex: map[int]int{0: 0}}},
	})
	require.NoError(t, err)
	require.Len(t, wire.Messages, 1)
	content := wire.Messages[0].Content.([]any)
	first := content[0].(*messages.TextBlock)
	second := content[1].(*messages.ToolUseBlock)
	require.NotNil(t, first.CacheControl)
	assert.Nil(t, second.CacheControl)
}

func TestRequestFromMessages(t *testing.T) {
	input := messages.Request{
		Model:         "claude-sonnet-4-6",
		MaxTokens:     128,
		Temperature:   0.4,
		TopP:          0.7,
		TopK:          5,
		StopSequences: []string{"DONE"},
		ToolChoice:    map[string]any{"type": "tool", "name": "search"},
		Thinking:      &messages.ThinkingConfig{Type: "adaptive", Display: "omitted"},
		Metadata:      &messages.Metadata{UserID: "user-123"},
		CacheControl:  &messages.CacheControl{Type: "ephemeral", TTL: "1h"},
		OutputConfig:  &messages.OutputConfig{Format: &messages.JSONOutputFormat{Type: "json_schema", Schema: map[string]any{"type": "object"}}, Effort: "high"},
		System: messages.SystemBlocks{
			&messages.TextBlock{Type: messages.BlockTypeText, Text: "sys", CacheControl: &messages.CacheControl{Type: "ephemeral"}},
		},
		Messages: []messages.Message{{Role: "user", Content: []any{
			map[string]any{"type": "text", "text": "hello", "cache_control": map[string]any{"type": "ephemeral"}},
			map[string]any{"type": "text", "text": "again"},
		}}},
	}

	uReq, err := RequestFromMessages(input)
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-4-6", uReq.Model)
	require.NotNil(t, uReq.Output)
	assert.Equal(t, OutputModeJSONSchema, uReq.Output.Mode)
	assert.Equal(t, EffortHigh, uReq.Effort)
	require.NotNil(t, uReq.Metadata)
	assert.Equal(t, "user-123", uReq.Metadata.EndUserID)
	require.NotNil(t, uReq.CacheHint)
	assert.Equal(t, "1h", uReq.CacheHint.TTL)
	require.NotNil(t, uReq.Extras.Messages)
	assert.Equal(t, []string{"DONE"}, uReq.Extras.Messages.StopSequences)
	assert.Equal(t, "adaptive", uReq.Extras.Messages.ThinkingType)
	assert.Equal(t, "omitted", uReq.Extras.Messages.ThinkingDisplay)
	assert.Equal(t, llm.ToolChoiceTool{Name: "search"}, uReq.ToolChoice)
	require.Len(t, uReq.Messages, 2)
	assert.Equal(t, "5m", uReq.Messages[0].CacheHint.TTL)
	require.NotNil(t, uReq.Extras.Messages.MessageCachePartIndex)
	assert.Equal(t, 0, uReq.Extras.Messages.MessageCachePartIndex[1])
}

func TestRequestFromMessages_ToolChoiceStringMap(t *testing.T) {
	uReq, err := RequestFromMessages(messages.Request{
		Model:      "claude-sonnet-4-6",
		ToolChoice: map[string]string{"type": "auto"},
		Messages:   []messages.Message{{Role: "user", Content: []any{map[string]any{"type": "text", "text": "hello"}}}},
	})
	require.NoError(t, err)
	assert.Equal(t, llm.ToolChoiceAuto{}, uReq.ToolChoice)
}
