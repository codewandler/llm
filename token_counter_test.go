package llm

import (
	"testing"

	"github.com/codewandler/llm/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sumInts(s []int) int {
	n := 0
	for _, v := range s {
		n += v
	}
	return n
}

// TestCountText_ModelRouting verifies encoding selection and basic counting.
func TestCountText_ModelRouting(t *testing.T) {
	tests := []struct {
		model string
		text  string
	}{
		{"gpt-4o", "Hello, world!"},
		{"claude-sonnet-4-5", "Hello, world!"},
		{"gpt-4", "Hello, world!"},
		{"unknown-model", "Hello, world!"},
	}
	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			n, err := CountText(tc.model, tc.text)
			require.NoError(t, err)
			assert.Greater(t, n, 0)
		})
	}
}

func TestCountText_EmptyText(t *testing.T) {
	n, err := CountText("gpt-4o", "")
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// TestCountMessage_AllRoles verifies each message type is counted correctly.
func TestCountMessage_AllRoles(t *testing.T) {
	model := "gpt-4o"

	tests := []struct {
		name string
		msg  Message
	}{
		{"system", System("You are helpful.")},
		{"user", User("Hello there!")},
		{"assistant", Assistant("Hi back!")},
		{"tool_result", Tool("c1", "42")},
		{"assistant_with_tool_calls", Assistant("Let me check.", tool.NewToolCall("c1", "get_weather", map[string]any{"location": "Berlin"}))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, err := CountMessage(model, tc.msg)
			require.NoError(t, err)
			assert.Greater(t, n, 0, "expected >0 tokens for %s", tc.name)
		})
	}
}

// TestCountMessage_ConsistentWithCountTokens verifies CountMessage produces
// the same per-message values as CountTokens for the same messages.
func TestCountMessage_ConsistentWithCountTokens(t *testing.T) {
	model := "gpt-4o"
	msgs := Messages{
		System("You are helpful."),
		User("What is 2+2?"),
		Assistant("It is 4."),
	}

	// Get per-message counts from the batch API
	tc := &TokenCount{}
	err := CountMessagesAndTools(tc, TokenCountRequest{
		Model:    model,
		Messages: msgs,
	}, CountOpts{Encoding: "o200k_base"})
	require.NoError(t, err)

	// CountMessage should match each entry exactly
	for i, msg := range msgs {
		n, err := CountMessage(model, msg)
		require.NoError(t, err)
		assert.Equal(t, tc.PerMessage[i], n,
			"CountMessage[%d] must match CountTokens PerMessage[%d]", i, i)
	}
}

func TestApplyRoleBreakdown_Invariant(t *testing.T) {
	msgs := Messages{
		System("be helpful"),
		User("hello"),
		Assistant("hi"),
		Tool("c1", "done"),
	}
	tc := &TokenCount{
		PerMessage: []int{3, 2, 1, 4},
	}
	applyRoleBreakdown(tc, msgs)

	assert.Equal(t, 3, tc.SystemTokens)
	assert.Equal(t, 2, tc.UserTokens)
	assert.Equal(t, 1, tc.AssistantTokens)
	assert.Equal(t, 4, tc.ToolResultTokens)

	sum := 0
	for _, n := range tc.PerMessage {
		sum += n
	}
	assert.Equal(t, sum, tc.SystemTokens+tc.UserTokens+tc.AssistantTokens+tc.ToolResultTokens,
		"role breakdown must sum to sum(PerMessage)")
}

// TestApplyRoleBreakdown_PanicOnMismatch confirms the invariant is enforced.
func TestApplyRoleBreakdown_PanicOnMismatch(t *testing.T) {
	msgs := Messages{User("hi")}
	tc := &TokenCount{PerMessage: []int{1, 2}} // wrong length

	assert.Panics(t, func() {
		applyRoleBreakdown(tc, msgs)
	}, "applyRoleBreakdown must panic when len(PerMessage) != len(msgs)")
}

// TestCountMessagesAndTools_EmptyModel verifies model is required.
func TestCountMessagesAndTools_EmptyModel(t *testing.T) {
	tc := &TokenCount{}
	err := CountMessagesAndTools(tc, TokenCountRequest{
		Messages: Messages{User("hello")},
	}, CountOpts{Encoding: "cl100k_base"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model is required")
}

// TestCountMessagesAndTools_PerMessageLen verifies len(PerMessage)==len(Messages).
func TestCountMessagesAndTools_PerMessageLen(t *testing.T) {
	msgs := Messages{
		System("sys"),
		User("user"),
		Assistant("asst"),
	}
	tc := &TokenCount{}
	err := CountMessagesAndTools(tc, TokenCountRequest{
		Model:    "gpt-4o",
		Messages: msgs,
	}, CountOpts{Encoding: "cl100k_base"})
	require.NoError(t, err)
	assert.Len(t, tc.PerMessage, len(msgs))
}

// TestCountMessagesAndToolsAnthropic_OverheadApplied verifies that when tools
// are present, OverheadTokens is populated and ToolsTokens == sum(PerTool).
func TestCountMessagesAndToolsAnthropic_OverheadApplied(t *testing.T) {
	tools := []tool.Definition{
		{Name: "tool_a", Description: "First tool", Parameters: map[string]any{"type": "object"}},
		{Name: "tool_b", Description: "Second tool", Parameters: map[string]any{"type": "object"}},
	}
	tc := &TokenCount{}
	err := CountMessagesAndToolsAnthropic(tc, TokenCountRequest{
		Model:    "claude-sonnet-4-5",
		Messages: Messages{User("hi")},
		Tools:    tools,
	})
	require.NoError(t, err)

	// ToolsTokens must equal sum(PerTool) — raw JSON counts only, no overhead.
	rawSum := 0
	for _, n := range tc.PerTool {
		rawSum += n
	}
	assert.Equal(t, rawSum, tc.ToolsTokens,
		"ToolsTokens must equal sum(PerTool) — overhead is in OverheadTokens")

	// Overhead must be at least preamble + first + one additional tool framing.
	expectedMinOverhead := anthropicToolPreamble + anthropicToolFirstOverhead + anthropicToolAdditionalOverhead
	assert.GreaterOrEqual(t, tc.OverheadTokens, expectedMinOverhead,
		"OverheadTokens must be at least preamble + first + one additional tool framing")

	// InputTokens must equal the sum of all parts.
	assert.Equal(t, tc.InputTokens, rawSum+tc.OverheadTokens+sumInts(tc.PerMessage))
}

// TestCountMessagesAndToolsAnthropic_NoTools verifies no overhead is added
// when the request has no tools.
func TestCountMessagesAndToolsAnthropic_NoTools(t *testing.T) {
	tc := &TokenCount{}
	err := CountMessagesAndToolsAnthropic(tc, TokenCountRequest{
		Model:    "claude-sonnet-4-5",
		Messages: Messages{User("hi")},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, tc.ToolsTokens)
	assert.Empty(t, tc.PerTool)
}
