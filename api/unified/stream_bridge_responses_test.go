package unified

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapResponsesEvent(t *testing.T) {
	ev, ignored, err := MapResponsesEvent(&responses.ResponseCreatedEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Started)
	assert.Equal(t, responses.EventResponseCreated, ev.Extras.RawEventName)

	ev, ignored, err = MapResponsesEvent(&responses.ResponseQueuedEvent{Response: responses.ResponsePayload{ID: "resp_1"}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Lifecycle)
	assert.Equal(t, LifecycleScopeResponse, ev.Lifecycle.Scope)
	assert.Equal(t, LifecycleStateQueued, ev.Lifecycle.State)

	ev, ignored, err = MapResponsesEvent(&responses.OutputTextDeltaEvent{ContentRef: responses.ContentRef{OutputIndex: 4}, Delta: "pong"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindText, ev.ContentDelta.Kind)
	assert.Equal(t, ContentVariantPrimary, ev.ContentDelta.Variant)
	assert.Equal(t, llm.DeltaKindText, ev.Delta.Kind)
	require.NotNil(t, ev.Delta.Index)
	assert.Equal(t, uint32(4), *ev.Delta.Index)

	ev, ignored, err = MapResponsesEvent(&responses.ReasoningTextDeltaEvent{OutputRef: responses.OutputRef{OutputIndex: 1}, Delta: "reason"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentVariantRaw, ev.ContentDelta.Variant)
	assert.Equal(t, llm.DeltaKindThinking, ev.Delta.Kind)

	ev, ignored, err = MapResponsesEvent(&responses.ReasoningSummaryTextDeltaEvent{OutputRef: responses.OutputRef{OutputIndex: 2}, Delta: "summary"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindReasoning, ev.ContentDelta.Kind)
	assert.Equal(t, ContentVariantSummary, ev.ContentDelta.Variant)

	ev, ignored, err = MapResponsesEvent(&responses.RefusalDoneEvent{ContentRef: responses.ContentRef{OutputIndex: 0, ContentIndex: 1}, Refusal: "no"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.StreamContent)
	assert.Equal(t, ContentKindRefusal, ev.StreamContent.Kind)

	ev, ignored, err = MapResponsesEvent(&responses.OutputTextAnnotationAddedEvent{ContentRef: responses.ContentRef{OutputIndex: 0, ContentIndex: 1}, AnnotationIndex: 3, Annotation: responses.OutputTextAnnotation{Type: "file_citation", FileID: "file_1"}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Annotation)
	assert.Equal(t, "file_citation", ev.Annotation.Type)
	require.NotNil(t, ev.Annotation.Ref.AnnotationIndex)
	assert.Equal(t, uint32(3), *ev.Annotation.Ref.AnnotationIndex)

	ev, ignored, err = MapResponsesEvent(&responses.FunctionCallArgumentsDeltaEvent{OutputRef: responses.OutputRef{OutputIndex: 3}, Delta: "{}"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ToolDelta)
	assert.Equal(t, llm.DeltaKindTool, ev.Delta.Kind)

	ev, ignored, err = MapResponsesEvent(&responses.FunctionCallArgumentsDoneEvent{OutputRef: responses.OutputRef{ItemID: "call_1"}, Name: "lookup", Arguments: `{"a":1}`})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ToolCall)
	require.NotNil(t, ev.StreamToolCall)
	assert.Equal(t, "call_1", ev.ToolCall.ID)
	assert.Equal(t, `{"a":1}`, ev.StreamToolCall.RawInput)

	ev, ignored, err = MapResponsesEvent(&responses.CustomToolCallInputDoneEvent{OutputRef: responses.OutputRef{OutputIndex: 7, ItemID: "cust_1"}, Input: "raw-input"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ToolDelta)
	assert.True(t, ev.ToolDelta.Final)
	assert.Equal(t, ToolDeltaKindCustomInput, ev.ToolDelta.Kind)

	ev, ignored, err = MapResponsesEvent(&responses.AudioTranscriptDeltaEvent{ResponseRef: responses.ResponseRef{ResponseID: "resp_1"}, Delta: "hello"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindMedia, ev.ContentDelta.Kind)
	assert.Equal(t, ContentVariantTranscript, ev.ContentDelta.Variant)

	ev, ignored, err = MapResponsesEvent(&responses.ResponseCompletedEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed)
	require.NotNil(t, ev.Lifecycle)
	assert.Equal(t, LifecycleStateDone, ev.Lifecycle.State)

	ev, ignored, err = MapResponsesEvent(&responses.ResponseFailedEvent{Response: responses.ResponsePayload{ID: "resp_1", Error: &responses.ResponseError{Code: "server_error", Message: "boom"}}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed)
	require.NotNil(t, ev.Error)
	require.NotNil(t, ev.Lifecycle)
	assert.Equal(t, LifecycleStateFailed, ev.Lifecycle.State)

	ev, ignored, err = MapResponsesEvent(&responses.APIErrorEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Error)

	ev, ignored, err = MapResponsesEvent(&responses.OutputItemAddedEvent{OutputIndex: 1, Item: responses.ResponseOutputItem{ID: "it_1", Type: "message"}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Lifecycle)
	assert.Equal(t, LifecycleScopeItem, ev.Lifecycle.Scope)
	assert.Equal(t, LifecycleStateAdded, ev.Lifecycle.State)

	ev, ignored, err = MapResponsesEvent(apicore.StreamResult{Event: &responses.WebSearchCallInProgressEvent{OutputRef: responses.OutputRef{OutputIndex: 9, ItemID: "ws_1"}}, RawEventName: responses.EventWebSearchCallInProgress, RawJSON: []byte(`{"output_index":9,"item_id":"ws_1"}`)})
	require.NoError(t, err)
	require.False(t, ignored)
	assert.Equal(t, StreamEventUnknown, ev.Type)
	assert.Equal(t, responses.EventWebSearchCallInProgress, ev.Extras.RawEventName)
	assert.NotEmpty(t, ev.Extras.RawJSON)
}

func TestMapResponsesEvent_PreservesSourceRawPayload(t *testing.T) {
	result := apicore.StreamResult{Event: &responses.OutputTextDeltaEvent{ContentRef: responses.ContentRef{OutputIndex: 4}, Delta: "pong"}, RawEventName: responses.EventOutputTextDelta, RawJSON: []byte(`{"output_index":4,"delta":"pong"}`)}

	ev, ignored, err := MapResponsesEvent(result)
	require.NoError(t, err)
	require.False(t, ignored)
	assert.Equal(t, responses.EventOutputTextDelta, ev.Extras.RawEventName)
	assert.JSONEq(t, string(result.RawJSON), string(ev.Extras.RawJSON))
}
