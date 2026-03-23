package llm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/llmtest"
)

// --- tests ---

func TestStreamResponse_TextAccumulation(t *testing.T) {
	ch := llmtest.SendEvents(
		llmtest.TextEvent("hello"),
		llmtest.TextEvent(" "),
		llmtest.TextEvent("world"),
		llmtest.DoneEvent(nil),
	)

	result := <-llm.Process(context.Background(), ch).Result()

	require.NoError(t, result.Error())
	assert.Equal(t, "hello world", result.Text)
	assert.Equal(t, llm.StopReasonEndTurn, result.StopReason)
	assert.Nil(t, result.Usage)
}

func TestStreamResponse_ReasoningAccumulation(t *testing.T) {
	ch := llmtest.SendEvents(
		llmtest.ReasoningEvent("step1 "),
		llmtest.ReasoningEvent("step2"),
		llmtest.TextEvent("answer"),
		llmtest.DoneEvent(nil),
	)

	result := <-llm.Process(context.Background(), ch).Result()

	require.NoError(t, result.Error())
	assert.Equal(t, "step1 step2", result.Reasoning)
	assert.Equal(t, "answer", result.Text)
}

func TestStreamResponse_Usage(t *testing.T) {
	usage := &llm.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15, Cost: 0.001}
	ch := llmtest.SendEvents(llmtest.TextEvent("hi"), llmtest.DoneEvent(usage))

	result := <-llm.Process(context.Background(), ch).Result()

	require.NoError(t, result.Error())
	require.NotNil(t, result.Usage)
	assert.Equal(t, 10, result.Usage.InputTokens)
	assert.Equal(t, 5, result.Usage.OutputTokens)
}

func TestStreamResponse_OnTextCallback(t *testing.T) {
	var received []string
	ch := llmtest.SendEvents(llmtest.TextEvent("a"), llmtest.TextEvent("b"), llmtest.TextEvent("c"), llmtest.DoneEvent(nil))

	result := <-llm.Process(context.Background(), ch).
		OnText(func(s string) { received = append(received, s) }).
		Result()

	require.NoError(t, result.Error())
	assert.Equal(t, []string{"a", "b", "c"}, received)
}

func TestStreamResponse_OnReasoningCallback(t *testing.T) {
	var received []string
	ch := llmtest.SendEvents(llmtest.ReasoningEvent("r1"), llmtest.ReasoningEvent("r2"), llmtest.DoneEvent(nil))

	result := <-llm.Process(context.Background(), ch).
		OnReasoning(func(s string) { received = append(received, s) }).
		Result()

	require.NoError(t, result.Error())
	assert.Equal(t, []string{"r1", "r2"}, received)
}

func TestStreamResponse_OnToolDeltaCallback(t *testing.T) {
	var received []string
	toolDelta := llm.ToolDelta(llm.DeltaIndex(0), "tid", "get_weather", `{"loc`)
	deltaCh := llmtest.SendEvents(
		llm.StreamEvent{Type: llm.StreamEventDelta, Delta: toolDelta},
		llmtest.DoneEvent(nil),
	)

	result := <-llm.Process(context.Background(), deltaCh).
		OnToolDelta(func(d *llm.Delta) { received = append(received, d.ToolArgs) }).
		Result()

	require.NoError(t, result.Error())
	assert.Equal(t, []string{`{"loc`}, received)
}

func TestStreamResponse_StopReasonToolUse(t *testing.T) {
	ch := llmtest.SendEvents(
		llmtest.ToolEvent("id1", "get_weather", map[string]any{"location": "Berlin"}),
		llmtest.DoneEvent(nil),
	)

	result := <-llm.Process(context.Background(), ch).Result()

	require.NoError(t, result.Error())
	assert.Equal(t, llm.StopReasonToolUse, result.StopReason)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, "get_weather", result.ToolCalls[0].Name)
}

func TestStreamResponse_ToolHandler_Sync(t *testing.T) {
	type In struct {
		Location string `json:"location"`
	}
	type Out struct {
		Temp int `json:"temp"`
	}

	ch := llmtest.SendEvents(
		llmtest.ToolEvent("id1", "get_weather", map[string]any{"location": "Berlin"}),
		llmtest.DoneEvent(nil),
	)

	weatherSpec := llm.NewToolSpec[In]("get_weather", "Get weather")
	result := <-llm.Process(context.Background(), ch).
		HandleTool(llm.Handle(weatherSpec, func(_ context.Context, in In) (*Out, error) {
			assert.Equal(t, "Berlin", in.Location)
			return &Out{Temp: 22}, nil
		})).
		Result()

	require.NoError(t, result.Error())
	require.Len(t, result.ToolResults, 1)
	assert.Equal(t, "id1", result.ToolResults[0].ToolCallID)
	assert.JSONEq(t, `{"temp":22}`, result.ToolResults[0].Output)
	assert.False(t, result.ToolResults[0].IsError)
}

func TestStreamResponse_ToolHandler_Error(t *testing.T) {
	type In struct {
		Location string `json:"location"`
	}
	type Out struct {
		Temp int `json:"temp"`
	}

	ch := llmtest.SendEvents(
		llmtest.ToolEvent("id1", "get_weather", map[string]any{"location": "Berlin"}),
		llmtest.DoneEvent(nil),
	)

	boom := errors.New("service unavailable")

	weatherSpec := llm.NewToolSpec[In]("get_weather", "Get weather")
	result := <-llm.Process(context.Background(), ch).
		HandleTool(llm.Handle(weatherSpec, func(_ context.Context, _ In) (*Out, error) { return nil, boom })).
		Result()

	// The error is surfaced on the result.
	assert.ErrorIs(t, result.Error(), boom)
	require.Len(t, result.ToolResults, 1)
	assert.True(t, result.ToolResults[0].IsError)
}

func TestStreamResponse_ToolHandler_Async(t *testing.T) {
	type In struct {
		N int `json:"n"`
	}
	type Out struct {
		N int `json:"n"`
	}

	ch := llmtest.SendEvents(
		llmtest.ToolEvent("id1", "double", map[string]any{"n": 3}),
		llmtest.ToolEvent("id2", "double", map[string]any{"n": 7}),
		llmtest.DoneEvent(nil),
	)

	doubleSpec := llm.NewToolSpec[In]("double", "Double a number")
	result := <-llm.Process(context.Background(), ch).
		HandleTool(llm.Handle(doubleSpec, func(_ context.Context, in In) (*Out, error) { return &Out{N: in.N * 2}, nil })).
		DispatchAsync().
		Result()

	require.NoError(t, result.Error())
	require.Len(t, result.ToolResults, 2)
	// Results must be in emission order even when async.
	assert.JSONEq(t, `{"n":6}`, result.ToolResults[0].Output)
	assert.JSONEq(t, `{"n":14}`, result.ToolResults[1].Output)
}

func TestStreamResponse_UnhandledToolCall(t *testing.T) {
	// Tool call with no handler registered — gets an error ToolCallResult so
	// the conversation history remains valid (model always gets a result back).
	ch := llmtest.SendEvents(
		llmtest.ToolEvent("id1", "unknown_tool", map[string]any{}),
		llmtest.DoneEvent(nil),
	)

	result := <-llm.Process(context.Background(), ch).Result()

	require.NoError(t, result.Error())
	require.Len(t, result.ToolCalls, 1)
	require.Len(t, result.ToolResults, 1)
	assert.Equal(t, "id1", result.ToolResults[0].ToolCallID)
	assert.True(t, result.ToolResults[0].IsError)
	assert.Contains(t, result.ToolResults[0].Output, "unknown_tool")
}

func TestStreamResponse_StreamError(t *testing.T) {
	ch := llmtest.SendEvents(
		llmtest.TextEvent("partial"),
		llmtest.ErrorEvent(llm.NewErrProviderMsg("test", "foo")),
	)

	result := <-llm.Process(context.Background(), ch).Result()

	assert.Error(t, result.Error())
	assert.Equal(t, llm.StopReasonError, result.StopReason)
	assert.Equal(t, "partial", result.Text)
}

func TestStreamResponse_ContextCancellation(t *testing.T) {
	// Use an unbuffered channel so the goroutine blocks waiting.
	live := make(chan llm.StreamEvent)
	ctx, cancel := context.WithCancel(context.Background())

	resp := llm.Process(ctx, live)
	resultCh := resp.Result()

	cancel()

	result := <-resultCh
	assert.ErrorIs(t, result.Error(), context.Canceled)
	assert.Equal(t, llm.StopReasonCancelled, result.StopReason)
}

func TestStreamResponse_CallbackPanicRecovered(t *testing.T) {
	ch := llmtest.SendEvents(llmtest.TextEvent("x"), llmtest.DoneEvent(nil))

	result := <-llm.Process(context.Background(), ch).
		OnText(func(_ string) { panic("boom") }).
		Result()

	// Panic is recovered; result still has the text.
	assert.Error(t, result.Error())
	assert.Contains(t, result.Error().Error(), "boom")
	assert.Equal(t, "x", result.Text)
}

func TestStreamResponse_ToolHandlerPanicRecovered(t *testing.T) {
	type In struct{}
	type Out struct{}

	ch := llmtest.SendEvents(
		llmtest.ToolEvent("id1", "explode", map[string]any{}),
		llmtest.DoneEvent(nil),
	)

	explodeSpec := llm.NewToolSpec[In]("explode", "Explode")
	result := <-llm.Process(context.Background(), ch).
		HandleTool(llm.Handle(explodeSpec, func(_ context.Context, _ In) (*Out, error) { panic("kaboom") })).
		Result()

	assert.Error(t, result.Error())
	assert.Contains(t, result.Error().Error(), "kaboom")
	require.Len(t, result.ToolResults, 1)
	assert.True(t, result.ToolResults[0].IsError)
}

func TestStreamResponse_Message(t *testing.T) {
	ch := llmtest.SendEvents(
		llmtest.TextEvent("hello"),
		llmtest.ToolEvent("id1", "search", map[string]any{"q": "go"}),
		llmtest.DoneEvent(nil),
	)

	result := <-llm.Process(context.Background(), ch).Result()

	msg := result.Message()
	assert.Equal(t, llm.RoleAssistant, msg.Role())
	assert.Equal(t, "hello", msg.Content)
	require.Len(t, msg.ToolCalls, 1)
	assert.Equal(t, "search", msg.ToolCalls[0].Name)
}

func TestStreamResponse_Next(t *testing.T) {
	type In struct {
		Q string `json:"q"`
	}
	type Out struct {
		Results []string `json:"results"`
	}

	ch := llmtest.SendEvents(
		llmtest.TextEvent("searching..."),
		llmtest.ToolEvent("id1", "search", map[string]any{"q": "go"}),
		llmtest.DoneEvent(nil),
	)

	searchSpec := llm.NewToolSpec[In]("search", "Search")
	result := <-llm.Process(context.Background(), ch).
		HandleTool(llm.Handle(searchSpec, func(_ context.Context, in In) (*Out, error) {
			return &Out{Results: []string{"golang.org"}}, nil
		})).
		Result()

	require.NoError(t, result.Error())
	next := result.Next()
	require.Len(t, next, 2)

	asst, ok := next[0].(*llm.AssistantMsg)
	require.True(t, ok)
	assert.Equal(t, "searching...", asst.Content)

	tcr, ok := next[1].(*llm.ToolCallResult)
	require.True(t, ok)
	assert.Equal(t, "id1", tcr.ToolCallID)
	assert.JSONEq(t, `{"results":["golang.org"]}`, tcr.Output)
}

func TestStreamResponse_Apply(t *testing.T) {
	ch := llmtest.SendEvents(llmtest.TextEvent("hi"), llmtest.DoneEvent(nil))

	var msgs llm.Messages
	msgs.AddUserMsg("hello")

	result := <-llm.Process(context.Background(), ch).Result()
	result.Apply(&msgs)

	assert.Len(t, msgs, 2)
	assert.Equal(t, llm.RoleUser, msgs[0].Role())
	assert.Equal(t, llm.RoleAssistant, msgs[1].Role())
}

func TestStreamResponse_ResultIdempotent(t *testing.T) {
	ch := llmtest.SendEvents(llmtest.TextEvent("hi"), llmtest.DoneEvent(nil))

	r := llm.Process(context.Background(), ch)
	ch1 := r.Result()
	ch2 := r.Result() // second call — same channel

	assert.Equal(t, ch1, ch2)
	result := <-ch1
	require.NoError(t, result.Error())
	assert.Equal(t, "hi", result.Text)
}

// TestStreamResponse_HandleTool_BoundSpec verifies that llm.Handle(spec, fn)
// satisfies ToolHandler and that HandleTool reads the name from the spec.
func TestStreamResponse_HandleTool_BoundSpec(t *testing.T) {
	type In struct {
		Location string `json:"location" jsonschema:"required"`
	}
	type Out struct {
		Temp int `json:"temp"`
	}

	spec := llm.NewToolSpec[In]("get_weather", "Get weather")

	ch := llmtest.SendEvents(
		llmtest.ToolEvent("id1", "get_weather", map[string]any{"location": "Paris"}),
		llmtest.DoneEvent(nil),
	)

	result := <-llm.Process(context.Background(), ch).
		HandleTool(llm.Handle(spec, func(_ context.Context, in In) (*Out, error) {
			return &Out{Temp: 18}, nil
		})).
		Result()

	require.NoError(t, result.Error())
	require.Len(t, result.ToolResults, 1)
	assert.JSONEq(t, `{"temp":18}`, result.ToolResults[0].Output)
}
