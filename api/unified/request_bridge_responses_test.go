package unified

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/msg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildResponsesRequest(t *testing.T) {
	useInstructions := false
	uReq := Request{
		Model:      "gpt-5.4",
		MaxTokens:  256,
		Effort:     EffortHigh,
		Output:     &OutputSpec{Mode: OutputModeJSONObject},
		Metadata:   &RequestMetadata{User: "user-123", Metadata: map[string]any{"session_id": "sess-1", "trace_id": "trace-1", "request_id": "req-1"}},
		CacheHint:  &msg.CacheHint{Enabled: true, TTL: "1h"},
		ToolChoice: llm.ToolChoiceTool{Name: "search"},
		Tools:      []Tool{{Name: "search", Description: "Search", Parameters: map[string]any{"type": "object"}}},
		Extras: RequestExtras{Responses: &ResponsesExtras{
			PreviousResponseID: "resp_prev",
			ReasoningSummary:   "concise",
			Store:              true,
			ParallelToolCalls:  true,
			UseInstructions:    &useInstructions,
			UsedMaxTokenField:  "max_tokens",
			ExtraMetadata:      map[string]any{"custom": "value"},
		}},
		Messages: []Message{
			{Role: RoleDeveloper, Parts: []Part{{Type: PartTypeText, Text: "secondary system"}}},
			{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}},
			{Role: RoleAssistant, Phase: AssistantPhaseCommentary, Parts: []Part{{Type: PartTypeText, Text: "ok"}, {Type: PartTypeToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "search", Args: map[string]any{"q": "golang"}}}}},
			{Role: RoleTool, Parts: []Part{{Type: PartTypeToolResult, ToolResult: &ToolResult{ToolCallID: "call-1", ToolOutput: "result"}}}},
		},
	}

	wire, err := BuildResponsesRequest(uReq)
	require.NoError(t, err)

	assert.Equal(t, "gpt-5.4", wire.Model)
	assert.True(t, wire.Stream)
	assert.Empty(t, wire.Instructions)
	assert.Equal(t, 256, wire.MaxTokens)
	assert.Equal(t, "resp_prev", wire.PreviousResponseID)
	assert.Equal(t, "24h", wire.PromptCacheRetention)
	assert.Equal(t, "user-123", wire.User)
	assert.Equal(t, "sess-1", wire.Metadata["session_id"])
	assert.Equal(t, "trace-1", wire.Metadata["trace_id"])
	assert.Equal(t, "req-1", wire.Metadata["request_id"])
	assert.Equal(t, "value", wire.Metadata["custom"])
	require.NotEmpty(t, wire.Input)
	assert.Equal(t, "developer", wire.Input[0].Role)
	assert.Equal(t, "secondary system", wire.Input[0].Content)
	assert.Equal(t, "commentary", wire.Input[2].Phase)
	assert.Equal(t, "commentary", wire.Input[3].Phase)
	require.NotNil(t, wire.Reasoning)
	assert.Equal(t, "high", wire.Reasoning.Effort)
	assert.Equal(t, "concise", wire.Reasoning.Summary)
	assert.True(t, wire.Store)
	assert.True(t, wire.ParallelToolCalls)
	require.Len(t, wire.Tools, 1)
	require.NotNil(t, wire.ToolChoice)
}

func TestBuildResponsesRequest_UsesInstructionsForLeadingSystemMessage(t *testing.T) {
	wire, err := BuildResponsesRequest(Request{
		Model: "gpt-5.4",
		Messages: []Message{
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "primary system"}}},
			{Role: RoleDeveloper, Parts: []Part{{Type: PartTypeText, Text: "secondary system"}}},
			{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "primary system", wire.Instructions)
	require.Len(t, wire.Input, 2)
	assert.Equal(t, "developer", wire.Input[0].Role)
	assert.Equal(t, "secondary system", wire.Input[0].Content)
}

func TestBuildResponsesRequest_JSONSchemaRejected(t *testing.T) {
	_, err := BuildResponsesRequest(Request{
		Model:    "gpt-5.4",
		Output:   &OutputSpec{Mode: OutputModeJSONSchema, Schema: map[string]any{"type": "object"}},
		Messages: []Message{{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support output mode")
}

func TestBuildResponsesRequest_ErrorsOnSystemMessageWhenUseInstructionsDisabled(t *testing.T) {
	useInstructions := false
	_, err := BuildResponsesRequest(Request{
		Model:    "gpt-5.4",
		Extras:   RequestExtras{Responses: &ResponsesExtras{UseInstructions: &useInstructions}},
		Messages: []Message{{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "primary system"}}}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UseInstructions=false")
}

func TestBuildResponsesRequest_ErrorsOnMultipleSystemMessages(t *testing.T) {
	_, err := BuildResponsesRequest(Request{
		Model: "gpt-5.4",
		Messages: []Message{
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "one"}}},
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "two"}}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple system messages")
}

func TestBuildResponsesRequest_ErrorsOnNonLeadingSystemMessage(t *testing.T) {
	_, err := BuildResponsesRequest(Request{
		Model: "gpt-5.4",
		Messages: []Message{
			{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}},
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "late system"}}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires system message to be first")
}

func TestBuildResponsesRequest_ErrorsOnNonTextSystemMessage(t *testing.T) {
	_, err := BuildResponsesRequest(Request{
		Model: "gpt-5.4",
		Messages: []Message{{
			Role:  RoleSystem,
			Parts: []Part{{Type: PartTypeToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "search", Args: map[string]any{"q": "golang"}}}},
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text-only")
}

func TestBuildResponsesRequest_ErrorsOnAssistantTextToolTextInterleaving(t *testing.T) {
	_, err := BuildResponsesRequest(Request{
		Model: "gpt-5.4",
		Messages: []Message{{
			Role: RoleAssistant,
			Parts: []Part{
				{Type: PartTypeText, Text: "before"},
				{Type: PartTypeToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "search", Args: map[string]any{"q": "golang"}}},
				{Type: PartTypeText, Text: "after"},
			},
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "text after tool calls")
}

func TestBuildResponsesRequest_ErrorsOnAssistantThinkingPart(t *testing.T) {
	_, err := BuildResponsesRequest(Request{
		Model: "gpt-5.4",
		Messages: []Message{{
			Role:  RoleAssistant,
			Parts: []Part{{Type: PartTypeThinking, Thinking: &ThinkingPart{Text: "think"}}},
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support thinking parts")
}

func TestBuildResponsesRequest_ErrorsOnAssistantNativePart(t *testing.T) {
	_, err := BuildResponsesRequest(Request{
		Model: "gpt-5.4",
		Messages: []Message{{
			Role:  RoleAssistant,
			Parts: []Part{{Native: map[string]any{"type": "output_text", "text": "hi"}}},
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support native parts")
}

func TestRequestFromResponses(t *testing.T) {
	input := responses.Request{
		Model:                "gpt-5.4",
		MaxTokens:            256,
		Temperature:          0.2,
		TopP:                 0.7,
		TopK:                 12,
		ResponseFormat:       &responses.ResponseFormat{Type: "json_object"},
		PromptCacheRetention: "24h",
		PreviousResponseID:   "resp_prev",
		Reasoning:            &responses.Reasoning{Effort: "high", Summary: "concise"},
		User:                 "user-123",
		Metadata:             map[string]any{"session_id": "sess-1", "trace_id": "trace-1", "request_id": "req-1", "custom": "value"},
		Store:                true,
		ParallelToolCalls:    true,
		Instructions:         "primary system",
		Input: []responses.Input{
			{Role: "developer", Content: "secondary system"},
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "ok", Phase: "commentary"},
			{Type: "function_call", CallID: "call-1", Name: "search", Arguments: `{"q":"golang"}`, Phase: "commentary"},
			{Type: "function_call_output", CallID: "call-1", Output: "result"},
		},
	}

	uReq, err := RequestFromResponses(input)
	require.NoError(t, err)
	assert.Equal(t, 256, uReq.MaxTokens)
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
	require.NotNil(t, uReq.Extras.Responses)
	assert.Equal(t, "resp_prev", uReq.Extras.Responses.PreviousResponseID)
	assert.Equal(t, "concise", uReq.Extras.Responses.ReasoningSummary)
	assert.Equal(t, "24h", uReq.Extras.Responses.PromptCacheRetention)
	assert.Equal(t, "max_tokens", uReq.Extras.Responses.UsedMaxTokenField)
	assert.True(t, uReq.Extras.Responses.Store)
	assert.True(t, uReq.Extras.Responses.ParallelToolCalls)
	require.NotNil(t, uReq.Extras.Responses.UseInstructions)
	assert.True(t, *uReq.Extras.Responses.UseInstructions)
	require.Len(t, uReq.Messages, 5)
	assert.Equal(t, RoleSystem, uReq.Messages[0].Role)
	assert.Equal(t, RoleDeveloper, uReq.Messages[1].Role)
	assert.Equal(t, AssistantPhaseCommentary, uReq.Messages[3].Phase)
	require.Len(t, uReq.Messages[3].Parts, 2)
	assert.Equal(t, PartTypeToolCall, uReq.Messages[3].Parts[1].Type)
	assert.Equal(t, "value", uReq.Metadata.Metadata["custom"])
}

func TestRequestFromResponses_GroupsAssistantTextAndMultipleToolCalls(t *testing.T) {
	uReq, err := RequestFromResponses(responses.Request{
		Model: "gpt-5.4",
		Input: []responses.Input{
			{Role: "assistant", Content: "working"},
			{Type: "function_call", CallID: "call-1", Name: "search", Arguments: `{"q":"golang"}`},
			{Type: "function_call", CallID: "call-2", Name: "lookup", Arguments: `{"id":"42"}`},
		},
	})
	require.NoError(t, err)
	require.Len(t, uReq.Messages, 1)
	assert.Equal(t, RoleAssistant, uReq.Messages[0].Role)
	require.Len(t, uReq.Messages[0].Parts, 3)
	assert.Equal(t, PartTypeToolCall, uReq.Messages[0].Parts[1].Type)
	assert.Equal(t, PartTypeToolCall, uReq.Messages[0].Parts[2].Type)
}

func TestRequestFromResponses_FlushesAssistantTurnBeforeToolOutput(t *testing.T) {
	uReq, err := RequestFromResponses(responses.Request{
		Model: "gpt-5.4",
		Input: []responses.Input{
			{Role: "assistant", Content: "working"},
			{Type: "function_call", CallID: "call-1", Name: "search", Arguments: `{"q":"golang"}`},
			{Type: "function_call_output", CallID: "call-1", Output: "result"},
		},
	})
	require.NoError(t, err)
	require.Len(t, uReq.Messages, 2)
	assert.Equal(t, RoleAssistant, uReq.Messages[0].Role)
	assert.Equal(t, RoleTool, uReq.Messages[1].Role)
}

func TestRequestFromResponses_DoesNotMergeAcrossUserBoundary(t *testing.T) {
	uReq, err := RequestFromResponses(responses.Request{
		Model: "gpt-5.4",
		Input: []responses.Input{
			{Role: "assistant", Content: "working"},
			{Role: "user", Content: "next"},
			{Type: "function_call", CallID: "call-1", Name: "search", Arguments: `{"q":"golang"}`},
		},
	})
	require.NoError(t, err)
	require.Len(t, uReq.Messages, 3)
	assert.Equal(t, RoleAssistant, uReq.Messages[0].Role)
	assert.Equal(t, RoleUser, uReq.Messages[1].Role)
	assert.Equal(t, RoleAssistant, uReq.Messages[2].Role)
	require.Len(t, uReq.Messages[2].Parts, 1)
	assert.Equal(t, PartTypeToolCall, uReq.Messages[2].Parts[0].Type)
}

func TestRequestFromResponses_FunctionCallWithoutAssistantTextCreatesAssistantTurn(t *testing.T) {
	uReq, err := RequestFromResponses(responses.Request{
		Model: "gpt-5.4",
		Input: []responses.Input{{Type: "function_call", CallID: "call-1", Name: "search", Arguments: `{"q":"golang"}`}},
	})
	require.NoError(t, err)
	require.Len(t, uReq.Messages, 1)
	assert.Equal(t, RoleAssistant, uReq.Messages[0].Role)
	require.Len(t, uReq.Messages[0].Parts, 1)
	assert.Equal(t, PartTypeToolCall, uReq.Messages[0].Parts[0].Type)
}

func TestRequestFromResponses_SplitsAssistantTurnsOnPhaseChange(t *testing.T) {
	uReq, err := RequestFromResponses(responses.Request{
		Model: "gpt-5.4",
		Input: []responses.Input{
			{Role: "assistant", Content: "thinking", Phase: "commentary"},
			{Type: "function_call", CallID: "call-1", Name: "search", Arguments: `{"q":"golang"}`, Phase: "commentary"},
			{Role: "assistant", Content: "final", Phase: "final_answer"},
		},
	})
	require.NoError(t, err)
	require.Len(t, uReq.Messages, 2)
	assert.Equal(t, AssistantPhaseCommentary, uReq.Messages[0].Phase)
	assert.Equal(t, AssistantPhaseFinalAnswer, uReq.Messages[1].Phase)
}

func TestRequestFromResponses_PreservesToolCallOnlyAssistantPhase(t *testing.T) {
	uReq, err := RequestFromResponses(responses.Request{
		Model: "gpt-5.4",
		Input: []responses.Input{{Type: "function_call", CallID: "call-1", Name: "search", Arguments: `{"q":"golang"}`, Phase: "commentary"}},
	})
	require.NoError(t, err)
	require.Len(t, uReq.Messages, 1)
	assert.Equal(t, AssistantPhaseCommentary, uReq.Messages[0].Phase)
}
