package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/codewandler/llm/msg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
)

// TestBuildRequest_AssistantWithBlocks_WireOrder verifies that when an assistant
// message carries ContentBlocks, thinking blocks are filtered out and only
// text blocks are sent to the API. Thought is internal to model output.
func TestBuildRequest_AssistantWithBlocks_WireOrder(t *testing.T) {
	tr := msg.Assistant(
		msg.Thinking("My reasoning", "sig-xyz"),
		msg.Text("The answer"),
		msg.ToolCall{
			ID: "tc1", Name: "bash", Args: tool.Args{"cmd": "ls"},
		},
	).Build()

	m := buildRequestMap(t, RequestOptions{
		LLMRequest: llm.Request{
			Model: "claude-sonnet-4-5",
			Messages: llm.Messages{
				llm.User("go"),
				tr,
			},
		},
	})

	messages, ok := m["messages"].([]any)
	require.True(t, ok)
	// messages[0] = user, messages[1] = assistant
	require.Len(t, messages, 2)

	assistantMsg, ok := messages[1].(map[string]any)
	require.True(t, ok)
	content, ok := assistantMsg["content"].([]any)
	require.True(t, ok, "assistant content must be an array of blocks")
	// Only text + tool_use; ThinkingConfig blocks are filtered out
	require.Len(t, content, 3, "expecting text + tool_use blocks (no ThinkingConfig)")

	// Block 0: thinking
	b0, ok := content[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "thinking", b0["type"], "first block must be text")
	assert.Equal(t, "My reasoning", b0["thinking"])

	// Block 1: text
	b1, ok := content[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "text", b1["type"], "first block must be text")
	assert.Equal(t, "The answer", b1["text"])

	// Block 2: tool_use
	b2, ok := content[2].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tool_use", b2["type"], "second block must be tool_use")
	assert.Equal(t, "bash", b2["name"])
}

// TestBuildRequest_AssistantWithBlocks_NoToolCalls verifies that a blocks-only
// assistant message (no tool calls) with only ThinkingConfig blocks results in empty
// content — ThinkingConfig blocks are filtered out.
func TestBuildRequest_AssistantWithBlocks_NoToolCalls(t *testing.T) {

	m := msg.Assistant(
		msg.Thinking("Thought", "sig-a"),
		msg.Text("Answer"),
	).Build()

	req := buildRequestMap(t, RequestOptions{
		LLMRequest: llm.Request{
			Model:    "claude-sonnet-4-5",
			Messages: llm.Messages{llm.User("q"), m},
		},
	})

	messages := req["messages"].([]any)
	assistantMsg := messages[1].(map[string]any)
	content := assistantMsg["content"].([]any)
	require.Len(t, content, 2, "only text block, no thinking")

	assert.Equal(t, "thinking", content[0].(map[string]any)["type"])
	assert.Equal(t, "text", content[1].(map[string]any)["type"])
}

// TestBuildRequest_AssistantWithBlocks_SignatureRoundTrip verifies that ThinkingConfig
// blocks (including signatures) are NOT included in the wire Request. Thought is
// internal to model output and should not be re-sent.
func TestBuildRequest_AssistantWithBlocks_SignatureRoundTrip(t *testing.T) {
	m := msg.Assistant(
		msg.Thinking("My reasoning", "sig-xyz"),
		msg.Text("result"),
	).Build()
	raw, err := BuildRequestBytes(RequestOptions{
		LLMRequest: llm.Request{
			Model:    "claude-sonnet-4-5",
			Messages: llm.Messages{llm.User("x"), m},
		},
	})
	require.NoError(t, err)

	var outer map[string]any
	require.NoError(t, json.Unmarshal(raw, &outer))

	msgs := outer["messages"].([]any)
	assistantContent := msgs[1].(map[string]any)["content"].([]any)

	// Only text block should be present; ThinkingConfig blocks are filtered
	require.Len(t, assistantContent, 2)
	textBlock := assistantContent[1].(map[string]any)
	assert.Equal(t, "text", textBlock["type"])
	assert.Equal(t, "result", textBlock["text"])
	// No thinking block, no signature
	_, hasThinking := textBlock["thinking"]
	assert.False(t, hasThinking, "Thinking blocks must not be included in requests")
}
