package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

func TestBuildRequest_SystemMessage(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Hello"),
		),
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	require.Len(t, req.Messages, 2)
	assert.Equal(t, "system", req.Messages[0].Role)
	assert.Equal(t, "You are a helpful assistant.", req.Messages[0].Content)
}

func TestBuildRequest_UserMessage(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: msg.BuildTranscript(
			msg.User("What is the weather?"),
		),
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	require.Len(t, req.Messages, 1)
	assert.Equal(t, "user", req.Messages[0].Role)
	assert.Equal(t, "What is the weather?", req.Messages[0].Content)
}

func TestBuildRequest_AssistantMessage(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: msg.BuildTranscript(
			msg.User("Hello"),
			msg.Assistant(msg.Text("Hi there!")),
		),
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	require.Len(t, req.Messages, 2)
	assert.Equal(t, "assistant", req.Messages[1].Role)
	assert.Equal(t, "Hi there!", req.Messages[1].Content)
}

func TestBuildRequest_AssistantWithToolCalls(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: msg.BuildTranscript(
			msg.User("Whats the weather?"),
			msg.Assistant(
				msg.ToolCall(msg.NewToolCall("call_123", "get_weather", msg.ToolArgs{"location": "Paris"})),
			),
		),
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
		Messages: msg.BuildTranscript(
			msg.User("Whats the weather?"),
			msg.Assistant(
				msg.Text("Fetching info ..."),
				msg.ToolCall(msg.NewToolCall("call_123", "get_weather", msg.ToolArgs{"location": "Paris"})),
			),
			msg.Tool().Results(msg.ToolResult{
				ToolCallID: "call_123",
				ToolOutput: `{"temp": 72, "conditions": "sunny"}`,
			}),
		),
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	// Find the tool message
	var toolMsg *messagePayload
	for i := range req.Messages {
		if req.Messages[i].Role == "tool" {
			toolMsg = &req.Messages[i]
			break
		}
	}
	require.NotNil(t, toolMsg, "tool message should exist")
	assert.Equal(t, `{"temp": 72, "conditions": "sunny"}`, toolMsg.Content)
	assert.Equal(t, "call_123", toolMsg.ToolCallID)
}

func TestBuildRequest_ToolResultEmptyContent(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: msg.BuildTranscript(
			msg.User("test"),
			msg.Assistant(
				msg.ToolCall(msg.NewToolCall("call_123", "test_tool", nil)),
			),
			msg.Tool().Results(msg.ToolResult{
				ToolCallID: "call_123",
				ToolOutput: "",
			}),
		),
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	// Find the tool message
	var toolMsg *messagePayload
	for i := range req.Messages {
		if req.Messages[i].Role == "tool" {
			toolMsg = &req.Messages[i]
			break
		}
	}
	require.NotNil(t, toolMsg, "tool message should exist")
	assert.Equal(t, "tool", toolMsg.Role)
	// Empty content should be empty
	assert.Equal(t, "", toolMsg.Content)
	assert.Equal(t, "call_123", toolMsg.ToolCallID)
}

func TestBuildRequest_MultipleToolResults(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: msg.BuildTranscript(
			msg.User("test"),
			msg.Assistant(
				msg.ToolCall(msg.NewToolCall("call_1", "tool_a", nil)),
				msg.ToolCall(msg.NewToolCall("call_2", "tool_b", nil)),
			),
			msg.Tool().Results(msg.ToolResult{ToolCallID: "call_1", ToolOutput: "result a"}),
			msg.Tool().Results(msg.ToolResult{ToolCallID: "call_2", ToolOutput: "result b"}),
		),
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	// Count tool messages
	var toolMsgCount int
	for _, m := range req.Messages {
		if m.Role == "tool" {
			toolMsgCount++
		}
	}
	assert.Equal(t, 2, toolMsgCount, "should have 2 tool messages")
}

func TestBuildRequest_ToolResultsMultiple(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: msg.BuildTranscript(
			msg.User("test"),
			msg.Assistant(
				msg.Text("Checking..."),
				msg.ToolCall(msg.NewToolCall("call_1", "tool_a", nil)),
				msg.ToolCall(msg.NewToolCall("call_2", "tool_b", nil)),
			),
			msg.Tool().Results(msg.ToolResult{ToolCallID: "call_1", ToolOutput: "result a"}),
			msg.Tool().Results(msg.ToolResult{ToolCallID: "call_2", ToolOutput: "result b"}),
		),
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	// Find tool messages by their tool_call_id
	toolMsgs := make(map[string]string)
	for _, m := range req.Messages {
		if m.Role == "tool" {
			toolMsgs[m.ToolCallID] = m.Content
		}
	}
	assert.Equal(t, "result a", toolMsgs["call_1"])
	assert.Equal(t, "result b", toolMsgs["call_2"])
}

func TestBuildRequest_NoTools(t *testing.T) {
	opts := llm.Request{
		Model: "test/model",
		Messages: msg.BuildTranscript(
			msg.User("Hello"),
		),
		Tools: []tool.Definition{}, // explicitly empty
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	// Should not include "tools" field when empty
	assert.False(t, strings.Contains(string(body), `"tools":[]`))
}

func TestBuildRequest_ModelPassthrough(t *testing.T) {
	opts := llm.Request{
		Model: "openrouter/ai/anthropic/claude-3-5-sonnet",
		Messages: msg.BuildTranscript(
			msg.User("Hello"),
		),
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	assert.Equal(t, "openrouter/ai/anthropic/claude-3-5-sonnet", req.Model)
}

func TestBuildRequest_MaxTokens(t *testing.T) {
	opts := llm.Request{
		Model:     "test/model",
		MaxTokens: 1000,
		Messages: msg.BuildTranscript(
			msg.User("Hello"),
		),
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	assert.Equal(t, 1000, req.MaxTokens)
}

func TestBuildRequest_Temperature(t *testing.T) {
	opts := llm.Request{
		Model:       "test/model",
		Temperature: 0.7,
		Messages: msg.BuildTranscript(
			msg.User("Hello"),
		),
	}

	body, err := buildRequest(opts)
	require.NoError(t, err)

	var req request
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	assert.Equal(t, 0.7, req.Temperature)
}

func TestProvider_ResolveDefaultModel(t *testing.T) {
	p := New()

	m, err := p.Resolve(llm.ModelDefault)
	require.NoError(t, err)
	assert.Equal(t, "openrouter/auto", m.ID)
}

func TestProvider_ResolveAutoAlias(t *testing.T) {
	p := New()

	m, err := p.Resolve("auto")
	require.NoError(t, err)
	assert.Equal(t, "openrouter/auto", m.ID)
}

func TestProvider_CreateStream_DefaultModelApplied(t *testing.T) {
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotModel, _ = body["model"].(string)

		w.Header().Set("Content-Type", "text/event-stream")
		_, err := io.WriteString(w, "data: {\"id\":\"req_1\",\"model\":\"openai/gpt-4o\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		require.NoError(t, err)
		_, err = io.WriteString(w, "data: [DONE]\n\n")
		require.NoError(t, err)
	}))
	defer server.Close()

	p := New(
		llm.WithBaseURL(server.URL),
		llm.WithAPIKey("test-key"),
	).WithDefaultModel("openai/gpt-4o")

	stream, err := p.CreateStream(t.Context(), llm.Request{
		Messages: msg.BuildTranscript(msg.User("Hello")),
	})
	require.NoError(t, err)

	for range stream {
	}

	assert.Equal(t, "openai/gpt-4o", gotModel)
}

func TestProvider_CreateStream_ExplicitAutoModelPreservedWithCustomDefault(t *testing.T) {
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotModel, _ = body["model"].(string)

		w.Header().Set("Content-Type", "text/event-stream")
		_, err := io.WriteString(w, "data: {\"id\":\"req_1\",\"model\":\"openrouter/auto\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		require.NoError(t, err)
		_, err = io.WriteString(w, "data: [DONE]\n\n")
		require.NoError(t, err)
	}))
	defer server.Close()

	p := New(
		llm.WithBaseURL(server.URL),
		llm.WithAPIKey("test-key"),
	).WithDefaultModel("openai/gpt-4o")

	stream, err := p.CreateStream(t.Context(), llm.Request{
		Model:    "auto",
		Messages: msg.BuildTranscript(msg.User("Hello")),
	})
	require.NoError(t, err)

	for range stream {
	}

	assert.Equal(t, "auto", gotModel)
}

func TestBuildRequest_DoesNotEnableReasoningByDefault(t *testing.T) {
	body, err := buildRequest(llm.Request{
		Model: "test/model",
		Messages: msg.BuildTranscript(
			msg.User("Hello"),
		),
	})
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))

	assert.NotContains(t, req, "include_reasoning")
	assert.NotContains(t, req, "reasoning_effort")
	assert.NotContains(t, req, "reasoning")
}

func TestBuildRequest_EffortUsesReasoningObject(t *testing.T) {
	body, err := buildRequest(llm.Request{
		Model:    "test/model",
		Effort:   llm.EffortHigh,
		Thinking: llm.ThinkingOn,
		Messages: msg.BuildTranscript(
			msg.User("Hello"),
		),
	})
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))

	reasoning, ok := req["reasoning"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, string(llm.EffortHigh), reasoning["effort"])
}

func TestBuildRequest_AssistantThinkingIncluded(t *testing.T) {
	body, err := buildRequest(llm.Request{
		Model: "test/model",
		Messages: msg.BuildTranscript(
			msg.User("Hello"),
			msg.Assistant(
				msg.Thinking("Let me think", "sig-1"),
				msg.Text("Hi there!"),
			),
		),
	})
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))

	messages, ok := req["messages"].([]any)
	require.True(t, ok)
	require.Len(t, messages, 2)

	assistantMsg, ok := messages[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Hi there!", assistantMsg["content"])
	assert.Equal(t, "Let me think", assistantMsg["reasoning"])
}

func TestParseStream_ToolCallAccumulation(t *testing.T) {
	events := collectStreamEvents(t, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","type":"function","function":{"name":"get_weather"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location\""}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"Paris\"}"}}]}}]}

data: {"choices":[{"finish_reason":"tool_calls"}]}

data: [DONE]

`, "")

	var toolCalls []tool.Call
	for _, event := range events {
		if event.Type == llm.StreamEventToolCall {
			toolCalls = append(toolCalls, event.Data.(*llm.ToolCallEvent).ToolCall)
		}
	}

	require.Len(t, toolCalls, 1)
	assert.Equal(t, "call_123", toolCalls[0].ToolCallID())
	assert.Equal(t, "get_weather", toolCalls[0].ToolName())
	assert.Equal(t, "Paris", toolCalls[0].ToolArgs()["location"])
}

func TestParseStream_DoesNotEmitToolCallsOnStopFinishReason(t *testing.T) {
	events := collectStreamEvents(t, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","type":"function","function":{"name":"get_weather"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location\":\"Paris\"}"}}]}}]}

data: {"choices":[{"finish_reason":"stop"}]}

data: [DONE]

`, "")

	for _, event := range events {
		if event.Type == llm.StreamEventCreated {
			continue
		}
		assert.NotEqual(t, llm.StreamEventToolCall, event.Type)
	}
}

func TestParseStream_UsageWithDetails(t *testing.T) {
	events := collectStreamEvents(t, `data: {"choices":[{"delta":{"content":"test"}}]}

data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150,"cost":0.123,"prompt_tokens_details":{"cached_tokens":80},"completion_tokens_details":{"reasoning_tokens":30}}}

data: [DONE]

`, "")

	var usageRec *usage.Record
	for _, event := range events {
		if event.Type == llm.StreamEventUsageUpdated {
			u := event.Data.(*llm.UsageUpdatedEvent)
			usageRec = &u.Record
		}
	}

	require.NotNil(t, usageRec)
	// 100 - 80 cached = 20 regular input; 50 - 30 reasoning = 20 regular output
	assert.Equal(t, 20, usageRec.Tokens.Count(usage.KindInput))
	assert.Equal(t, 20, usageRec.Tokens.Count(usage.KindOutput))
	assert.Equal(t, 80, usageRec.Tokens.Count(usage.KindCacheRead))
	assert.Equal(t, 30, usageRec.Tokens.Count(usage.KindReasoning))
	// OpenRouter reports cost directly → Source == "reported"
	assert.Equal(t, "reported", usageRec.Cost.Source)
	assert.InDelta(t, 0.123, usageRec.Cost.Total, 1e-10)
}

func TestParseStream_ReasoningDetails(t *testing.T) {
	events := collectStreamEvents(t, `data: {"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.text","text":"step 1","index":0}]}}]}

data: [DONE]

`, "")

	var thoughts []string
	for _, event := range events {
		if event.Type == llm.StreamEventDelta {
			delta := event.Data.(*llm.DeltaEvent)
			if delta.Kind == llm.DeltaKindThinking {
				thoughts = append(thoughts, delta.Thinking)
			}
		}
	}

	assert.Equal(t, []string{"step 1"}, thoughts)
}

func TestParseStream_ErrorBeforeStart(t *testing.T) {
	events := collectStreamEvents(t, `data: {"error":{"message":"boom"}}

`, "")

	require.Len(t, events, 2)
	assert.Equal(t, llm.StreamEventCreated, events[0].Type)
	assert.Equal(t, llm.StreamEventError, events[1].Type)
	for _, event := range events {
		if event.Type == llm.StreamEventStarted {
			assert.Fail(t, "unexpected started event before error")
		}
	}
}

func TestProvider_CountTokens_NormalizesProviderPrefix(t *testing.T) {
	p := New()
	req := tokencount.TokenCountRequest{
		Model:    "openai/gpt-4o",
		Messages: msg.BuildTranscript(msg.User("Count these tokens carefully.")),
		Tools: []tool.Definition{
			tool.DefinitionFor[struct {
				Location string `json:"location" jsonschema:"required"`
			}]("get_weather", "Get weather"),
		},
	}

	got, err := p.CountTokens(context.Background(), req)
	require.NoError(t, err)

	expected := &tokencount.TokenCount{}
	err = tokencount.CountMessagesAndTools(expected, tokencount.TokenCountRequest{
		Model:    "gpt-4o",
		Messages: req.Messages,
		Tools:    req.Tools,
	}, tokencount.CountOpts{Encoding: tokencount.EncodingO200K})
	require.NoError(t, err)

	assert.Equal(t, expected.InputTokens, got.InputTokens)
	assert.Equal(t, expected.PerMessage, got.PerMessage)
	assert.Equal(t, expected.ToolsTokens, got.ToolsTokens)
	assert.Equal(t, expected.PerTool, got.PerTool)
}

func TestProvider_CountTokens_UsesDefaultModelWhenEmpty(t *testing.T) {
	p := New().WithDefaultModel("openai/gpt-4o")

	got, err := p.CountTokens(context.Background(), tokencount.TokenCountRequest{
		Messages: msg.BuildTranscript(msg.User("Hello")),
	})
	require.NoError(t, err)
	assert.Positive(t, got.InputTokens)
}

func collectStreamEvents(t *testing.T, sseData string, requestedModel string) []llm.Envelope {
	t.Helper()

	pub, ch := llm.NewEventPublisher()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, requestedModel, nil)

	var events []llm.Envelope
	for event := range ch {
		events = append(events, event)
	}
	return events
}

func TestBuildRequest_AssistantInterleavedThinkingOrder(t *testing.T) {
	// Verify that reasoning_details preserve the part emission order
	// when thinking is interleaved with tool calls.
	body, err := buildRequest(llm.Request{
		Model: "test/model",
		Messages: msg.BuildTranscript(
			msg.User("find it"),
			msg.Assistant(
				msg.Thinking("Plan search", "sig-1"),
				msg.ToolCall{ID: "tc1", Name: "search", Args: tool.Args{"q": "go"}},
				msg.Thinking("Evaluate results", "sig-2"),
				msg.Text("Here are the results"),
			),
		),
	})
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))

	messages := req["messages"].([]any)
	assistantMsg := messages[1].(map[string]any)

	// reasoning_details must have 2 entries in the correct order
	details := assistantMsg["reasoning_details"].([]any)
	require.Len(t, details, 2)
	assert.Equal(t, "Plan search", details[0].(map[string]any)["text"])
	assert.Equal(t, "sig-1", details[0].(map[string]any)["signature"])
	assert.Equal(t, "Evaluate results", details[1].(map[string]any)["text"])
	assert.Equal(t, "sig-2", details[1].(map[string]any)["signature"])

	// reasoning string must concatenate thinking texts
	assert.Equal(t, "Plan searchEvaluate results", assistantMsg["reasoning"])

	// tool_calls must be present
	toolCalls := assistantMsg["tool_calls"].([]any)
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "tc1", toolCalls[0].(map[string]any)["id"])
}
