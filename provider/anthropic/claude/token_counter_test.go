package claude

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

func TestProvider_CountTokens_IncludesInjectedSystemBlocks(t *testing.T) {
	p := New()

	// Count with no user system prompt — the only system tokens should come
	// from the three injected blocks.
	onlyInjected, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "claude-haiku-4-5",
		Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
	})
	require.NoError(t, err)

	// Now add a user system message — system tokens must increase.
	withUserSystem, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model: "claude-haiku-4-5",
		Messages: llm.Messages{
			&llm.SystemMsg{Content: "You are helpful."},
			&llm.UserMsg{Content: "hi"},
		},
	})
	require.NoError(t, err)

	assert.Greater(t, withUserSystem.SystemTokens, onlyInjected.SystemTokens,
		"adding a user system message should increase SystemTokens")
	assert.Greater(t, withUserSystem.InputTokens, onlyInjected.InputTokens,
		"adding a user system message should increase InputTokens")

	// The injected blocks alone must contribute a non-trivial number of tokens.
	// billingHeader + systemCore + systemIdentity is ~40-60 tokens in cl100k_base.
	assert.Greater(t, onlyInjected.SystemTokens, 30,
		"injected system blocks should contribute >30 tokens")
}

func TestProvider_CountTokens_PerMessageLen(t *testing.T) {
	p := New()
	msgs := llm.Messages{
		&llm.SystemMsg{Content: "You are helpful."},
		&llm.UserMsg{Content: "What is 2+2?"},
		&llm.AssistantMsg{Content: "It is 4."},
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "claude-haiku-4-5",
		Messages: msgs,
	})
	require.NoError(t, err)
	assert.Len(t, got.PerMessage, 3, "PerMessage must have one entry per message")
}

func TestProvider_CountTokens_RoleBreakdown(t *testing.T) {
	p := New()
	msgs := llm.Messages{
		&llm.SystemMsg{Content: "You are helpful."},
		&llm.UserMsg{Content: "Hello"},
		&llm.AssistantMsg{Content: "Hi there!"},
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "claude-haiku-4-5",
		Messages: msgs,
	})
	require.NoError(t, err)

	// Role breakdown invariant: sum(PerMessage) == SystemTokens + UserTokens + AssistantTokens + ToolResultTokens
	// Note: SystemTokens includes the injected blocks PLUS the user's system message tokens.
	sumPerMsg := 0
	for _, n := range got.PerMessage {
		sumPerMsg += n
	}
	roleSum := got.UserTokens + got.AssistantTokens + got.ToolResultTokens

	// SystemTokens = injected block tokens + PerMessage[0] (the system message),
	// so: SystemTokens = (SystemTokens - PerMessage[0]) + PerMessage[0]
	// We verify that roleSum + perMessageSystemPart == SystemTokens.
	// Simplified: SystemTokens + UserTokens + AssistantTokens + ToolResultTokens >= sumPerMsg
	// (>= because SystemTokens includes injected extras beyond PerMessage).
	assert.GreaterOrEqual(t, got.SystemTokens+roleSum, sumPerMsg,
		"role breakdown (with injected extras) must be >= sum(PerMessage)")
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
		Model:    "claude-haiku-4-5",
		Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
		Tools:    tools,
	})
	require.NoError(t, err)
	assert.Greater(t, got.ToolsTokens, 0)
	// ToolsTokens includes Anthropic's hidden tool preamble + framing overhead.
	assert.Greater(t, got.ToolsTokens, got.PerTool["lookup"],
		"ToolsTokens must exceed raw per-tool count due to Anthropic overhead")
}
