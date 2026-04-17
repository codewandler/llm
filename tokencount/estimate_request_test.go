package tokencount

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

func TestEstimate_AnthropicModel(t *testing.T) {
	t.Parallel()

	result := Estimate(context.Background(), "anthropic", llm.Request{
		Model:    "claude-sonnet-4-5",
		Messages: llm.Messages{llm.User("hello")},
	})
	require.NotNil(t, result)
	assert.Equal(t, "cl100k_base", result.Encoder)
	assert.True(t, result.CostKnown, "Anthropic models should have known costs")
	assert.Greater(t, result.Tokens.Count(usage.KindInput), 0)
}

func TestEstimate_OpenAIModel(t *testing.T) {
	t.Parallel()

	result := Estimate(context.Background(), "openai", llm.Request{
		Model:    "gpt-4o",
		Messages: llm.Messages{llm.User("hello")},
	})
	require.NotNil(t, result)
	assert.True(t, result.CostKnown, "OpenAI models should have known costs")
	assert.Contains(t, result.Encoder, "o200k_base")
}

func TestEstimate_UnknownModel(t *testing.T) {
	t.Parallel()

	result := Estimate(context.Background(), "ollama", llm.Request{
		Model:    "my-custom-model",
		Messages: llm.Messages{llm.User("hello")},
	})
	require.NotNil(t, result)
	assert.False(t, result.CostKnown, "Unknown models should not have known costs")
}

func TestEstimate_EmptyModel(t *testing.T) {
	t.Parallel()

	result := Estimate(context.Background(), "openai", llm.Request{
		Messages: llm.Messages{llm.User("hello")},
	})
	assert.Nil(t, result)
}

func TestEstimate_AnthropicWithTools(t *testing.T) {
	t.Parallel()

	tools := []tool.Definition{
		{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{"type": "string"},
				},
			},
		},
	}

	withoutTools := Estimate(context.Background(), "anthropic", llm.Request{
		Model:    "claude-sonnet-4-5",
		Messages: llm.Messages{llm.User("hello")},
	})
	withTools := Estimate(context.Background(), "anthropic", llm.Request{
		Model:    "claude-sonnet-4-5",
		Messages: llm.Messages{llm.User("hello")},
		Tools:    tools,
	})

	require.NotNil(t, withoutTools)
	require.NotNil(t, withTools)
	assert.Greater(t, withTools.Tokens.Count(usage.KindInput), withoutTools.Tokens.Count(usage.KindInput),
		"Tool definitions should add tokens")
}
