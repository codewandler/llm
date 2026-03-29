package openrouter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
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
