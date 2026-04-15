package responses_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func handler() apicore.EventHandler { return responses.NewParser()() }

func fixture(t *testing.T, name string) *http.Client {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err, "missing fixture %s", name)
	return &http.Client{Transport: apicore.FixedSSEResponse(200, string(data))}
}

type parserCase struct {
	name    string
	payload string
	want    any
	done    bool
	check   func(*testing.T, any)
}

func TestParser_DocumentedEventMatrix(t *testing.T) {
	tests := []parserCase{
		{
			name:    responses.EventResponseCreated,
			payload: `{"response":{"id":"resp_01","model":"gpt-4o-mini"}}`,
			want:    &responses.ResponseCreatedEvent{},
			check: func(t *testing.T, ev any) {
				e := ev.(*responses.ResponseCreatedEvent)
				assert.Equal(t, "resp_01", e.Response.ID)
				assert.Equal(t, "gpt-4o-mini", e.Response.Model)
			},
		},
		{name: responses.EventResponseInProgress, payload: `{"response":{"id":"resp_01","status":"in_progress"}}`, want: &responses.ResponseInProgressEvent{}},
		{name: responses.EventResponseCompleted, payload: `{"response":{"id":"resp_01","model":"gpt-4o-mini","status":"completed","usage":{"input_tokens":10,"output_tokens":5}}}`, want: &responses.ResponseCompletedEvent{}, done: true},
		{name: responses.EventResponseFailed, payload: `{"response":{"id":"resp_01","status":"failed","error":{"code":"server_error","message":"boom"}}}`, want: &responses.ResponseFailedEvent{}, done: true},
		{name: responses.EventResponseIncomplete, payload: `{"response":{"id":"resp_01","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`, want: &responses.ResponseIncompleteEvent{}, done: true},
		{name: responses.EventResponseQueued, payload: `{"response":{"id":"resp_01","status":"queued"}}`, want: &responses.ResponseQueuedEvent{}},
		{name: responses.EventOutputItemAdded, payload: `{"output_index":0,"item":{"id":"msg_1","type":"message","status":"in_progress","role":"assistant"}}`, want: &responses.OutputItemAddedEvent{}},
		{name: responses.EventOutputItemDone, payload: `{"output_index":0,"item":{"id":"msg_1","type":"message","status":"completed","role":"assistant"}}`, want: &responses.OutputItemDoneEvent{}},
		{name: responses.EventContentPartAdded, payload: `{"item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":"hello","annotations":[]}}`, want: &responses.ContentPartAddedEvent{}},
		{name: responses.EventContentPartDone, payload: `{"item_id":"msg_1","output_index":0,"content_index":0,"part":{"type":"output_text","text":"hello","annotations":[]}}`, want: &responses.ContentPartDoneEvent{}},
		{name: responses.EventOutputTextDelta, payload: `{"item_id":"msg_1","output_index":0,"content_index":0,"delta":"hel","sequence_number":1}`, want: &responses.OutputTextDeltaEvent{}},
		{name: responses.EventOutputTextDone, payload: `{"item_id":"msg_1","output_index":0,"content_index":0,"text":"hello","sequence_number":2}`, want: &responses.OutputTextDoneEvent{}},
		{name: responses.EventOutputTextAnnotationAdded, payload: `{"item_id":"msg_1","output_index":0,"content_index":0,"annotation_index":0,"annotation":{"type":"file_citation","file_id":"file_1","filename":"a.txt","index":0}}`, want: &responses.OutputTextAnnotationAddedEvent{}},
		{name: responses.EventRefusalDelta, payload: `{"item_id":"msg_1","output_index":0,"content_index":0,"delta":"can't do that"}`, want: &responses.RefusalDeltaEvent{}},
		{name: responses.EventRefusalDone, payload: `{"item_id":"msg_1","output_index":0,"content_index":0,"refusal":"can't do that"}`, want: &responses.RefusalDoneEvent{}},
		{name: responses.EventFunctionCallArgumentsDelta, payload: `{"item_id":"call_1","output_index":0,"delta":"{\"city\":"}`, want: &responses.FunctionCallArgumentsDeltaEvent{}},
		{name: responses.EventFunctionCallArgumentsDone, payload: `{"item_id":"call_1","name":"get_weather","output_index":0,"arguments":"{\"city\":\"Berlin\"}"}`, want: &responses.FunctionCallArgumentsDoneEvent{}},
		{name: responses.EventFileSearchCallInProgress, payload: `{"item_id":"fs_1","output_index":0}`, want: &responses.FileSearchCallInProgressEvent{}},
		{name: responses.EventFileSearchCallSearching, payload: `{"item_id":"fs_1","output_index":0}`, want: &responses.FileSearchCallSearchingEvent{}},
		{name: responses.EventFileSearchCallCompleted, payload: `{"item_id":"fs_1","output_index":0}`, want: &responses.FileSearchCallCompletedEvent{}},
		{name: responses.EventWebSearchCallInProgress, payload: `{"item_id":"ws_1","output_index":0}`, want: &responses.WebSearchCallInProgressEvent{}},
		{name: responses.EventWebSearchCallSearching, payload: `{"item_id":"ws_1","output_index":0}`, want: &responses.WebSearchCallSearchingEvent{}},
		{name: responses.EventWebSearchCallCompleted, payload: `{"item_id":"ws_1","output_index":0}`, want: &responses.WebSearchCallCompletedEvent{}},
		{name: responses.EventReasoningSummaryPartAdded, payload: `{"item_id":"rs_1","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":"step one"}}`, want: &responses.ReasoningSummaryPartAddedEvent{}},
		{name: responses.EventReasoningSummaryPartDone, payload: `{"item_id":"rs_1","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":"step one"}}`, want: &responses.ReasoningSummaryPartDoneEvent{}},
		{name: responses.EventReasoningSummaryTextDelta, payload: `{"item_id":"rs_1","output_index":0,"delta":"think"}`, want: &responses.ReasoningSummaryTextDeltaEvent{}},
		{name: responses.EventReasoningSummaryTextDone, payload: `{"item_id":"rs_1","output_index":0,"text":"thinking"}`, want: &responses.ReasoningSummaryTextDoneEvent{}},
		{name: responses.EventReasoningTextDelta, payload: `{"item_id":"rs_1","output_index":0,"delta":"hidden"}`, want: &responses.ReasoningTextDeltaEvent{}},
		{name: responses.EventReasoningTextDone, payload: `{"item_id":"rs_1","output_index":0,"text":"hidden chain"}`, want: &responses.ReasoningTextDoneEvent{}},
		{name: responses.EventImageGenerationCallCompleted, payload: `{"item_id":"img_1","output_index":0}`, want: &responses.ImageGenerationCallCompletedEvent{}},
		{name: responses.EventImageGenerationCallGenerating, payload: `{"item_id":"img_1","output_index":0}`, want: &responses.ImageGenerationCallGeneratingEvent{}},
		{name: responses.EventImageGenerationCallInProgress, payload: `{"item_id":"img_1","output_index":0}`, want: &responses.ImageGenerationCallInProgressEvent{}},
		{name: responses.EventImageGenerationCallPartialImage, payload: `{"item_id":"img_1","output_index":0,"partial_image_index":0,"partial_image_b64":"abcd"}`, want: &responses.ImageGenerationCallPartialImageEvent{}},
		{name: responses.EventMCPCallArgumentsDelta, payload: `{"item_id":"mcp_1","output_index":0,"delta":"{"}`, want: &responses.MCPCallArgumentsDeltaEvent{}},
		{name: responses.EventMCPCallArgumentsDone, payload: `{"item_id":"mcp_1","output_index":0,"arguments":"{\"x\":1}"}`, want: &responses.MCPCallArgumentsDoneEvent{}},
		{name: responses.EventMCPCallCompleted, payload: `{"item_id":"mcp_1","output_index":0}`, want: &responses.MCPCallCompletedEvent{}},
		{name: responses.EventMCPCallFailed, payload: `{"item_id":"mcp_1","output_index":0}`, want: &responses.MCPCallFailedEvent{}},
		{name: responses.EventMCPCallInProgress, payload: `{"item_id":"mcp_1","output_index":0}`, want: &responses.MCPCallInProgressEvent{}},
		{name: responses.EventMCPListToolsCompleted, payload: `{"item_id":"mcp_ls_1","output_index":0}`, want: &responses.MCPListToolsCompletedEvent{}},
		{name: responses.EventMCPListToolsFailed, payload: `{"item_id":"mcp_ls_1","output_index":0}`, want: &responses.MCPListToolsFailedEvent{}},
		{name: responses.EventMCPListToolsInProgress, payload: `{"item_id":"mcp_ls_1","output_index":0}`, want: &responses.MCPListToolsInProgressEvent{}},
		{name: responses.EventCodeInterpreterCallInProgress, payload: `{"item_id":"ci_1","output_index":0}`, want: &responses.CodeInterpreterCallInProgressEvent{}},
		{name: responses.EventCodeInterpreterCallInterpreting, payload: `{"item_id":"ci_1","output_index":0}`, want: &responses.CodeInterpreterCallInterpretingEvent{}},
		{name: responses.EventCodeInterpreterCallCompleted, payload: `{"item_id":"ci_1","output_index":0}`, want: &responses.CodeInterpreterCallCompletedEvent{}},
		{name: responses.EventCodeInterpreterCallCodeDelta, payload: `{"item_id":"ci_1","output_index":0,"delta":"print("}`, want: &responses.CodeInterpreterCallCodeDeltaEvent{}},
		{name: responses.EventCodeInterpreterCallCodeDone, payload: `{"item_id":"ci_1","output_index":0,"code":"print(42)"}`, want: &responses.CodeInterpreterCallCodeDoneEvent{}},
		{name: responses.EventCustomToolCallInputDelta, payload: `{"item_id":"ct_1","output_index":0,"delta":"partial"}`, want: &responses.CustomToolCallInputDeltaEvent{}},
		{name: responses.EventCustomToolCallInputDone, payload: `{"item_id":"ct_1","output_index":0,"input":"complete"}`, want: &responses.CustomToolCallInputDoneEvent{}},
		{name: responses.EventAPIError, payload: `{"code":"rate_limit_exceeded","message":"slow down","param":null}`, done: true},
		{name: responses.EventAudioTranscriptDone, payload: `{"response_id":"resp_01"}`, want: &responses.AudioTranscriptDoneEvent{}},
		{name: responses.EventAudioTranscriptDelta, payload: `{"response_id":"resp_01","delta":"hello"}`, want: &responses.AudioTranscriptDeltaEvent{}},
		{name: responses.EventAudioDone, payload: `{"response_id":"resp_01"}`, want: &responses.AudioDoneEvent{}},
		{name: responses.EventAudioDelta, payload: `{"response_id":"resp_01","delta":"YmFzZTY0"}`, want: &responses.AudioDeltaEvent{}},
	}

	require.Len(t, tests, len(responses.DocumentedStreamEvents))

	h := handler()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := h(tt.name, []byte(tt.payload))
			assert.Equal(t, tt.done, result.Done)
			if tt.name == responses.EventAPIError {
				require.Error(t, result.Err)
				assert.Contains(t, result.Err.Error(), "rate_limit_exceeded")
				assert.Contains(t, result.Err.Error(), "slow down")
				return
			}

			require.NoError(t, result.Err)
			require.NotNil(t, result.Event)
			require.IsType(t, tt.want, result.Event)
			assertEventTypeField(t, result.Event, tt.name)
			if tt.check != nil {
				tt.check(t, result.Event)
			}
		})
	}
}

func TestParser_UnknownEvent_NoOp(t *testing.T) {
	h := handler()
	result := h("response.future_unknown_event", []byte(`{"some":"data"}`))
	assert.Nil(t, result.Event)
	assert.NoError(t, result.Err)
	assert.False(t, result.Done)
}

func TestParser_UnnamedSSE_TypeInJSON(t *testing.T) {
	tests := []struct {
		name string
		data string
		want any
		done bool
	}{
		{name: responses.EventResponseCreated, data: `{"type":"response.created","response":{"id":"resp_01","model":"gpt-4o"}}`, want: &responses.ResponseCreatedEvent{}},
		{name: responses.EventOutputTextDelta, data: `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"hello"}`, want: &responses.OutputTextDeltaEvent{}},
		{name: responses.EventReasoningTextDelta, data: `{"type":"response.reasoning_text.delta","item_id":"rs_1","output_index":0,"delta":"17 × 19 = 323"}`, want: &responses.ReasoningTextDeltaEvent{}},
		{name: responses.EventResponseCompleted, data: `{"type":"response.completed","response":{"id":"r1","model":"gpt-4o","status":"completed","usage":{"input_tokens":5,"output_tokens":3}}}`, want: &responses.ResponseCompletedEvent{}, done: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler()("", []byte(tt.data))
			require.NoError(t, result.Err)
			require.IsType(t, tt.want, result.Event)
			assert.Equal(t, tt.done, result.Done)
			assertEventTypeField(t, result.Event, tt.name)
		})
	}

	t.Run("no type field stays no-op", func(t *testing.T) {
		result := handler()("", []byte(`{"sequence_number":99}`))
		assert.Nil(t, result.Event)
		assert.NoError(t, result.Err)
		assert.False(t, result.Done)
	})
}

func collectEvents(t *testing.T, httpClient *http.Client) []apicore.StreamResult {
	t.Helper()
	client := responses.NewClient(
		responses.WithBaseURL("https://fake.api"),
		responses.WithHTTPClient(httpClient),
	)
	req := &responses.Request{Model: "test", Stream: true, Input: []responses.Input{{Role: "user", Content: "ping"}}}

	handle, err := client.Stream(t.Context(), req)
	require.NoError(t, err)

	var events []apicore.StreamResult
	for result := range handle.Events {
		events = append(events, result)
	}
	return events
}

func TestFixture_TextStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "text_stream.sse"))
	require.NotEmpty(t, events)

	var textDeltas []string
	var completed *responses.ResponseCompletedEvent
	for _, r := range events {
		switch ev := r.Event.(type) {
		case *responses.OutputTextDeltaEvent:
			textDeltas = append(textDeltas, ev.Delta)
		case *responses.ResponseCompletedEvent:
			completed = ev
		}
	}

	require.NotEmpty(t, textDeltas)
	assert.Equal(t, "pong", textDeltas[0])
	require.NotNil(t, completed)
	assert.Equal(t, responses.StatusCompleted, completed.Response.Status)
	require.NotNil(t, completed.Response.Usage)
	assert.Equal(t, 12, completed.Response.Usage.InputTokens)
	assert.Equal(t, 1, completed.Response.Usage.OutputTokens)
	assert.True(t, events[len(events)-1].Done)
}

func TestFixture_ToolCallStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "tool_call_stream.sse"))

	var itemDone *responses.OutputItemDoneEvent
	var funcDeltas []string
	for _, r := range events {
		switch ev := r.Event.(type) {
		case *responses.FunctionCallArgumentsDeltaEvent:
			funcDeltas = append(funcDeltas, ev.Delta)
		case *responses.OutputItemDoneEvent:
			if ev.Item.Type == "function_call" {
				itemDone = ev
			}
		}
	}

	require.NotEmpty(t, funcDeltas)
	require.NotNil(t, itemDone)
	assert.Equal(t, "call_abc", itemDone.Item.CallID)
	assert.Equal(t, "get_weather", itemDone.Item.Name)
	assert.Equal(t, `{"city":"Berlin"}`, itemDone.Item.Arguments)
}

func TestFixture_ReasoningStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "reasoning_stream.sse"))

	var reasoningDeltas, textDeltas []string
	for _, r := range events {
		switch ev := r.Event.(type) {
		case *responses.ReasoningSummaryTextDeltaEvent:
			reasoningDeltas = append(reasoningDeltas, ev.Delta)
		case *responses.OutputTextDeltaEvent:
			textDeltas = append(textDeltas, ev.Delta)
		}
	}

	assert.NotEmpty(t, reasoningDeltas)
	assert.NotEmpty(t, textDeltas)
}

func TestFixture_ErrorStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "error_stream.sse"))
	require.Len(t, events, 1)
	assert.True(t, events[0].Done)
	require.Error(t, events[0].Err)
	assert.Contains(t, events[0].Err.Error(), "rate_limit_exceeded")
}

func assertEventTypeField(t *testing.T, event any, want string) {
	t.Helper()
	te, ok := event.(interface{ EventType() string })
	require.True(t, ok, "event %T does not expose EventType()", event)
	assert.Equal(t, want, te.EventType())
}
