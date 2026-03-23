package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyRoleBreakdown_Invariant verifies role sums equal sum(PerMessage).
func TestApplyRoleBreakdown_Invariant(t *testing.T) {
	msgs := Messages{
		&SystemMsg{Content: "be helpful"},
		&UserMsg{Content: "hello"},
		&AssistantMsg{Content: "hi"},
		&ToolCallResult{ToolCallID: "c1", Output: "done"},
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
	msgs := Messages{&UserMsg{Content: "hi"}}
	tc := &TokenCount{PerMessage: []int{1, 2}} // wrong length

	assert.Panics(t, func() {
		applyRoleBreakdown(tc, msgs)
	}, "applyRoleBreakdown must panic when len(PerMessage) != len(msgs)")
}

// TestCountMessagesAndTools_EmptyModel verifies model is required.
func TestCountMessagesAndTools_EmptyModel(t *testing.T) {
	tc := &TokenCount{}
	err := CountMessagesAndTools(tc, TokenCountRequest{
		Messages: Messages{&UserMsg{Content: "hello"}},
	}, "cl100k_base", 0, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model is required")
}

// TestCountMessagesAndTools_PerMessageLen verifies len(PerMessage)==len(Messages).
func TestCountMessagesAndTools_PerMessageLen(t *testing.T) {
	msgs := Messages{
		&SystemMsg{Content: "sys"},
		&UserMsg{Content: "user"},
		&AssistantMsg{Content: "asst"},
	}
	tc := &TokenCount{}
	err := CountMessagesAndTools(tc, TokenCountRequest{
		Model:    "gpt-4o",
		Messages: msgs,
	}, "cl100k_base", 0, 0)
	require.NoError(t, err)
	assert.Len(t, tc.PerMessage, len(msgs))
}

// TestCountMessagesAndToolsAnthropic_OverheadApplied verifies that when tools
// are present, ToolsTokens > sum(PerTool values) due to the injected preamble
// and per-tool framing overhead.
func TestCountMessagesAndToolsAnthropic_OverheadApplied(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "tool_a", Description: "First tool", Parameters: map[string]any{"type": "object"}},
		{Name: "tool_b", Description: "Second tool", Parameters: map[string]any{"type": "object"}},
	}
	tc := &TokenCount{}
	err := CountMessagesAndToolsAnthropic(tc, TokenCountRequest{
		Model:    "claude-sonnet-4-5",
		Messages: Messages{&UserMsg{Content: "hi"}},
		Tools:    tools,
	})
	require.NoError(t, err)

	rawSum := 0
	for _, n := range tc.PerTool {
		rawSum += n
	}

	assert.Greater(t, tc.ToolsTokens, rawSum,
		"ToolsTokens must exceed sum(PerTool) due to Anthropic preamble+framing overhead")

	overhead := tc.ToolsTokens - rawSum
	expectedMinOverhead := anthropicToolPreamble + anthropicToolFirstOverhead + anthropicToolAdditionalOverhead
	assert.GreaterOrEqual(t, overhead, expectedMinOverhead,
		"overhead must be at least preamble + first + one additional tool framing")
}

// TestCountMessagesAndToolsAnthropic_NoTools verifies no overhead is added
// when the request has no tools.
func TestCountMessagesAndToolsAnthropic_NoTools(t *testing.T) {
	tc := &TokenCount{}
	err := CountMessagesAndToolsAnthropic(tc, TokenCountRequest{
		Model:    "claude-sonnet-4-5",
		Messages: Messages{&UserMsg{Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, tc.ToolsTokens)
	assert.Empty(t, tc.PerTool)
}
