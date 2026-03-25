package openai

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
	p2 := p.WithDefaultModel("")
	_, err := p2.CountTokens(context.Background(), llm.TokenCountRequest{
		Messages: llm.Messages{llm.User("hello")},
	})
	require.Error(t, err)
}

func TestProvider_CountTokens_PerMessageLen(t *testing.T) {
	p := New()
	msgs := llm.Messages{
		llm.System("You are helpful."),
		llm.User("What is 2+2?"),
		llm.Assistant("It is 4."),
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "gpt-4o",
		Messages: msgs,
	})
	require.NoError(t, err)
	assert.Len(t, got.PerMessage, 3, "PerMessage must have one entry per message")
}

func TestProvider_CountTokens_RoleBreakdown(t *testing.T) {
	p := New()
	msgs := llm.Messages{
		llm.System("You are helpful."),
		llm.User("What is 2+2?"),
		llm.Assistant("It is 4."),
		llm.ToolResult(tool.NewResult("c1", "done", false)),
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "gpt-4o",
		Messages: msgs,
	})
	require.NoError(t, err)

	sumPerMsg := 0
	for _, n := range got.PerMessage {
		sumPerMsg += n
	}
	roleSum := got.SystemTokens + got.UserTokens + got.AssistantTokens + got.ToolResultTokens
	assert.Equal(t, sumPerMsg, roleSum, "role breakdown must sum to sum(PerMessage)")

	assert.Greater(t, got.SystemTokens, 0)
	assert.Greater(t, got.UserTokens, 0)
	assert.Greater(t, got.AssistantTokens, 0)
	assert.Greater(t, got.ToolResultTokens, 0)
}

func TestProvider_CountTokens_Tools(t *testing.T) {
	p := New()
	tools := []tool.Definition{
		{Name: "get_weather", Description: "Get current weather", Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{"type": "string"},
			},
		}},
		{Name: "search", Description: "Search the web", Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
		}},
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "gpt-4o",
		Messages: llm.Messages{llm.User("hello")},
		Tools:    tools,
	})
	require.NoError(t, err)

	assert.Greater(t, got.ToolsTokens, 0)
	assert.Len(t, got.PerTool, 2)
	assert.Greater(t, got.PerTool["get_weather"], 0)
	assert.Greater(t, got.PerTool["search"], 0)

	sum := 0
	for _, n := range got.PerTool {
		sum += n
	}
	assert.Equal(t, got.ToolsTokens, sum)
}

func TestProvider_CountTokens_InputTokensPositive(t *testing.T) {
	p := New()
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    "gpt-4o-mini",
		Messages: llm.Messages{llm.User("Hello, how are you?")},
	})
	require.NoError(t, err)
	assert.Greater(t, got.InputTokens, 0)
}

func TestProvider_CountTokens_EncodingVariants(t *testing.T) {
	p := New()
	msgs := llm.Messages{llm.User("Hello world")}

	for _, model := range []string{"gpt-4o", "gpt-4", "gpt-3.5-turbo", "o1-mini", "o3"} {
		t.Run(model, func(t *testing.T) {
			got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
				Model:    model,
				Messages: msgs,
			})
			require.NoError(t, err)
			assert.Greater(t, got.InputTokens, 0)
		})
	}
}
