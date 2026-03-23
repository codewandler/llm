package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

// testMeta returns a streamMeta for testing.
func testMeta(model string) streamMeta {
	return streamMeta{
		RequestedModel: model,
		ResolvedModel:  model,
		StartTime:      time.Now(),
	}
}

// --- Unit tests for buildRequest ---

func TestBuildRequest_ToolDefinitions(t *testing.T) {
	type GetWeatherParams struct {
		Location string `json:"location" jsonschema:"description=City name,required"`
	}

	opts := llm.Request{
		Model: "test/model",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "test"},
		},
		Tools: []llm.ToolDefinition{
			llm.ToolDefinitionFor[GetWeatherParams]("get_weather", "Get weather for a location"),
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
			&llm.UserMsg{Content: "What's the weather?"},
			&llm.AssistantMsg{
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_123",
						Name: "get_weather",
						Arguments: map[string]any{
							"location": "Paris",
						},
					},
				},
			},
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

	// Arguments should be JSON string
	var args map[string]any
	err = json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
	require.NoError(t, err)
	assert.Equal(t, "Paris", args["location"])
}

func TestBuildRequest_ToolResults(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "What's the weather?"},
			&llm.AssistantMsg{
				ToolCalls: []llm.ToolCall{
					{ID: "call_123", Name: "get_weather", Arguments: map[string]any{"location": "Paris"}},
				},
			},
			&llm.ToolCallResult{
				ToolCallID: "call_123",
				Output:     `{"temp": 72, "conditions": "sunny"}`,
			},
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
			&llm.UserMsg{Content: "test"},
			&llm.AssistantMsg{
				ToolCalls: []llm.ToolCall{
					{ID: "call_123", Name: "test_tool", Arguments: map[string]any{}},
				},
			},
			&llm.ToolCallResult{
				ToolCallID: "call_123",
				Output:     "",
			},
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
			&llm.UserMsg{Content: "test"},
			&llm.AssistantMsg{
				ToolCalls: []llm.ToolCall{
					{ID: "call_1", Name: "tool_a", Arguments: map[string]any{}},
					{ID: "call_2", Name: "tool_b", Arguments: map[string]any{}},
				},
			},
			&llm.ToolCallResult{ToolCallID: "call_1", Output: "result_a"},
			&llm.ToolCallResult{ToolCallID: "call_2", Output: "result_b"},
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
			&llm.SystemMsg{Content: "You are a helpful assistant."},
			&llm.UserMsg{Content: "What's the weather in Paris?"},
			&llm.AssistantMsg{
				ToolCalls: []llm.ToolCall{
					{ID: "call_123", Name: "get_weather", Arguments: map[string]any{"location": "Paris"}},
				},
			},
			&llm.ToolCallResult{ToolCallID: "call_123", Output: `{"temp": 72}`},
			&llm.AssistantMsg{Content: "It's 72 degrees in Paris."},
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

	events := llm.NewEventStream()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events, testMeta("test/model"))

	var deltas []string
	var gotDone bool

	for event := range events.C() {
		switch event.Type {
		case llm.StreamEventDelta:
			if event.Delta != nil {
				deltas = append(deltas, event.Delta.Text)
			}
		case llm.StreamEventDone:
			gotDone = true
		case llm.StreamEventError:
			t.Fatalf("Unexpected error: %v", event.Error)
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

	events := llm.NewEventStream()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events, testMeta("test/model"))

	var toolCalls []*llm.ToolCall
	for event := range events.C() {
		if event.Type == llm.StreamEventToolCall {
			toolCalls = append(toolCalls, event.ToolCall)
		}
	}

	require.Len(t, toolCalls, 1)
	tc := toolCalls[0]
	assert.Equal(t, "call_123", tc.ID)
	assert.Equal(t, "get_weather", tc.Name)
	assert.Equal(t, "Paris", tc.Arguments["location"])
}

func TestParseStream_MultipleParallelToolCalls(t *testing.T) {
	// Arguments for index 1 arrive before index 0; must still emit in LLM-production order (0, 1).
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"tool_a"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"tool_b"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"b\":2}"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"a\":1}"}}]}}]}
data: {"choices":[{"finish_reason":"tool_calls"}]}
data: [DONE]
`

	events := llm.NewEventStream()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events, testMeta("test/model"))

	var toolCalls []*llm.ToolCall
	for event := range events.C() {
		if event.Type == llm.StreamEventToolCall {
			toolCalls = append(toolCalls, event.ToolCall)
		}
	}

	require.Len(t, toolCalls, 2)
	// Must be emitted in LLM-production order: index 0 first, then index 1
	assert.Equal(t, "call_1", toolCalls[0].ID)
	assert.Equal(t, "tool_a", toolCalls[0].Name)
	assert.Equal(t, float64(1), toolCalls[0].Arguments["a"])

	assert.Equal(t, "call_2", toolCalls[1].ID)
	assert.Equal(t, "tool_b", toolCalls[1].Name)
	assert.Equal(t, float64(2), toolCalls[1].Arguments["b"])
}

func TestParseStream_ReasoningContent(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"reasoning_content":"Let me think..."}}]}
data: {"choices":[{"delta":{"content":"The answer is"}}]}
data: {"choices":[{"finish_reason":"stop"}]}
data: [DONE]
`

	events := llm.NewEventStream()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events, testMeta("test/model"))

	var reasoning []string
	var content []string

	for event := range events.C() {
		switch event.Type {
		case llm.StreamEventDelta:
			if event.Delta == nil {
				continue
			}
			switch event.Delta.Type {
			case llm.DeltaTypeReasoning:
				reasoning = append(reasoning, event.Delta.Reasoning)
			case llm.DeltaTypeText:
				content = append(content, event.Delta.Text)
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

	events := llm.NewEventStream()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events, testMeta("test/model"))

	var usage *llm.Usage
	for event := range events.C() {
		if event.Type == llm.StreamEventDone && event.Usage != nil {
			usage = event.Usage
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

	events := llm.NewEventStream()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events, testMeta("test/model"))

	var usage *llm.Usage
	for event := range events.C() {
		if event.Type == llm.StreamEventDone && event.Usage != nil {
			usage = event.Usage
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

	events := llm.NewEventStream()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events, testMeta("test/model"))

	var gotError bool
	var errorMsg string

	for event := range events.C() {
		if event.Type == llm.StreamEventError {
			gotError = true
			errorMsg = event.Error.Error()
		}
	}

	assert.True(t, gotError)
	assert.Contains(t, errorMsg, "Rate limit exceeded")
}

func TestParseStream_ContextCancellation(t *testing.T) {
	// Long stream that would block
	sseData := strings.Repeat("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n", 1000)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := llm.NewEventStream()

	go parseStream(ctx, io.NopCloser(strings.NewReader(sseData)), events, testMeta("test/model"))

	// Cancel after receiving a few events
	eventCount := 0
	for event := range events.C() {
		eventCount++
		if eventCount == 5 {
			cancel()
		}
		if event.Type == llm.StreamEventError {
			assert.Contains(t, event.Error.Error(), "context canceled")
			return
		}
	}
}
