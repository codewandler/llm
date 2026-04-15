package openrouter

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/api/unified"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventFromResponsesUnified_ParityWithOpenAIParserContracts(t *testing.T) {
	created := &responses.ResponseCreatedEvent{}
	ev, ignored, err := unified.EventFromResponses(created)
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Started)

	textDelta := &responses.TextDeltaEvent{OutputIndex: 0, Delta: "hello"}
	ev, ignored, err = unified.EventFromResponses(textDelta)
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	assert.Equal(t, llm.DeltaKindText, ev.Delta.Kind)

	reasoningDelta := &responses.ReasoningDeltaEvent{OutputIndex: 0, Delta: "think"}
	ev, ignored, err = unified.EventFromResponses(reasoningDelta)
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	assert.Equal(t, llm.DeltaKindThinking, ev.Delta.Kind)

	funcArgsDelta := &responses.FuncArgsDeltaEvent{OutputIndex: 1, Delta: "{}"}
	ev, ignored, err = unified.EventFromResponses(funcArgsDelta)
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	assert.Equal(t, llm.DeltaKindTool, ev.Delta.Kind)

	toolComplete := &responses.ToolCompleteEvent{ID: "call_1", Name: "search", Args: map[string]any{"q": "go"}}
	ev, ignored, err = unified.EventFromResponses(toolComplete)
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ToolCall)
	assert.Equal(t, "call_1", ev.ToolCall.ID)
	assert.Equal(t, "search", ev.ToolCall.Name)

	completed := &responses.ResponseCompletedEvent{}
	completed.Response.Status = responses.StatusIncomplete
	completed.Response.IncompleteDetails = &struct {
		Reason string "json:\"reason\""
	}{Reason: responses.ReasonMaxOutputTokens}
	ev, ignored, err = unified.EventFromResponses(completed)
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed)
	assert.Equal(t, llm.StopReasonMaxTokens, ev.Completed.StopReason)

	failed := &responses.ResponseCompletedEvent{}
	failed.Response.Status = responses.StatusCompleted
	failed.Response.Usage = &responses.ResponseUsage{InputTokens: 10, OutputTokens: 5}
	ev, ignored, err = unified.EventFromResponses(failed)
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Usage)

	apiErr := &responses.APIErrorEvent{}
	ev, ignored, err = unified.EventFromResponses(apiErr)
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Error)
}

func TestMapStopReasonParity(t *testing.T) {
	assert.Equal(t, llm.StopReasonEndTurn, openaiMapStopReasonCompat("stop"))
	assert.Equal(t, llm.StopReasonToolUse, openaiMapStopReasonCompat("tool_calls"))
	assert.Equal(t, llm.StopReasonMaxTokens, openaiMapStopReasonCompat("length"))
	assert.Equal(t, llm.StopReasonContentFilter, openaiMapStopReasonCompat("content_filter"))
	assert.Equal(t, llm.StopReason("foo"), openaiMapStopReasonCompat("foo"))
}

func openaiMapStopReasonCompat(s string) llm.StopReason {
	switch s {
	case "stop":
		return llm.StopReasonEndTurn
	case "tool_calls":
		return llm.StopReasonToolUse
	case "length":
		return llm.StopReasonMaxTokens
	case "content_filter":
		return llm.StopReasonContentFilter
	default:
		return llm.StopReason(s)
	}
}
