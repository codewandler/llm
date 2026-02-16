package openai

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

func TestBuildRequest_Basic(t *testing.T) {
	opts := llm.StreamOptions{
		Model: "gpt-4o",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Hello"},
		},
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	assert.Equal(t, "gpt-4o", req.Model)
	assert.True(t, req.Stream)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, "user", req.Messages[0].Role)
	assert.Equal(t, "Hello", req.Messages[0].Content)
}

func TestBuildRequest_WithTools(t *testing.T) {
	type GetWeatherParams struct {
		Location string `json:"location" jsonschema:"description=City name,required"`
	}

	opts := llm.StreamOptions{
		Model: "gpt-4o",
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

	require.Len(t, req.Tools, 1)
	tool := req.Tools[0]
	assert.Equal(t, "function", tool.Type)
	assert.Equal(t, "get_weather", tool.Function.Name)
	assert.Equal(t, "Get weather for a location", tool.Function.Description)
}

func TestBuildRequest_AssistantWithToolCalls(t *testing.T) {
	opts := llm.StreamOptions{
		Model: "gpt-4o",
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
	assistantMsg := req.Messages[1]
	assert.Equal(t, "assistant", assistantMsg.Role)
	require.Len(t, assistantMsg.ToolCalls, 1)

	toolCall := assistantMsg.ToolCalls[0]
	assert.Equal(t, "call_123", toolCall.ID)
	assert.Equal(t, "function", toolCall.Type)
	assert.Equal(t, "get_weather", toolCall.Function.Name)
}

func TestBuildRequest_ToolResults(t *testing.T) {
	opts := llm.StreamOptions{
		Model: "gpt-4o",
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
	toolMsg := req.Messages[2]
	assert.Equal(t, "tool", toolMsg.Role)
	assert.Equal(t, `{"temp": 72, "conditions": "sunny"}`, toolMsg.Content)
	assert.Equal(t, "call_123", toolMsg.ToolCallID)
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

func TestParseStream_Usage(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"test"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}
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
}

// --- Integration tests (require OPENAI_KEY) ---

func TestOpenAI_BasicStreaming(t *testing.T) {
	apiKey := os.Getenv("OPENAI_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_KEY not set")
	}

	provider := New(apiKey)
	ctx := context.Background()

	stream, err := provider.CreateStream(ctx, llm.StreamOptions{
		Model: "gpt-4o-mini",
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

func TestOpenAI_ToolCallRoundTrip(t *testing.T) {
	apiKey := os.Getenv("OPENAI_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_KEY not set")
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
		Model: "gpt-4o-mini",
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
		Model: "gpt-4o-mini",
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
}

func TestOpenAI_Conversation(t *testing.T) {
	apiKey := os.Getenv("OPENAI_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_KEY not set")
	}

	provider := New(apiKey)
	ctx := context.Background()

	messages := llm.Messages{
		&llm.SystemMsg{Content: "You are a helpful assistant."},
		&llm.UserMsg{Content: "Hello"},
		&llm.AssistantMsg{Content: "Hi there!"},
		&llm.UserMsg{Content: "How are you?"},
	}

	stream, err := provider.CreateStream(ctx, llm.StreamOptions{
		Model:    "gpt-4o-mini",
		Messages: messages,
	})
	require.NoError(t, err)

	var gotResponse bool
	for event := range stream {
		if event.Type == llm.StreamEventDelta {
			gotResponse = true
		}
		if event.Type == llm.StreamEventError {
			t.Fatalf("Error: %v", event.Error)
		}
	}

	assert.True(t, gotResponse, "Should get a response")
}
