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
	return &http.Client{
		Transport: apicore.FixedSSEResponse(200, string(data)),
	}
}

func TestParser_ResponseCreated(t *testing.T) {
	h := handler()
	result := h(responses.EventResponseCreated,
		[]byte(`{"response":{"id":"resp_01","model":"gpt-4o-mini"}}`))
	require.NoError(t, result.Err)
	assert.False(t, result.Done)
	evt := result.Event.(*responses.ResponseCreatedEvent)
	assert.Equal(t, "resp_01", evt.Response.ID)
	assert.Equal(t, "gpt-4o-mini", evt.Response.Model)
}

func TestParser_TextDelta(t *testing.T) {
	h := handler()
	result := h(responses.EventOutputTextDelta,
		[]byte(`{"output_index":0,"delta":"hello"}`))
	require.NoError(t, result.Err)
	assert.False(t, result.Done)
	evt := result.Event.(*responses.TextDeltaEvent)
	assert.Equal(t, "hello", evt.Delta)
	assert.Equal(t, 0, evt.OutputIndex)
}

func TestParser_ReasoningDelta(t *testing.T) {
	tests := []struct {
		name      string
		eventName string
	}{
		{"reasoning_summary_text.delta (OpenAI o3)", responses.EventReasoningDelta},
		{"reasoning_text.delta (Claude via OpenRouter)", responses.EventReasoningTextDelta},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler()
			result := h(tt.eventName, []byte(`{"output_index":0,"delta":"hmm..."}`))
			require.NoError(t, result.Err)
			evt, ok := result.Event.(*responses.ReasoningDeltaEvent)
			require.True(t, ok, "expected *ReasoningDeltaEvent, got %T", result.Event)
			assert.Equal(t, "hmm...", evt.Delta)
		})
	}
}

func TestParser_ToolCall_ArgAccumulationAndComplete(t *testing.T) {
	h := handler()

	h(responses.EventOutputItemAdded,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_abc","name":"search"}}`))

	h(responses.EventFuncArgsDelta, []byte(`{"output_index":0,"delta":"{\"q\":"}`))
	h(responses.EventFuncArgsDelta, []byte(`{"output_index":0,"delta":"\"golang\"}"}`))

	result := h(responses.EventOutputItemDone,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_abc","name":"search","arguments":""}}`))
	require.NoError(t, result.Err)
	evt, ok := result.Event.(*responses.ToolCompleteEvent)
	require.True(t, ok, "expected *ToolCompleteEvent, got %T", result.Event)
	assert.Equal(t, "call_abc", evt.ID)
	assert.Equal(t, "search", evt.Name)
	assert.Equal(t, map[string]any{"q": "golang"}, evt.Args)
}

func TestParser_ToolCall_UsesArgumentsFieldWhenAccumulatorEmpty(t *testing.T) {
	h := handler()
	h(responses.EventOutputItemAdded,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"i1","call_id":"c1","name":"fn"}}`))
	result := h(responses.EventOutputItemDone,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"i1","call_id":"c1","name":"fn","arguments":"{\"x\":1}"}}`))
	evt := result.Event.(*responses.ToolCompleteEvent)
	assert.Equal(t, map[string]any{"x": float64(1)}, evt.Args)
}

func TestParser_ToolCall_FallsBackToAccumulatorForIDName(t *testing.T) {
	h := handler()
	h(responses.EventOutputItemAdded,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"i1","call_id":"the_id","name":"the_fn"}}`))
	result := h(responses.EventOutputItemDone,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"i1","call_id":"","name":"","arguments":"{\"k\":\"v\"}"}}`))
	evt := result.Event.(*responses.ToolCompleteEvent)
	assert.Equal(t, "the_id", evt.ID)
	assert.Equal(t, "the_fn", evt.Name)
}

func TestParser_ResponseCompleted_IncompleteMaxTokens(t *testing.T) {
	h := handler()
	result := h(responses.EventResponseCompleted,
		[]byte(`{"response":{"id":"r1","model":"m","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":10,"output_tokens":100}}}`),
	)
	assert.True(t, result.Done)
	require.NoError(t, result.Err)
	evt := result.Event.(*responses.ResponseCompletedEvent)
	assert.Equal(t, responses.StatusIncomplete, evt.Response.Status)
	require.NotNil(t, evt.Response.IncompleteDetails)
	assert.Equal(t, responses.ReasonMaxOutputTokens, evt.Response.IncompleteDetails.Reason)
}

func TestParser_ResponseCompleted_SetsDoneAndUsage(t *testing.T) {
	h := handler()
	result := h(responses.EventResponseCompleted,
		[]byte(`{"response":{"id":"r1","model":"gpt-4o-mini","status":"completed","usage":{"input_tokens":10,"output_tokens":5}}}`))
	assert.True(t, result.Done)
	require.NoError(t, result.Err)
	evt := result.Event.(*responses.ResponseCompletedEvent)
	assert.Equal(t, responses.StatusCompleted, evt.Response.Status)
	require.NotNil(t, evt.Response.Usage)
	assert.Equal(t, 10, evt.Response.Usage.InputTokens)
	assert.Equal(t, 5, evt.Response.Usage.OutputTokens)
}

func TestParser_ResponseFailed_SetsDone(t *testing.T) {
	h := handler()
	result := h(responses.EventResponseFailed,
		[]byte(`{"response":{"id":"r1","model":"m","status":"failed","error":{"code":"server_error","message":"internal error"}}}`))
	assert.True(t, result.Done)
	require.NoError(t, result.Err)
	evt := result.Event.(*responses.ResponseCompletedEvent)
	assert.Equal(t, responses.StatusFailed, evt.Response.Status)
	require.NotNil(t, evt.Response.Error)
	assert.Equal(t, "server_error", evt.Response.Error.Code)
}

func TestParser_APIError_ReturnsDoneAndErr(t *testing.T) {
	h := handler()
	result := h(responses.EventAPIError,
		[]byte(`{"error":{"message":"rate limit","code":"rate_limit_exceeded"}}`))
	assert.True(t, result.Done)
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "rate_limit_exceeded")
	assert.Contains(t, result.Err.Error(), "rate limit")
}

func TestParser_KnownNoOpEvent_NoOp(t *testing.T) {
	h := handler()
	tests := []string{
		responses.EventResponseInProgress,
		responses.EventContentPartAdded,
		responses.EventContentPartDone,
		responses.EventOutputTextDone,
		responses.EventOutputTextAnnotation,
		responses.EventFuncArgsDone,
		responses.EventReasoningDeltaRaw,
		responses.EventReasoningDone,
		responses.EventReasoningSummaryDone,
		responses.EventResponseQueued,
		responses.EventRateLimitsUpdated,
	}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			result := h(name, []byte(`{"some":"data"}`))
			assert.Nil(t, result.Event)
			assert.NoError(t, result.Err)
			assert.False(t, result.Done)
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

// TestParser_UnnamedSSE_TypeInJSON verifies that when the SSE event name is
// empty (OpenRouter-style unnamed SSE), the parser correctly extracts the
// event type from the JSON "type" field and routes accordingly.
func TestParser_UnnamedSSE_TypeInJSON(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		check   func(t *testing.T, r apicore.StreamResult)
	}{
		{
			name: "response.created",
			data: `{"type":"response.created","response":{"id":"resp_01","model":"gpt-4o"}}`,
			check: func(t *testing.T, r apicore.StreamResult) {
				require.NoError(t, r.Err)
				assert.False(t, r.Done)
				evt, ok := r.Event.(*responses.ResponseCreatedEvent)
				require.True(t, ok, "expected *ResponseCreatedEvent, got %T", r.Event)
				assert.Equal(t, "resp_01", evt.Response.ID)
				assert.Equal(t, "gpt-4o", evt.Response.Model)
			},
		},
		{
			name: "response.output_text.delta",
			data: `{"type":"response.output_text.delta","output_index":0,"delta":"hello"}`,
			check: func(t *testing.T, r apicore.StreamResult) {
				require.NoError(t, r.Err)
				evt, ok := r.Event.(*responses.TextDeltaEvent)
				require.True(t, ok, "expected *TextDeltaEvent, got %T", r.Event)
				assert.Equal(t, "hello", evt.Delta)
			},
		},
		{
			name: "response.reasoning_summary_text.delta",
			data: `{"type":"response.reasoning_summary_text.delta","output_index":0,"delta":"thinking..."}`,
			check: func(t *testing.T, r apicore.StreamResult) {
				require.NoError(t, r.Err)
				evt, ok := r.Event.(*responses.ReasoningDeltaEvent)
				require.True(t, ok, "expected *ReasoningDeltaEvent, got %T", r.Event)
				assert.Equal(t, "thinking...", evt.Delta)
			},
		},
		{
			name: "response.reasoning_text.delta (Claude-via-OpenRouter)",
			data: `{"type":"response.reasoning_text.delta","output_index":0,"delta":"17 × 19 = 323"}`,
			check: func(t *testing.T, r apicore.StreamResult) {
				require.NoError(t, r.Err)
				evt, ok := r.Event.(*responses.ReasoningDeltaEvent)
				require.True(t, ok, "expected *ReasoningDeltaEvent, got %T", r.Event)
				assert.Equal(t, "17 × 19 = 323", evt.Delta)
			},
		},
		{
			name: "response.completed",
			data: `{"type":"response.completed","response":{"id":"r1","model":"gpt-4o","status":"completed","usage":{"input_tokens":5,"output_tokens":3}}}`,
			check: func(t *testing.T, r apicore.StreamResult) {
				require.NoError(t, r.Err)
				assert.True(t, r.Done)
				evt, ok := r.Event.(*responses.ResponseCompletedEvent)
				require.True(t, ok)
				assert.Equal(t, 5, evt.Response.Usage.InputTokens)
			},
		},
		{
			name: "response.in_progress (no-op)",
			data: `{"type":"response.in_progress","response":{"id":"r1"}}`,
			check: func(t *testing.T, r apicore.StreamResult) {
				assert.Nil(t, r.Event)
				assert.NoError(t, r.Err)
				assert.False(t, r.Done)
			},
		},
		{
			name: "no type field → no-op",
			data: `{"sequence_number":99}`,
			check: func(t *testing.T, r apicore.StreamResult) {
				assert.Nil(t, r.Event)
				assert.NoError(t, r.Err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler() // fresh handler per sub-test
			result := h("", []byte(tt.data))
			tt.check(t, result)
		})
	}
}

func TestParser_IsolatedAcrossStreams(t *testing.T) {
	factory := responses.NewParser()
	h1, h2 := factory(), factory()

	h1(responses.EventOutputItemAdded,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"i1","call_id":"c1","name":"fn1"}}`))
	h1(responses.EventFuncArgsDelta, []byte(`{"output_index":0,"delta":"{\"a\":1}"}`))

	h2(responses.EventOutputItemAdded,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"i2","call_id":"c2","name":"fn2"}}`))
	h2(responses.EventFuncArgsDelta, []byte(`{"output_index":0,"delta":"{\"b\":2}"}`))

	r1 := h1(responses.EventOutputItemDone,
		[]byte(`{"output_index":0,"item":{"type":"function_call","call_id":"c1","name":"fn1","arguments":""}}`))
	r2 := h2(responses.EventOutputItemDone,
		[]byte(`{"output_index":0,"item":{"type":"function_call","call_id":"c2","name":"fn2","arguments":""}}`))

	e1 := r1.Event.(*responses.ToolCompleteEvent)
	e2 := r2.Event.(*responses.ToolCompleteEvent)
	assert.Equal(t, "c1", e1.ID)
	assert.Equal(t, "c2", e2.ID)
	assert.Equal(t, map[string]any{"a": float64(1)}, e1.Args)
	assert.Equal(t, map[string]any{"b": float64(2)}, e2.Args)
}

func collectEvents(t *testing.T, httpClient *http.Client) []apicore.StreamResult {
	t.Helper()
	client := responses.NewClient(
		responses.WithBaseURL("https://fake.api"),
		responses.WithHTTPClient(httpClient),
	)
	req := &responses.Request{Model: "test", Stream: true,
		Input: []responses.Input{{Role: "user", Content: "ping"}}}

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
		case *responses.TextDeltaEvent:
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

	last := events[len(events)-1]
	assert.True(t, last.Done)
}

func TestFixture_ToolCallStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "tool_call_stream.sse"))

	var toolComplete *responses.ToolCompleteEvent
	var funcDeltas []string
	for _, r := range events {
		switch ev := r.Event.(type) {
		case *responses.FuncArgsDeltaEvent:
			funcDeltas = append(funcDeltas, ev.Delta)
		case *responses.ToolCompleteEvent:
			toolComplete = ev
		}
	}

	require.NotEmpty(t, funcDeltas)
	require.NotNil(t, toolComplete)
	assert.Equal(t, "call_abc", toolComplete.ID)
	assert.Equal(t, "get_weather", toolComplete.Name)
	assert.Equal(t, map[string]any{"city": "Berlin"}, toolComplete.Args)
}

func TestFixture_ReasoningStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "reasoning_stream.sse"))

	var reasoningDeltas, textDeltas []string
	for _, r := range events {
		switch ev := r.Event.(type) {
		case *responses.ReasoningDeltaEvent:
			reasoningDeltas = append(reasoningDeltas, ev.Delta)
		case *responses.TextDeltaEvent:
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
