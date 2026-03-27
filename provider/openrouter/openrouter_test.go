package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
)

// --- Unit tests for buildRequest ---

func TestBuildRequest_ToolDefinitions(t *testing.T) {
	type GetWeatherParams struct {
		Location string `json:"location" jsonschema:"description=City name,required"`
	}

	opts := llm.Request{
		Model: "test/model",
		Messages: llm.Messages{
			llm.User("test"),
		},
		Tools: []tool.Definition{
			tool.DefinitionFor[GetWeatherParams]("get_weather", "Get weather for a location"),
		},
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	assert.Equal(t, "test/model", req.Model)
	assert.True(t, req.Stream)
	assert.True(t, req.IncludeReasoning)
	require.NotNil(t, req.StreamOptions)
	assert.True(t, req.StreamOptions.IncludeUsage)
	require.Len(t, req.Tools, 1)

	tool := req.Tools[0]
	assert.Equal(t, "function", tool.Type)
	assert.Equal(t, "get_weather", tool.Function.Name)
	assert.Equal(t, "Get weather for a location", tool.Function.Description)
	assert.NotNil(t, tool.Function.Parameters)
	assert.Equal(t, "object", tool.Function.Parameters["type"])
}

func TestBuildRequest_AssistantWithToolCalls(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: llm.Messages{
			llm.User("Whats the weather?"),
			llm.ToolCalls(
				tool.NewToolCall("call_123", "get_weather", map[string]any{"location": "Paris"}),
			),
		},
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	require.Len(t, req.Messages, 2)

	// Check assistant message
	assistantMsg := req.Messages[1]
	assert.Equal(t, "assistant", assistantMsg.Role)
	require.Len(t, assistantMsg.ToolCalls, 1)

	toolCall := assistantMsg.ToolCalls[0]
	assert.Equal(t, "call_123", toolCall.ID)
	assert.Equal(t, "function", toolCall.Type)
	assert.Equal(t, "get_weather", toolCall.Function.Name)

	// ToolArgs should be JSON string
	var args map[string]any
	err = json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
	require.NoError(t, err)
	assert.Equal(t, "Paris", args["location"])
}

func TestBuildRequest_ToolResults(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: llm.Messages{
			llm.User("Whats the weather?"),
			llm.Assistant(
				"Fetching info ...",
				tool.NewToolCall("call_123", "get_weather", map[string]any{"location": "Paris"}),
			),
			llm.Tool("call_123", `{"temp": 72, "conditions": "sunny"}`),
		},
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	require.Len(t, req.Messages, 3)

	// Check tool message
	toolMsg := req.Messages[2]
	assert.Equal(t, "tool", toolMsg.Role)
	assert.Equal(t, `{"temp": 72, "conditions": "sunny"}`, toolMsg.Content)
	assert.Equal(t, "call_123", toolMsg.ToolCallID)
}

func TestBuildRequest_ToolResultEmptyContent(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: llm.Messages{
			llm.User("test"),
			llm.Assistant("", tool.NewToolCall("call_123", "test_tool", map[string]any{})),
			llm.Tool("call_123", ""),
		},
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	toolMsg := req.Messages[2]
	assert.Equal(t, "tool", toolMsg.Role)
	// Empty content should be empty, not "<empty>"
	assert.Equal(t, "", toolMsg.Content)
	assert.Equal(t, "call_123", toolMsg.ToolCallID)
}

func TestBuildRequest_MultipleToolResults(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: llm.Messages{
			llm.User("test"),
			llm.Assistant("",
				tool.NewToolCall("call_1", "tool_a", map[string]any{}),
				tool.NewToolCall("call_2", "tool_b", map[string]any{}),
			),
			llm.Tool("call_1", "result_a"),
			llm.Tool("call_2", "result_b"),
		},
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	require.Len(t, req.Messages, 4)

	// Verify first tool result
	assert.Equal(t, "tool", req.Messages[2].Role)
	assert.Equal(t, "result_a", req.Messages[2].Content)
	assert.Equal(t, "call_1", req.Messages[2].ToolCallID)

	// Verify second tool result
	assert.Equal(t, "tool", req.Messages[3].Role)
	assert.Equal(t, "result_b", req.Messages[3].Content)
	assert.Equal(t, "call_2", req.Messages[3].ToolCallID)
}

func TestBuildRequest_FullConversationFlow(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: llm.Messages{
			llm.System("You are a helpful assistant."),
			llm.User("What's the weather in Paris?"),
			llm.Assistant("",
				tool.NewToolCall("call_123", "get_weather", map[string]any{"location": "Paris"}),
			),
			llm.Tool("call_123", `{"temp": 72}`),
			llm.Assistant("It's 72 degrees in Paris."),
		},
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	require.Len(t, req.Messages, 5)
	assert.Equal(t, "system", req.Messages[0].Role)
	assert.Equal(t, "user", req.Messages[1].Role)
	assert.Equal(t, "assistant", req.Messages[2].Role)
	assert.Equal(t, "tool", req.Messages[3].Role)
	assert.Equal(t, "assistant", req.Messages[4].Role)
}

// --- Unit tests for parseStream ---

func TestParseStream_TextDeltas(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hello"}}]}
data: {"choices":[{"delta":{"content":" world"}}]}
data: {"choices":[{"finish_reason":"stop"}]}
data: [DONE]
`

	pub, ch := llm.NewEventPublisher()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub)

	var deltas []string
	var gotDone bool

	for env := range ch {
		switch env.Type {
		case llm.StreamEventDelta:
			de := env.Data.(*llm.DeltaEvent)
			deltas = append(deltas, de.Text)
		case llm.StreamEventCompleted:
			gotDone = true
		case llm.StreamEventError:
			ee := env.Data.(*llm.ErrorEvent)
			t.Fatalf("Unexpected error: %v", ee.Error)
		}
	}

	assert.Equal(t, []string{"Hello", " world"}, deltas)
	assert.True(t, gotDone)
}

func TestParseStream_ToolCallAccumulation(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","type":"function","function":{"name":"get_weather"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location\""}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"Paris\"}"}}]}}]}
data: {"choices":[{"finish_reason":"tool_calls"}]}
data: [DONE]
`

	pub, ch := llm.NewEventPublisher()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub)

	var toolCalls []tool.Call
	for env := range ch {
		if env.Type == llm.StreamEventToolCall {
			tce := env.Data.(*llm.ToolCallEvent)
			toolCalls = append(toolCalls, tce.ToolCall)
		}
	}

	require.Len(t, toolCalls, 1)
	tc := toolCalls[0]
	assert.Equal(t, "call_123", tc.ToolCallID())
	assert.Equal(t, "get_weather", tc.ToolName())
	assert.Equal(t, "Paris", tc.ToolArgs()["location"])
}

func TestParseStream_MultipleParallelToolCalls(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"tool_a"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"tool_b"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"b\":2}"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"a\":1}"}}]}}]}
data: {"choices":[{"finish_reason":"tool_calls"}]}
data: [DONE]
`

	pub, ch := llm.NewEventPublisher()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub)

	var toolCalls []tool.Call
	for env := range ch {
		if env.Type == llm.StreamEventToolCall {
			tce := env.Data.(*llm.ToolCallEvent)
			toolCalls = append(toolCalls, tce.ToolCall)
		}
	}

	require.Len(t, toolCalls, 2)
	assert.Equal(t, "call_1", toolCalls[0].ToolCallID())
	assert.Equal(t, "tool_a", toolCalls[0].ToolName())
	assert.Equal(t, float64(1), toolCalls[0].ToolArgs()["a"])

	assert.Equal(t, "call_2", toolCalls[1].ToolCallID())
	assert.Equal(t, "tool_b", toolCalls[1].ToolName())
	assert.Equal(t, float64(2), toolCalls[1].ToolArgs()["b"])
}

func TestParseStream_ReasoningContent(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"reasoning_content":"Let me think..."}}]}
data: {"choices":[{"delta":{"content":"The answer is"}}]}
data: {"choices":[{"finish_reason":"stop"}]}
data: [DONE]
`

	pub, ch := llm.NewEventPublisher()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub)

	var reasoning []string
	var content []string

	for env := range ch {
		if env.Type == llm.StreamEventDelta {
			de := env.Data.(*llm.DeltaEvent)
			switch de.Kind {
			case llm.DeltaKindReasoning:
				reasoning = append(reasoning, de.Reasoning)
			case llm.DeltaKindText:
				content = append(content, de.Text)
			}
		}
	}

	assert.Equal(t, []string{"Let me think..."}, reasoning)
	assert.Equal(t, []string{"The answer is"}, content)
}

func TestParseStream_UsageData(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"test"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"cost":0.001}}
data: {"choices":[{"finish_reason":"stop"}]}
data: [DONE]
`

	pub, ch := llm.NewEventPublisher()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub)

	var usage *llm.Usage
	for env := range ch {
		if env.Type == llm.StreamEventUsageUpdated {
			ue := env.Data.(*llm.UsageUpdatedEvent)
			usage = &ue.Usage
		}
	}

	require.NotNil(t, usage)
	assert.Equal(t, 10, usage.InputTokens)
	assert.Equal(t, 5, usage.OutputTokens)
	assert.Equal(t, 15, usage.TotalTokens)
	assert.Equal(t, 0.001, usage.Cost)
}

func TestParseStream_UsageWithDetails(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"test"}}]}
data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150,"cost":0.005,"prompt_tokens_details":{"cached_tokens":80},"completion_tokens_details":{"reasoning_tokens":30}}}
data: [DONE]
`

	pub, ch := llm.NewEventPublisher()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub)

	var usage *llm.Usage
	for env := range ch {
		if env.Type == llm.StreamEventUsageUpdated {
			ue := env.Data.(*llm.UsageUpdatedEvent)
			usage = &ue.Usage
		}
	}

	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.InputTokens)
	assert.Equal(t, 50, usage.OutputTokens)
	assert.Equal(t, 150, usage.TotalTokens)
	assert.Equal(t, 0.005, usage.Cost)
	assert.Equal(t, 80, usage.CacheReadTokens)
	assert.Equal(t, 30, usage.ReasoningTokens)
}

func TestParseStream_ErrorHandling(t *testing.T) {
	sseData := `data: {"error":{"message":"Rate limit exceeded"}}
`

	pub, ch := llm.NewEventPublisher()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub)

	var gotError bool
	var errorMsg string

	for env := range ch {
		if env.Type == llm.StreamEventError {
			gotError = true
			ee := env.Data.(*llm.ErrorEvent)
			errorMsg = ee.Error.Error()
		}
	}

	assert.True(t, gotError)
	assert.Contains(t, errorMsg, "Rate limit exceeded")
}

func TestParseStream_ContextCancellation(t *testing.T) {
	sseData := strings.Repeat("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n", 1000)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pub, ch := llm.NewEventPublisher()

	go parseStream(ctx, io.NopCloser(strings.NewReader(sseData)), pub)

	eventCount := 0
	for env := range ch {
		eventCount++
		if eventCount == 5 {
			cancel()
		}
		if env.Type == llm.StreamEventError {
			ee := env.Data.(*llm.ErrorEvent)
			assert.Contains(t, ee.Error.Error(), "context canceled")
			return
		}
	}
}

// TestParseStream_ToolFlushOnlyOnToolCalls verifies that tool call buffers are
// NOT flushed when finish_reason == "stop" (normal text completion) and ARE
// flushed when finish_reason == "tool_calls".
func TestParseStream_ToolFlushOnlyOnToolCalls(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hello"}}]}
data: {"choices":[{"finish_reason":"stop"}]}
data: [DONE]
`
	pub, ch := llm.NewEventPublisher()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub)

	var toolCalls []tool.Call
	var stopReason llm.StopReason
	for env := range ch {
		switch env.Type {
		case llm.StreamEventToolCall:
			tce := env.Data.(*llm.ToolCallEvent)
			toolCalls = append(toolCalls, tce.ToolCall)
		case llm.StreamEventCompleted:
			ce := env.Data.(*llm.CompletedEvent)
			stopReason = ce.StopReason
		}
	}

	assert.Empty(t, toolCalls, "no tool calls expected for a text stop")
	assert.Equal(t, llm.StopReasonEndTurn, stopReason)
}
