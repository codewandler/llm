package bedrock

import (
	"context"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvider_CountTokens_MissingModel(t *testing.T) {
	p := New()
	_, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Messages: llm.Messages{llm.User("hello")},
	})
	require.Error(t, err)
}

func TestProvider_CountTokens_PerMessageLen(t *testing.T) {
	p := New()
	msgs := llm.Messages{
		llm.System("You are helpful."),
		llm.User("What is 2+2?"),
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "anthropic.claude-3-5-sonnet-20241022-v2:0",
		Messages: msgs,
	})
	require.NoError(t, err)
	assert.Len(t, got.PerMessage, 2)
}

func TestProvider_CountTokens_RoleBreakdown(t *testing.T) {
	p := New()
	msgs := llm.Messages{
		llm.System("Be concise."),
		llm.User("Hello"),
		llm.Assistant("Hi there!"),
		llm.Tool("t1", "result"),
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "anthropic.claude-3-haiku-20240307-v1:0",
		Messages: msgs,
	})
	require.NoError(t, err)

	sumPerMsg := 0
	for _, n := range got.PerMessage {
		sumPerMsg += n
	}
	roleSum := got.SystemTokens + got.UserTokens + got.AssistantTokens + got.ToolResultTokens
	assert.Equal(t, sumPerMsg, roleSum)
}

func TestProvider_CountTokens_Tools(t *testing.T) {
	p := New()
	tools := []tool.Definition{
		{Name: "fetch", Description: "Fetch a URL", Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"url": map[string]any{"type": "string"}},
		}},
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "anthropic.claude-3-5-sonnet-20241022-v2:0",
		Messages: llm.Messages{llm.User("fetch this")},
		Tools:    tools,
	})
	require.NoError(t, err)
	assert.Greater(t, got.ToolsTokens, 0)
	assert.Equal(t, got.ToolsTokens, got.PerTool["fetch"])
	assert.Greater(t, got.OverheadTokens, 0, "Anthropic tool preamble must appear in OverheadTokens")
}
