package llm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/llmtest"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
)

// --- tests ---

func TestStreamResponse_TextAccumulation(t *testing.T) {
	ch := llmtest.SendEvents(
		llmtest.TextEvent("hello"),
		llmtest.TextEvent(" "),
		llmtest.TextEvent("world"),
		llmtest.CompletedEvent(llm.StopReasonEndTurn),
	)

	result := llm.NewEventProcessor(context.Background(), ch).Result()
	require.NoError(t, result.Error())
	assert.Equal(t, "hello world", result.Text())
	assert.Equal(t, llm.StopReasonEndTurn, result.StopReason())
	assert.Nil(t, result.Usage())
}

func TestStreamResponse_ReasoningAccumulation(t *testing.T) {
	ch := llmtest.SendEvents(
		llmtest.ReasoningEvent("step1 "),
		llmtest.ReasoningEvent("step2"),
		llmtest.TextEvent("answer"),
		llmtest.CompletedEvent(llm.StopReasonEndTurn),
	)

	result := llm.NewEventProcessor(context.Background(), ch).Result()
	require.NoError(t, result.Error())
	assert.Equal(t, "step1 step2", result.Thought())
	assert.Equal(t, "answer", result.Text())
}

func TestStreamResponse_Usage(t *testing.T) {
	usage := llm.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15, Cost: 0.001}
	ch := llmtest.SendEvents(llmtest.TextEvent("hi"), llmtest.UsageEvent(usage), llmtest.CompletedEvent(llm.StopReasonEndTurn))

	result := llm.NewEventProcessor(context.Background(), ch).Result()
	require.NoError(t, result.Error())
	require.NotNil(t, result.Usage())
	assert.Equal(t, 10, result.Usage().InputTokens)
	assert.Equal(t, 5, result.Usage().OutputTokens)
}

func TestStreamResponse_OnTextCallback(t *testing.T) {
	var received []string
	ch := llmtest.SendEvents(llmtest.TextEvent("a"), llmtest.TextEvent("b"), llmtest.TextEvent("c"), llmtest.CompletedEvent(llm.StopReasonEndTurn))

	result := llm.NewEventProcessor(context.Background(), ch).
		OnTextDelta(func(s string) { received = append(received, s) }).
		Result()
	require.NoError(t, result.Error())
	assert.Equal(t, []string{"a", "b", "c"}, received)
}

func TestStreamResponse_OnReasoningCallback(t *testing.T) {
	var received []string
	ch := llmtest.SendEvents(llmtest.ReasoningEvent("r1"), llmtest.ReasoningEvent("r2"), llmtest.CompletedEvent(llm.StopReasonEndTurn))

	result := llm.NewEventProcessor(context.Background(), ch).
		OnReasoningDelta(func(s string) { received = append(received, s) }).
		Result()
	require.NoError(t, result.Error())
	assert.Equal(t, []string{"r1", "r2"}, received)
}

func TestStreamResponse_OnToolDeltaCallback(t *testing.T) {
	var received []string
	deltaCh := llmtest.SendEvents(
		llm.ToolDelta("tid", "get_weather", `{"loc`).WithIndex(0),
		llmtest.CompletedEvent(llm.StopReasonEndTurn),
	)

	result := llm.NewEventProcessor(context.Background(), deltaCh).
		OnToolDelta(func(d llm.ToolDeltaPart) { received = append(received, d.ToolArgs) }).
		Result()
	require.NoError(t, result.Error())
	assert.Equal(t, []string{`{"loc`}, received)
}

func TestStreamResponse_StopReasonToolUse(t *testing.T) {
	ch := llmtest.SendEvents(
		llmtest.ToolEvent("id1", "get_weather", map[string]any{"location": "Berlin"}),
		llmtest.CompletedEvent(llm.StopReasonToolUse),
	)

	result := llm.NewEventProcessor(context.Background(), ch).Result()
	require.NoError(t, result.Error())
	assert.Equal(t, llm.StopReasonToolUse, result.StopReason())
	require.Len(t, result.ToolCalls(), 1)
	assert.Equal(t, "get_weather", result.ToolCalls()[0].ToolName())
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
		llmtest.CompletedEvent(llm.StopReasonToolUse),
	)

	weatherSpec := tool.NewSpec[In]("get_weather", "Get weather")
	result := llm.NewEventProcessor(context.Background(), ch).
		HandleTool(tool.Handle(weatherSpec, func(_ context.Context, in In) (*Out, error) {
			assert.Equal(t, "Berlin", in.Location)
			return &Out{Temp: 22}, nil
		})).
		Result()
	require.NoError(t, result.Error())

	msgs := result.Next()
	require.Len(t, msgs, 2)
	toolMsg := msgs[1]
	assert.Equal(t, msg.RoleTool, toolMsg.Role)
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
		llmtest.CompletedEvent(llm.StopReasonToolUse),
	)

	boom := errors.New("service unavailable")

	weatherSpec := tool.NewSpec[In]("get_weather", "Get weather")
	result := llm.NewEventProcessor(context.Background(), ch).
		HandleTool(tool.Handle(weatherSpec, func(_ context.Context, _ In) (*Out, error) { return nil, boom })).
		Result()

	require.NoError(t, result.Error())
	msgs := result.Next()
	require.Len(t, msgs, 2)
	toolMsg := msgs[1]
	assert.Equal(t, msg.RoleTool, toolMsg.Role)
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
		llmtest.CompletedEvent(llm.StopReasonToolUse),
	)

	doubleSpec := tool.NewSpec[In]("double", "Double a number")
	result := llm.NewEventProcessor(context.Background(), ch).
		HandleTool(tool.Handle(doubleSpec, func(_ context.Context, in In) (*Out, error) { return &Out{N: in.N * 2}, nil })).
		WithAsyncToolDispatch().
		Result()
	require.NoError(t, result.Error())

	msgs := result.Next()
	// Async dispatch: returns assistant message + tool results
	assert.NotEmpty(t, msgs)
	assert.Equal(t, msg.RoleAssistant, msgs[0].Role)
}

func TestStreamResponse_UnhandledToolCall(t *testing.T) {
	ch := llmtest.SendEvents(
		llmtest.ToolEvent("id1", "unknown_tool", map[string]any{}),
		llmtest.CompletedEvent(llm.StopReasonToolUse),
	)

	result := llm.NewEventProcessor(context.Background(), ch).Result()
	require.NoError(t, result.Error())
	require.Len(t, result.ToolCalls(), 1)

	msgs := result.Next()
	require.Len(t, msgs, 2)
	assistantMsg := msgs[0]
	assert.Equal(t, msg.RoleAssistant, assistantMsg.Role)
	require.Len(t, assistantMsg.ToolCalls(), 1)
	assert.Equal(t, "unknown_tool", assistantMsg.ToolCalls()[0].Name)
	toolMsg := msgs[1]
	assert.Equal(t, msg.RoleTool, toolMsg.Role)
}

func TestStreamResponse_StreamError(t *testing.T) {
	ch := llmtest.SendEvents(
		llmtest.TextEvent("partial"),
		llmtest.ErrorEvent(llm.NewErrProviderMsg("test", "foo")),
	)

	result := llm.NewEventProcessor(context.Background(), ch).Result()
	assert.Error(t, result.Error())
	assert.Equal(t, llm.StopReasonError, result.StopReason())
	assert.Equal(t, "partial", result.Text())
}

func TestStreamResponse_ContextCancellation(t *testing.T) {
	live := make(chan llm.Envelope)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-ctx.Done()
		close(live)
	}()

	cancel()

	result := llm.NewEventProcessor(ctx, live).Result()
	assert.ErrorIs(t, result.Error(), context.Canceled)
	assert.Equal(t, llm.StopReasonCancelled, result.StopReason())
}

func TestStreamResponse_CallbackPanicRecovered(t *testing.T) {
	ch := llmtest.SendEvents(llmtest.TextEvent("x"), llmtest.CompletedEvent(llm.StopReasonEndTurn))

	result := llm.NewEventProcessor(context.Background(), ch).
		OnTextDelta(func(_ string) { panic("boom") }).
		Result()

	assert.Error(t, result.Error())
	assert.Contains(t, result.Error().Error(), "boom")
	assert.Equal(t, "x", result.Text())
}

func TestStreamResponse_ToolHandlerPanicRecovered(t *testing.T) {
	type In struct{}
	type Out struct{}

	ch := llmtest.SendEvents(
		llmtest.ToolEvent("id1", "explode", map[string]any{}),
		llmtest.CompletedEvent(llm.StopReasonToolUse),
	)

	explodeSpec := tool.NewSpec[In]("explode", "Explode")
	result := llm.NewEventProcessor(context.Background(), ch).
		HandleTool(tool.Handle(explodeSpec, func(_ context.Context, _ In) (*Out, error) { panic("kaboom") })).
		Result()
	require.NotNil(t, result)
	require.NoError(t, result.Error())

	msgs := result.Next()
	require.Len(t, msgs, 2)
	toolMsg := msgs[1]
	assert.Equal(t, msg.RoleTool, toolMsg.Role)
}

func TestStreamResponse_Message(t *testing.T) {
	ch := llmtest.SendEvents(
		llmtest.TextEvent("hello"),
		llmtest.ToolEvent("id1", "search", map[string]any{"q": "go"}),
		llmtest.CompletedEvent(llm.StopReasonToolUse),
	)

	result := llm.NewEventProcessor(context.Background(), ch).Result()

	assistantMsg := result.Message()
	assert.Equal(t, msg.RoleAssistant, assistantMsg.Role)
	assert.Equal(t, "hello", assistantMsg.Text())
	require.Len(t, assistantMsg.ToolCalls(), 1)
	assert.Equal(t, "search", assistantMsg.ToolCalls()[0].Name)
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
		llmtest.CompletedEvent(llm.StopReasonToolUse),
	)

	searchSpec := tool.NewSpec[In]("search", "Search")
	result := llm.NewEventProcessor(context.Background(), ch).
		HandleTool(tool.Handle(searchSpec, func(_ context.Context, in In) (*Out, error) {
			return &Out{Results: []string{"golang.org"}}, nil
		})).
		Result()
	require.NoError(t, result.Error())

	next := result.Next()
	require.Len(t, next, 2)
}

func TestStreamResponse_NextAndMessage(t *testing.T) {
	ch := llmtest.SendEvents(llmtest.TextEvent("hi"), llmtest.CompletedEvent(llm.StopReasonEndTurn))

	result := llm.NewEventProcessor(context.Background(), ch).Result()
	require.NoError(t, result.Error())

	next := result.Next()
	assert.Len(t, next, 1)
	assert.Equal(t, msg.RoleAssistant, next[0].Role)
}

func TestStreamResponse_ResultIdempotent(t *testing.T) {
	ch := llmtest.SendEvents(llmtest.TextEvent("hi"), llmtest.CompletedEvent(llm.StopReasonEndTurn))

	r := llm.NewEventProcessor(context.Background(), ch)
	result1 := r.Result()
	result2 := r.Result()

	assert.Equal(t, result1, result2)
	require.NoError(t, result1.Error())
	assert.Equal(t, "hi", result1.Text())
}

// TestStreamResponse_HandleTool_BoundSpec verifies that llm.Handle(spec, fn)
// satisfies Handler and that HandleTool reads the name from the spec.
func TestStreamResponse_HandleTool_BoundSpec(t *testing.T) {
	type In struct {
		Location string `json:"location" jsonschema:"required"`
	}
	type Out struct {
		Temp int `json:"temp"`
	}

	spec := tool.NewSpec[In]("get_weather", "Get weather")

	ch := llmtest.SendEvents(
		llmtest.ToolEvent("id1", "get_weather", map[string]any{"location": "Paris"}),
		llmtest.CompletedEvent(llm.StopReasonToolUse),
	)

	result := llm.NewEventProcessor(context.Background(), ch).
		HandleTool(tool.Handle(spec, func(_ context.Context, in In) (*Out, error) {
			return &Out{Temp: 18}, nil
		})).
		Result()
	require.NoError(t, result.Error())
}
