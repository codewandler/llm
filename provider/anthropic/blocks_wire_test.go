package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
)

// TestBuildRequest_AssistantWithBlocks_WireOrder verifies that when an assistant
// message carries ContentBlocks, the Anthropic wire JSON serializes blocks in
// their original index order: thinking → text → tool_use.
func TestBuildRequest_AssistantWithBlocks_WireOrder(t *testing.T) {
	msg := llm.AssistantWithBlocks(
		[]llm.ContentBlock{
			{Kind: llm.ContentBlockKindThinking, Text: "My reasoning", Signature: "sig-xyz"},
			{Kind: llm.ContentBlockKindText, Text: "The answer"},
		},
		tool.NewToolCall("tc1", "bash", tool.Args{"cmd": "ls"}),
	)

	m := buildRequestMap(t, RequestOptions{
		Model: "claude-sonnet-4-5",
		StreamOptions: llm.Request{
			Model:    "claude-sonnet-4-5",
			Messages: llm.Messages{llm.User("go"), msg},
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
	require.Len(t, content, 3, "expecting thinking + text + tool_use blocks")

	// Block 0: thinking
	b0, ok := content[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "thinking", b0["type"], "first block must be thinking")
	assert.Equal(t, "My reasoning", b0["thinking"])
	assert.Equal(t, "sig-xyz", b0["signature"], "signature must survive serialization intact")

	// Block 1: text
	b1, ok := content[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "text", b1["type"], "second block must be text")
	assert.Equal(t, "The answer", b1["text"])

	// Block 2: tool_use
	b2, ok := content[2].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tool_use", b2["type"], "third block must be tool_use")
	assert.Equal(t, "bash", b2["name"])
}

// TestBuildRequest_AssistantWithBlocks_NoToolCalls verifies that a blocks-only
// assistant message (no tool calls) serializes in order without appending any
// tool_use block.
func TestBuildRequest_AssistantWithBlocks_NoToolCalls(t *testing.T) {
	msg := llm.AssistantWithBlocks([]llm.ContentBlock{
		{Kind: llm.ContentBlockKindThinking, Text: "Thinking", Signature: "sig-a"},
		{Kind: llm.ContentBlockKindText, Text: "Answer"},
	})

	m := buildRequestMap(t, RequestOptions{
		Model: "claude-sonnet-4-5",
		StreamOptions: llm.Request{
			Model:    "claude-sonnet-4-5",
			Messages: llm.Messages{llm.User("q"), msg},
		},
	})

	messages := m["messages"].([]any)
	assistantMsg := messages[1].(map[string]any)
	content := assistantMsg["content"].([]any)
	require.Len(t, content, 2, "thinking + text, no tool_use")

	assert.Equal(t, "thinking", content[0].(map[string]any)["type"])
	assert.Equal(t, "text", content[1].(map[string]any)["type"])
}

// TestBuildRequest_AssistantWithBlocks_SignatureRoundTrip verifies that a
// thinking block's signature survives the full marshal → unmarshal cycle with
// no truncation or mutation.
func TestBuildRequest_AssistantWithBlocks_SignatureRoundTrip(t *testing.T) {
	const sig = "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9.very-long-opaque-signature-value"

	msg := llm.AssistantWithBlocks([]llm.ContentBlock{
		{Kind: llm.ContentBlockKindThinking, Text: "reasoning", Signature: sig},
		{Kind: llm.ContentBlockKindText, Text: "result"},
	})

	raw, err := BuildRequest(RequestOptions{
		Model: "claude-sonnet-4-5",
		StreamOptions: llm.Request{
			Model:    "claude-sonnet-4-5",
			Messages: llm.Messages{llm.User("x"), msg},
		},
	})
	require.NoError(t, err)

	// Round-trip through raw JSON to confirm the exact byte string is preserved.
	var outer map[string]any
	require.NoError(t, json.Unmarshal(raw, &outer))

	msgs := outer["messages"].([]any)
	assistantContent := msgs[1].(map[string]any)["content"].([]any)
	thinkingBlock := assistantContent[0].(map[string]any)
	assert.Equal(t, sig, thinkingBlock["signature"],
		"signature must survive BuildRequest marshal/unmarshal with zero mutation")
}
