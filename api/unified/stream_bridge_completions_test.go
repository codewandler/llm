package unified

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/completions"
	"github.com/codewandler/llm/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapCompletionsEvent(t *testing.T) {
	ev, ignored, err := MapCompletionsEvent(&completions.Chunk{ID: "chatcmpl_1", Model: "gpt-4o", Choices: []completions.Choice{{Delta: completions.Delta{Content: "hello"}}}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Started)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindText, ev.ContentDelta.Kind)
	assert.Equal(t, llm.DeltaKindText, ev.Delta.Kind)
	assert.Equal(t, "hello", ev.Delta.Text)

	ev, ignored, err = MapCompletionsEvent(&completions.Chunk{Choices: []completions.Choice{{Delta: completions.Delta{ToolCalls: []completions.ToolCallDelta{{Index: 2, ID: "call_1", Function: completions.FuncCallDelta{Name: "search", Arguments: `{"q":`}}}}}}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ToolDelta)
	assert.Equal(t, ToolDeltaKindFunctionArguments, ev.ToolDelta.Kind)
	assert.Equal(t, llm.DeltaKindTool, ev.Delta.Kind)
	require.NotNil(t, ev.Delta.Index)
	assert.Equal(t, uint32(2), *ev.Delta.Index)
	assert.Equal(t, "call_1", ev.Delta.ToolID)
	assert.Equal(t, "search", ev.Delta.ToolName)

	finish := completions.FinishReasonToolCalls
	ev, ignored, err = MapCompletionsEvent(&completions.Chunk{Choices: []completions.Choice{{FinishReason: &finish}}, Usage: &completions.Usage{PromptTokens: 12, CompletionTokens: 5}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed)
	assert.Equal(t, llm.StopReasonToolUse, ev.Completed.StopReason)
	require.NotNil(t, ev.Usage)
	assert.Equal(t, 12, ev.Usage.Tokens.Count(usage.KindInput))
	assert.Equal(t, 5, ev.Usage.Tokens.Count(usage.KindOutput))
}

func TestMapCompletionsEvent_PreservesSourceRawPayload(t *testing.T) {
	result := apicore.StreamResult{Event: &completions.Chunk{ID: "chatcmpl-1", Model: "gpt-4o"}, RawJSON: []byte(`{"id":"chatcmpl-1","model":"gpt-4o"}`)}

	ev, ignored, err := MapCompletionsEvent(result)
	require.NoError(t, err)
	require.False(t, ignored)
	assert.JSONEq(t, string(result.RawJSON), string(ev.Extras.RawJSON))
}
