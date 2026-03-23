package anthropic

import (
	"context"
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvider_CountTokens_MissingModel(t *testing.T) {
	p := New()
	_, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Messages: llm.Messages{&llm.UserMsg{Content: "hello"}},
	})
	require.Error(t, err)
}

func TestProvider_CountTokens_PerMessageLen(t *testing.T) {
	p := New()
	msgs := llm.Messages{
		&llm.SystemMsg{Content: "You are helpful."},
		&llm.UserMsg{Content: "What is 2+2?"},
		&llm.AssistantMsg{Content: "It is 4."},
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "claude-sonnet-4-5",
		Messages: msgs,
	})
	require.NoError(t, err)
	assert.Len(t, got.PerMessage, 3)
}

func TestProvider_CountTokens_RoleBreakdown(t *testing.T) {
	p := New()
	msgs := llm.Messages{
		&llm.SystemMsg{Content: "You are helpful."},
		&llm.UserMsg{Content: "What is 2+2?"},
		&llm.AssistantMsg{Content: "It is 4."},
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: msgs,
	})
	require.NoError(t, err)

	sumPerMsg := 0
	for _, n := range got.PerMessage {
		sumPerMsg += n
	}
	roleSum := got.SystemTokens + got.UserTokens + got.AssistantTokens + got.ToolResultTokens
	assert.Equal(t, sumPerMsg, roleSum)
	assert.Greater(t, got.SystemTokens, 0)
	assert.Greater(t, got.UserTokens, 0)
	assert.Greater(t, got.AssistantTokens, 0)
}

func TestProvider_CountTokens_Tools(t *testing.T) {
	p := New()
	tools := []llm.ToolDefinition{
		{Name: "lookup", Description: "Look something up", Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"q": map[string]any{"type": "string"}},
		}},
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "claude-sonnet-4-5",
		Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
		Tools:    tools,
	})
	require.NoError(t, err)
	assert.Greater(t, got.ToolsTokens, 0)
	// ToolsTokens is now raw JSON only; overhead is in OverheadTokens.
	assert.Equal(t, got.ToolsTokens, got.PerTool["lookup"])
	assert.Greater(t, got.OverheadTokens, 0, "Anthropic tool preamble must appear in OverheadTokens")
}
