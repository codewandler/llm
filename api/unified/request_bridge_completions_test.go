package unified

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/completions"
	"github.com/codewandler/llm/msg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCompletionsRequest(t *testing.T) {
	uReq := Request{
		Model:      "gpt-4o",
		MaxTokens:  128,
		Effort:     EffortMedium,
		Output:     &OutputSpec{Mode: OutputModeJSONObject},
		Metadata:   &RequestMetadata{User: "user-123", Metadata: map[string]any{"session_id": "sess-1", "trace_id": "trace-1", "request_id": "req-1"}},
		CacheHint:  &msg.CacheHint{Enabled: true, TTL: "1h"},
		ToolChoice: llm.ToolChoiceRequired{},
		Tools:      []Tool{{Name: "search", Description: "Search", Parameters: map[string]any{"type": "object"}}},
		Extras: RequestExtras{Completions: &CompletionsExtras{
			Stop:              []string{"DONE"},
			N:                 2,
			PresencePenalty:   0.1,
			FrequencyPenalty:  0.2,
			LogProbs:          true,
			TopLogProbs:       3,
			Store:             true,
			ParallelToolCalls: true,
			ServiceTier:       "default",
			ExtraMetadata:     map[string]any{"custom": "value"},
		}},
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
	assert.Equal(t, "user-123", wire.User)
	assert.Equal(t, "sess-1", wire.Metadata["session_id"])
	assert.Equal(t, "trace-1", wire.Metadata["trace_id"])
	assert.Equal(t, "req-1", wire.Metadata["request_id"])
	assert.Equal(t, "value", wire.Metadata["custom"])
	assert.Equal(t, []string{"DONE"}, wire.Stop)
	assert.Equal(t, 2, wire.N)
	assert.True(t, wire.LogProbs)
	assert.True(t, wire.Store)
	assert.True(t, wire.ParallelToolCalls)
	assert.Equal(t, "default", wire.ServiceTier)
	require.Len(t, wire.Messages, 4)
}

func TestBuildCompletionsRequest_JSONSchemaRejected(t *testing.T) {
	_, err := BuildCompletionsRequest(Request{
		Model:    "gpt-4o",
		Output:   &OutputSpec{Mode: OutputModeJSONSchema, Schema: map[string]any{"type": "object"}},
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support output mode")
}

func TestRequestFromCompletions(t *testing.T) {
	input := completions.Request{
		Model:                "gpt-4o",
		MaxTokens:            128,
		Temperature:          0.3,
		TopP:                 0.8,
		TopK:                 10,
		Stop:                 []string{"DONE"},
		N:                    2,
		PresencePenalty:      0.1,
		FrequencyPenalty:     0.2,
		LogProbs:             true,
		TopLogProbs:          3,
		PromptCacheRetention: "24h",
		ResponseFormat:       &completions.ResponseFormat{Type: "json_object"},
		ReasoningEffort:      "high",
		User:                 "user-123",
		Metadata:             map[string]any{"session_id": "sess-1", "trace_id": "trace-1", "request_id": "req-1", "custom": "value"},
		Store:                true,
		ParallelToolCalls:    true,
		ServiceTier:          "default",
		Messages:             []completions.Message{{Role: "user", Content: "hi"}},
	}

	uReq, err := RequestFromCompletions(input)
	require.NoError(t, err)
	require.NotNil(t, uReq.Output)
	assert.Equal(t, OutputModeJSONObject, uReq.Output.Mode)
	assert.Equal(t, EffortHigh, uReq.Effort)
	require.NotNil(t, uReq.Metadata)
	assert.Equal(t, "user-123", uReq.Metadata.User)
	assert.Equal(t, "sess-1", uReq.Metadata.Metadata["session_id"])
	assert.Equal(t, "trace-1", uReq.Metadata.Metadata["trace_id"])
	assert.Equal(t, "req-1", uReq.Metadata.Metadata["request_id"])
	require.NotNil(t, uReq.CacheHint)
	assert.Equal(t, "1h", uReq.CacheHint.TTL)
	require.NotNil(t, uReq.Extras.Completions)
	assert.Equal(t, []string{"DONE"}, uReq.Extras.Completions.Stop)
	assert.Equal(t, "24h", uReq.Extras.Completions.PromptCacheRetention)
	assert.Equal(t, "value", uReq.Metadata.Metadata["custom"])
	assert.True(t, uReq.Extras.Completions.Store)
	assert.True(t, uReq.Extras.Completions.ParallelToolCalls)
}

func TestBuildCompletionsRequest_PreservesNativeContent(t *testing.T) {
	rawContent := []map[string]any{{"type": "input_text", "text": "hi"}}
	wire, err := BuildCompletionsRequest(Request{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Native: rawContent}}}},
	})
	require.NoError(t, err)
	require.Len(t, wire.Messages, 1)
	assert.Equal(t, any(rawContent), wire.Messages[0].Content)
}

func TestBuildCompletionsRequest_RejectsMixedNativeAndTextContent(t *testing.T) {
	_, err := BuildCompletionsRequest(Request{
		Model: "gpt-4o",
		Messages: []Message{{
			Role:  RoleUser,
			Parts: []Part{{Type: PartTypeText, Text: "hi"}, {Native: []map[string]any{{"type": "input_text", "text": "raw"}}}},
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot mix native content with canonical parts")
}

func TestBuildCompletionsRequest_RejectsMixedNativeAndToolCallContent(t *testing.T) {
	_, err := BuildCompletionsRequest(Request{
		Model: "gpt-4o",
		Messages: []Message{{
			Role:  RoleAssistant,
			Parts: []Part{{Native: []map[string]any{{"type": "input_text", "text": "raw"}}}, {Type: PartTypeToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "search", Args: map[string]any{"q": "golang"}}}},
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot mix native content with canonical parts")
}
