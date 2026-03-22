package anthropic

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderNameAndModels(t *testing.T) {
	t.Parallel()

	p := New()
	assert.Equal(t, providerName, p.Name())
	models := p.Models()
	require.NotEmpty(t, models)
	assert.Equal(t, providerName, models[0].Provider)
}

func TestNewAPIRequestHeaders(t *testing.T) {
	t.Parallel()

	p := New(llm.WithBaseURL("https://example.test"), llm.WithAPIKey("k"))
	req, err := p.newAPIRequest(context.Background(), "token-123", []byte(`{"ok":true}`))
	require.NoError(t, err)

	assert.Equal(t, "https://example.test/v1/messages", req.URL.String())
	assert.Equal(t, "token-123", req.Header.Get("x-api-key"))
	assert.Equal(t, anthropicVersion, req.Header.Get("Anthropic-Version"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

func TestBuildRequest_SystemAndTools(t *testing.T) {
	t.Parallel()

	body, err := BuildRequest(RequestOptions{
		Model: "claude-sonnet-4-5-20250929",
		StreamOptions: llm.StreamRequest{
			Model: "claude-sonnet-4-5-20250929",
			Messages: llm.Messages{
				&llm.SystemMsg{Content: "system prompt"},
				&llm.UserMsg{Content: "hello"},
			},
			Tools: []llm.ToolDefinition{{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  map[string]any{"type": "object"},
			}},
			ToolChoice: llm.ToolChoiceAuto{},
		},
	})
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))

	// System should now be an array of text blocks
	system, ok := req["system"].([]any)
	require.True(t, ok, "system should be an array")
	require.Len(t, system, 1)
	block := system[0].(map[string]any)
	assert.Equal(t, "text", block["type"])
	assert.Equal(t, "system prompt", block["text"])

	tools := req["tools"].([]any)
	require.Len(t, tools, 1)
	assert.Equal(t, "get_weather", tools[0].(map[string]any)["name"])
	require.NotNil(t, req["tool_choice"])
}

func TestBuildRequest_MultipleSystemMessages(t *testing.T) {
	t.Parallel()

	t.Run("consecutive system messages are accumulated", func(t *testing.T) {
		body, err := BuildRequest(RequestOptions{
			Model: "claude-sonnet-4-5-20250929",
			StreamOptions: llm.StreamRequest{
				Model: "claude-sonnet-4-5-20250929",
				Messages: llm.Messages{
					&llm.SystemMsg{Content: "first instruction"},
					&llm.SystemMsg{Content: "second instruction"},
					&llm.UserMsg{Content: "hello"},
				},
			},
		})
		require.NoError(t, err)

		var req map[string]any
		require.NoError(t, json.Unmarshal(body, &req))

		system, ok := req["system"].([]any)
		require.True(t, ok, "system should be an array")
		require.Len(t, system, 2)

		assert.Equal(t, "first instruction", system[0].(map[string]any)["text"])
		assert.Equal(t, "second instruction", system[1].(map[string]any)["text"])
	})

	t.Run("mid-conversation system messages are accumulated", func(t *testing.T) {
		body, err := BuildRequest(RequestOptions{
			Model: "claude-sonnet-4-5-20250929",
			StreamOptions: llm.StreamRequest{
				Model: "claude-sonnet-4-5-20250929",
				Messages: llm.Messages{
					&llm.SystemMsg{Content: "initial system"},
					&llm.UserMsg{Content: "hello"},
					&llm.AssistantMsg{Content: "hi there"},
					&llm.SystemMsg{Content: "additional context"},
					&llm.UserMsg{Content: "continue"},
				},
			},
		})
		require.NoError(t, err)

		var req map[string]any
		require.NoError(t, json.Unmarshal(body, &req))

		system, ok := req["system"].([]any)
		require.True(t, ok, "system should be an array")
		require.Len(t, system, 2)

		assert.Equal(t, "initial system", system[0].(map[string]any)["text"])
		assert.Equal(t, "additional context", system[1].(map[string]any)["text"])
	})

	t.Run("empty system messages are filtered out", func(t *testing.T) {
		body, err := BuildRequest(RequestOptions{
			Model: "claude-sonnet-4-5-20250929",
			StreamOptions: llm.StreamRequest{
				Model: "claude-sonnet-4-5-20250929",
				Messages: llm.Messages{
					&llm.SystemMsg{Content: "keep this"},
					&llm.SystemMsg{Content: "   "}, // whitespace only
					&llm.SystemMsg{Content: ""},    // empty
					&llm.UserMsg{Content: "hello"},
				},
			},
		})
		require.NoError(t, err)

		var req map[string]any
		require.NoError(t, json.Unmarshal(body, &req))

		system, ok := req["system"].([]any)
		require.True(t, ok, "system should be an array")
		require.Len(t, system, 1)
		assert.Equal(t, "keep this", system[0].(map[string]any)["text"])
	})

	t.Run("no system messages results in nil system field", func(t *testing.T) {
		body, err := BuildRequest(RequestOptions{
			Model: "claude-sonnet-4-5-20250929",
			StreamOptions: llm.StreamRequest{
				Model: "claude-sonnet-4-5-20250929",
				Messages: llm.Messages{
					&llm.UserMsg{Content: "hello"},
				},
			},
		})
		require.NoError(t, err)

		var req map[string]any
		require.NoError(t, json.Unmarshal(body, &req))

		_, exists := req["system"]
		assert.False(t, exists, "system field should not exist when no system messages")
	})
}

func TestBuildRequest_ToolCallWithNilArguments(t *testing.T) {
	t.Parallel()

	// This tests the fix for: "messages.N.content.0.tool_use.input: Field required"
	// When a tool call has nil Arguments, the serialized JSON must still include
	// the "input" field (as an empty object) because Anthropic API requires it.
	body, err := BuildRequest(RequestOptions{
		Model: "claude-sonnet-4-5-20250929",
		StreamOptions: llm.StreamRequest{
			Model: "claude-sonnet-4-5-20250929",
			Messages: llm.Messages{
				&llm.UserMsg{Content: "hello"},
				&llm.AssistantMsg{
					Content: "",
					ToolCalls: []llm.ToolCall{
						{ID: "call_123", Name: "get_time", Arguments: nil}, // nil arguments
					},
				},
				&llm.ToolCallResult{ToolCallID: "call_123", Output: "12:00"},
			},
		},
	})
	require.NoError(t, err)

	// Parse the JSON and verify the tool_use block has "input" field
	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))

	messages := req["messages"].([]any)
	require.Len(t, messages, 3) // user, assistant (with tool_use), user (with tool_result)

	// Second message should be the assistant message with tool_use
	assistantMsg := messages[1].(map[string]any)
	assert.Equal(t, "assistant", assistantMsg["role"])

	content := assistantMsg["content"].([]any)
	require.Len(t, content, 1)

	toolUse := content[0].(map[string]any)
	assert.Equal(t, "tool_use", toolUse["type"])
	assert.Equal(t, "call_123", toolUse["id"])
	assert.Equal(t, "get_time", toolUse["name"])

	// Critical: "input" must be present and be an empty object, not nil/missing
	input, exists := toolUse["input"]
	require.True(t, exists, "input field must be present in tool_use block")
	require.NotNil(t, input, "input field must not be nil")
	inputMap, ok := input.(map[string]any)
	require.True(t, ok, "input must be an object")
	assert.Empty(t, inputMap, "input should be an empty object for nil arguments")
}

func TestEnsureInputMap(t *testing.T) {
	t.Parallel()

	t.Run("nil input returns empty map", func(t *testing.T) {
		result := ensureInputMap(nil)
		require.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("non-nil input returned as-is", func(t *testing.T) {
		input := map[string]any{"key": "value"}
		result := ensureInputMap(input)
		assert.Equal(t, input, result)
	})
}
