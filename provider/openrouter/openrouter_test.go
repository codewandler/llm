package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

// --- Unit tests for buildRequest ---

func TestBuildRequest_ToolDefinitions(t *testing.T) {
	type GetWeatherParams struct {
		Location string `json:"location" jsonschema:"description=City name,required"`
	}

	opts := llm.StreamOptions{
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
	opts := llm.StreamOptions{
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
	opts := llm.StreamOptions{
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
	opts := llm.StreamOptions{
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
	opts := llm.StreamOptions{
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
	opts := llm.StreamOptions{
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

	events := make(chan llm.StreamEvent, 64)
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events)

	var deltas []string
	var gotDone bool

	for event := range events {
		switch event.Type {
		case llm.StreamEventDelta:
			deltas = append(deltas, event.Delta)
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

	events := make(chan llm.StreamEvent, 64)
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events)

	var toolCalls []*llm.ToolCall
	for event := range events {
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
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"tool_a"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"tool_b"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"a\":1}"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"b\":2}"}}]}}]}
data: {"choices":[{"finish_reason":"tool_calls"}]}
data: [DONE]
`

	events := make(chan llm.StreamEvent, 64)
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events)

	var toolCalls []*llm.ToolCall
	for event := range events {
		if event.Type == llm.StreamEventToolCall {
			toolCalls = append(toolCalls, event.ToolCall)
		}
	}

	require.Len(t, toolCalls, 2)

	// Order might vary, so check both are present
	toolMap := make(map[string]*llm.ToolCall)
	for _, tc := range toolCalls {
		toolMap[tc.ID] = tc
	}

	assert.Contains(t, toolMap, "call_1")
	assert.Contains(t, toolMap, "call_2")
	assert.Equal(t, "tool_a", toolMap["call_1"].Name)
	assert.Equal(t, "tool_b", toolMap["call_2"].Name)
	assert.Equal(t, float64(1), toolMap["call_1"].Arguments["a"])
	assert.Equal(t, float64(2), toolMap["call_2"].Arguments["b"])
}

func TestParseStream_ReasoningContent(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"reasoning_content":"Let me think..."}}]}
data: {"choices":[{"delta":{"content":"The answer is"}}]}
data: {"choices":[{"finish_reason":"stop"}]}
data: [DONE]
`

	events := make(chan llm.StreamEvent, 64)
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events)

	var reasoning []string
	var content []string

	for event := range events {
		switch event.Type {
		case llm.StreamEventReasoning:
			reasoning = append(reasoning, event.Reasoning)
		case llm.StreamEventDelta:
			content = append(content, event.Delta)
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

	events := make(chan llm.StreamEvent, 64)
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events)

	var usage *llm.Usage
	for event := range events {
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

	events := make(chan llm.StreamEvent, 64)
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events)

	var usage *llm.Usage
	for event := range events {
		if event.Type == llm.StreamEventDone && event.Usage != nil {
			usage = event.Usage
		}
	}

	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.InputTokens)
	assert.Equal(t, 50, usage.OutputTokens)
	assert.Equal(t, 150, usage.TotalTokens)
	assert.Equal(t, 0.005, usage.Cost)
	assert.Equal(t, 80, usage.CachedTokens)
	assert.Equal(t, 30, usage.ReasoningTokens)
}

func TestParseStream_ErrorHandling(t *testing.T) {
	sseData := `data: {"error":{"message":"Rate limit exceeded"}}
`

	events := make(chan llm.StreamEvent, 64)
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), events)

	var gotError bool
	var errorMsg string

	for event := range events {
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
	events := make(chan llm.StreamEvent, 64)

	go parseStream(ctx, io.NopCloser(strings.NewReader(sseData)), events)

	// Cancel after receiving a few events
	eventCount := 0
	for event := range events {
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

// --- Integration tests (require OPENROUTER_API_KEY) ---

func TestOpenRouter_BasicStreaming(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	provider := New(apiKey).WithDefaultModel("moonshotai/kimi-k2-0905")
	ctx := context.Background()

	stream, err := provider.CreateStream(ctx, llm.StreamOptions{
		Model: "moonshotai/kimi-k2-0905",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Say 'hello' and nothing else."},
		},
	})
	require.NoError(t, err)

	var gotDelta bool
	var gotDone bool

	for event := range stream {
		switch event.Type {
		case llm.StreamEventError:
			t.Fatalf("Unexpected error: %v", event.Error)
		case llm.StreamEventDelta:
			gotDelta = true
			t.Logf("Delta: %s", event.Delta)
		case llm.StreamEventDone:
			gotDone = true
			if event.Usage != nil {
				t.Logf("Usage: %+v", event.Usage)
			}
		}
	}

	assert.True(t, gotDelta, "Should receive at least one delta")
	assert.True(t, gotDone, "Should receive done event")
}

func TestOpenRouter_ToolCallRoundTrip(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	type GetWeatherParams struct {
		Location string `json:"location" jsonschema:"description=City name,required"`
	}

	provider := New(apiKey)
	ctx := context.Background()

	tools := []llm.ToolDefinition{
		llm.ToolDefinitionFor[GetWeatherParams]("get_weather", "Get the current weather for a location"),
	}

	// First request: model should call the tool
	t.Log("Sending initial request with tool definitions...")
	stream, err := provider.CreateStream(ctx, llm.StreamOptions{
		Model: "moonshotai/kimi-k2-0905",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "What's the weather in Paris? Use the get_weather tool."},
		},
		Tools: tools,
	})
	require.NoError(t, err)

	var toolCall *llm.ToolCall
	for event := range stream {
		switch event.Type {
		case llm.StreamEventError:
			t.Fatalf("Error in first request: %v", event.Error)
		case llm.StreamEventToolCall:
			toolCall = event.ToolCall
			t.Logf("Received tool call: %s with args %+v", toolCall.Name, toolCall.Arguments)
		case llm.StreamEventDelta:
			t.Logf("Delta: %s", event.Delta)
		}
	}

	// We expect a tool call
	if toolCall == nil {
		t.Skip("Model did not call tool (expected but not guaranteed)")
	}

	require.NotEmpty(t, toolCall.ID, "Tool call should have an ID")
	assert.Equal(t, "get_weather", toolCall.Name)

	// Simulate tool execution
	toolResult := `{"temperature": 72, "conditions": "sunny", "humidity": 65}`

	// Second request: send tool result back
	t.Log("Sending tool result back...")
	stream2, err := provider.CreateStream(ctx, llm.StreamOptions{
		Model: "moonshotai/kimi-k2-0905",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "What's the weather in Paris? Use the get_weather tool."},
			&llm.AssistantMsg{
				ToolCalls: []llm.ToolCall{
					{
						ID:        toolCall.ID,
						Name:      toolCall.Name,
						Arguments: toolCall.Arguments,
					},
				},
			},
			&llm.ToolCallResult{
				ToolCallID: toolCall.ID,
				Output:     toolResult,
			},
		},
		Tools: tools,
	})
	require.NoError(t, err)

	var finalResponse strings.Builder
	var gotDone bool

	for event := range stream2 {
		switch event.Type {
		case llm.StreamEventError:
			t.Fatalf("Error in second request: %v", event.Error)
		case llm.StreamEventDelta:
			finalResponse.WriteString(event.Delta)
		case llm.StreamEventDone:
			gotDone = true
		}
	}

	assert.True(t, gotDone)
	response := finalResponse.String()
	t.Logf("Final response: %s", response)

	// Verify the response mentions the weather data
	assert.NotEmpty(t, response, "Should get a final response")
	// The model should incorporate the tool result in its response
	// (we can't guarantee exact phrasing, but it should mention temperature or weather)
}

func TestOpenRouter_MultipleToolCalls(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}

	type LocationParams struct {
		Location string `json:"location" jsonschema:"description=Location name,required"`
	}

	provider := New(apiKey)
	ctx := context.Background()

	tools := []llm.ToolDefinition{
		llm.ToolDefinitionFor[LocationParams]("get_weather", "Get weather for a location"),
		llm.ToolDefinitionFor[LocationParams]("get_time", "Get current time for a location"),
	}

	stream, err := provider.CreateStream(ctx, llm.StreamOptions{
		Model: "moonshotai/kimi-k2-0905",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "What's the weather AND the time in London? Use both tools."},
		},
		Tools: tools,
	})
	require.NoError(t, err)

	var toolCalls []*llm.ToolCall
	for event := range stream {
		switch event.Type {
		case llm.StreamEventError:
			t.Fatalf("Error: %v", event.Error)
		case llm.StreamEventToolCall:
			toolCalls = append(toolCalls, event.ToolCall)
			t.Logf("Tool call %d: %s", len(toolCalls), event.ToolCall.Name)
		}
	}

	t.Logf("Total tool calls received: %d", len(toolCalls))

	// Models may or may not call both tools in parallel
	// Just verify we can handle multiple tool calls
	for _, tc := range toolCalls {
		assert.NotEmpty(t, tc.ID, "Each tool call should have an ID")
		assert.NotEmpty(t, tc.Name, "Each tool call should have a name")
	}
}
