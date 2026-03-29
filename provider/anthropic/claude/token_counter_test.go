package claude

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/tool"
)

func TestProvider_CountTokens_MissingModel(t *testing.T) {
	p := New()
	_, err := p.CountTokens(context.Background(), tokencount.TokenCountRequest{
		Messages: llm.Messages{llm.User("hello")},
	})
	require.Error(t, err)
}

func TestProvider_CountTokens_IncludesInjectedSystemBlocks(t *testing.T) {
	p := New()

	onlyInjected, err := p.CountTokens(context.Background(), tokencount.TokenCountRequest{
		Model:    "claude-haiku-4-5",
		Messages: llm.Messages{llm.User("hi")},
	})
	require.NoError(t, err)

	withUserSystem, err := p.CountTokens(context.Background(), tokencount.TokenCountRequest{
		Model: "claude-haiku-4-5",
		Messages: llm.Messages{
			llm.System("You are helpful."),
			llm.User("hi"),
		},
	})
	require.NoError(t, err)

	assert.Greater(t, withUserSystem.SystemTokens, onlyInjected.SystemTokens,
		"adding a user system message should increase SystemTokens")
	assert.Greater(t, withUserSystem.InputTokens, onlyInjected.InputTokens,
		"adding a user system message should increase InputTokens")

	assert.Greater(t, onlyInjected.OverheadTokens, 30,
		"injected system blocks should contribute >30 tokens in OverheadTokens")
}

func TestProvider_CountTokens_PerMessageLen(t *testing.T) {
	p := New()
	msgs := llm.Messages{
		llm.System("You are helpful."),
		llm.User("What is 2+2?"),
		llm.Assistant("It is 4."),
	}
	got, err := p.CountTokens(context.Background(), tokencount.TokenCountRequest{
		Model:    "claude-haiku-4-5",
		Messages: msgs,
	})
	require.NoError(t, err)
	assert.Len(t, got.PerMessage, 3, "PerMessage must have one entry per message")
}

func TestProvider_CountTokens_RoleBreakdown(t *testing.T) {
	p := New()
	msgs := llm.Messages{
		llm.System("You are helpful."),
		llm.User("Hello"),
		llm.Assistant("Hi there!"),
	}
	got, err := p.CountTokens(context.Background(), tokencount.TokenCountRequest{
		Model:    "claude-haiku-4-5",
		Messages: msgs,
	})
	require.NoError(t, err)

	sumPerMsg := 0
	for _, n := range got.PerMessage {
		sumPerMsg += n
	}
	roleSum := got.UserTokens + got.AssistantTokens + got.ToolResultTokens

	assert.GreaterOrEqual(t, got.SystemTokens+roleSum, sumPerMsg,
		"role breakdown (with injected extras) must be >= sum(PerMessage)")
	assert.Greater(t, got.UserTokens, 0)
	assert.Greater(t, got.AssistantTokens, 0)
}

func TestProvider_CountTokens_Tools(t *testing.T) {
	p := New()
	tools := []tool.Definition{
		{Name: "lookup", Description: "Look something up", Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"q": map[string]any{"type": "string"}},
		}},
	}
	got, err := p.CountTokens(context.Background(), tokencount.TokenCountRequest{
		Model:    "claude-haiku-4-5",
		Messages: llm.Messages{llm.User("hi")},
		Tools:    tools,
	})
	require.NoError(t, err)
	assert.Greater(t, got.ToolsTokens, 0)
	assert.Equal(t, got.ToolsTokens, got.PerTool["lookup"])
	assert.Greater(t, got.OverheadTokens, 0, "Anthropic tool preamble must appear in OverheadTokens")
}
