package messages_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/messages"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func handler() apicore.EventHandler { return messages.NewParser()() }

func fixture(t *testing.T, name string) *http.Client {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err, "missing fixture %s", name)
	return &http.Client{Transport: apicore.FixedSSEResponse(200, string(data))}
}

func TestParser_MessageStart(t *testing.T) {
	h := handler()
	result := h(messages.EventMessageStart, []byte(`{"message":{"id":"msg_1","model":"claude-3-5-haiku-20241022","usage":{"input_tokens":10}}}`))
	require.NoError(t, result.Err)
	assert.False(t, result.Done)
	evt := result.Event.(*messages.MessageStartEvent)
	assert.Equal(t, "msg_1", evt.Message.ID)
	assert.Equal(t, 10, evt.Message.Usage.InputTokens)
}

func TestParser_TextBlock_AccumulatedAndComplete(t *testing.T) {
	h := handler()
	h(messages.EventContentBlockStart, []byte(`{"index":0,"content_block":{"type":"text"}}`))
	h(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"text_delta","text":"hello "}}`))
	h(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"text_delta","text":"world"}}`))

	result := h(messages.EventContentBlockStop, []byte(`{"index":0}`))
	require.NoError(t, result.Err)
	evt := result.Event.(*messages.TextCompleteEvent)
	assert.Equal(t, "hello world", evt.Text)
	assert.Equal(t, 0, evt.Index)
}

func TestParser_ThinkingBlock_AccumulatedAndComplete(t *testing.T) {
	h := handler()
	h(messages.EventContentBlockStart, []byte(`{"index":0,"content_block":{"type":"thinking"}}`))
	h(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"thinking_delta","thinking":"Let me think"}}`))
	h(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"signature_delta","signature":"sig123"}}`))

	result := h(messages.EventContentBlockStop, []byte(`{"index":0}`))
	require.NoError(t, result.Err)
	evt := result.Event.(*messages.ThinkingCompleteEvent)
	assert.Equal(t, "Let me think", evt.Thinking)
	assert.Equal(t, "sig123", evt.Signature)
}

func TestParser_ToolBlock_AccumulatedAndComplete(t *testing.T) {
	h := handler()
	h(messages.EventContentBlockStart, []byte(`{"index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"search"}}`))
	h(messages.EventContentBlockDelta, []byte(`{"index":1,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`))
	h(messages.EventContentBlockDelta, []byte(`{"index":1,"delta":{"type":"input_json_delta","partial_json":"\"golang\"}"}}`))

	result := h(messages.EventContentBlockStop, []byte(`{"index":1}`))
	require.NoError(t, result.Err)
	evt := result.Event.(*messages.ToolCompleteEvent)
	assert.Equal(t, "toolu_1", evt.ID)
	assert.Equal(t, "search", evt.Name)
	assert.Equal(t, map[string]any{"q": "golang"}, evt.Args)
}

func TestParser_ServerToolUseBlock_StopIsObservable(t *testing.T) {
	h := handler()
	h(messages.EventContentBlockStart, []byte(`{"index":2,"content_block":{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search"}}`))

	result := h(messages.EventContentBlockStop, []byte(`{"index":2}`))
	require.NoError(t, result.Err)
	evt := result.Event.(*messages.ContentBlockStopEvent)
	assert.Equal(t, 2, evt.Index)
}

func TestParser_UnknownDeltaSubtype_NoFail(t *testing.T) {
	h := handler()
	h(messages.EventContentBlockStart, []byte(`{"index":0,"content_block":{"type":"text"}}`))
	result := h(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"future_delta","text":"x"}}`))
	require.NoError(t, result.Err)
	_, ok := result.Event.(*messages.ContentBlockDeltaEvent)
	assert.True(t, ok)
}

func TestParser_PingEvent(t *testing.T) {
	h := handler()
	result := h(messages.EventPing, []byte(`{"type":"ping"}`))
	require.NoError(t, result.Err)
	assert.IsType(t, &messages.PingEvent{}, result.Event)
	assert.False(t, result.Done)
}

func TestParser_MessageStop_Done(t *testing.T) {
	h := handler()
	result := h(messages.EventMessageStop, []byte(`{"type":"message_stop"}`))
	assert.True(t, result.Done)
	assert.IsType(t, &messages.MessageStopEvent{}, result.Event)
}

func TestParser_Error_DoneAndErr(t *testing.T) {
	h := handler()
	result := h(messages.EventError, []byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	assert.True(t, result.Done)
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "Overloaded")
}

func TestParser_UnknownEvent_NoOp(t *testing.T) {
	h := handler()
	result := h("future.unknown", []byte(`{"x":1}`))
	assert.Nil(t, result.Event)
	assert.NoError(t, result.Err)
	assert.False(t, result.Done)
}

func TestParser_IsolatedAcrossStreams(t *testing.T) {
	factory := messages.NewParser()
	h1 := factory()
	h2 := factory()

	h1(messages.EventContentBlockStart, []byte(`{"index":0,"content_block":{"type":"text"}}`))
	h1(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"text_delta","text":"h1"}}`))

	h2(messages.EventContentBlockStart, []byte(`{"index":0,"content_block":{"type":"text"}}`))
	h2(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"text_delta","text":"h2"}}`))

	r1 := h1(messages.EventContentBlockStop, []byte(`{"index":0}`))
	r2 := h2(messages.EventContentBlockStop, []byte(`{"index":0}`))

	e1 := r1.Event.(*messages.TextCompleteEvent)
	e2 := r2.Event.(*messages.TextCompleteEvent)
	assert.Equal(t, "h1", e1.Text)
	assert.Equal(t, "h2", e2.Text)
}

func collectEvents(t *testing.T, httpClient *http.Client) []apicore.StreamResult {
	t.Helper()
	client := messages.NewClient(
		messages.WithBaseURL("https://fake.api"),
		messages.WithHTTPClient(httpClient),
	)
	req := &messages.Request{
		Model:     "claude-3-5-haiku-20241022",
		MaxTokens: 32,
		Stream:    true,
		Messages:  []messages.Message{{Role: "user", Content: "ping"}},
	}

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

	var text *messages.TextCompleteEvent
	var completed *messages.MessageStopEvent
	for _, r := range events {
		switch ev := r.Event.(type) {
		case *messages.TextCompleteEvent:
			text = ev
		case *messages.MessageStopEvent:
			completed = ev
		}
	}

	require.NotNil(t, text)
	assert.Equal(t, "pong", text.Text)
	require.NotNil(t, completed)
	assert.True(t, events[len(events)-1].Done)
}

func TestFixture_ToolStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "tool_stream.sse"))
	var toolEvt *messages.ToolCompleteEvent
	for _, r := range events {
		if ev, ok := r.Event.(*messages.ToolCompleteEvent); ok {
			toolEvt = ev
		}
	}
	require.NotNil(t, toolEvt)
	assert.Equal(t, "get_weather", toolEvt.Name)
	assert.Equal(t, map[string]any{"city": "Berlin"}, toolEvt.Args)
}

func TestFixture_ThinkingStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "thinking_stream.sse"))
	var thinkEvt *messages.ThinkingCompleteEvent
	for _, r := range events {
		if ev, ok := r.Event.(*messages.ThinkingCompleteEvent); ok {
			thinkEvt = ev
		}
	}
	require.NotNil(t, thinkEvt)
	assert.NotEmpty(t, thinkEvt.Thinking)
	assert.NotEmpty(t, thinkEvt.Signature)
}

func TestFixture_ErrorStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "error_stream.sse"))
	require.Len(t, events, 1)
	assert.True(t, events[0].Done)
	require.Error(t, events[0].Err)
	assert.Contains(t, events[0].Err.Error(), "Overloaded")
}
