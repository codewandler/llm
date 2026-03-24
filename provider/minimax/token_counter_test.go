package minimax

import (
	"context"
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvider_CountTokens_PerMessageLen(t *testing.T) {
	t.Parallel()

	p := New()
	msgs := llm.Messages{
		&llm.SystemMsg{Content: "You are helpful."},
		&llm.UserMsg{Content: "What is 2+2?"},
		&llm.AssistantMsg{Content: "It is 4."},
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    ModelM27,
		Messages: msgs,
	})
	require.NoError(t, err)
	assert.Len(t, got.PerMessage, 3, "PerMessage must have one entry per message")
}

func TestProvider_CountTokens_RoleBreakdown(t *testing.T) {
	t.Parallel()

	p := New()
	msgs := llm.Messages{
		&llm.SystemMsg{Content: "You are helpful."},
		&llm.UserMsg{Content: "What is 2+2?"},
		&llm.AssistantMsg{Content: "It is 4."},
		&llm.ToolCallResult{ToolCallID: "c1", Output: "done"},
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    ModelM27,
		Messages: msgs,
	})
	require.NoError(t, err)

	// Role breakdown must sum to sum(PerMessage)
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
	t.Parallel()

	p := New()
	tools := []llm.ToolDefinition{
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
		Model:    ModelM27,
		Messages: llm.Messages{&llm.UserMsg{Content: "hello"}},
		Tools:    tools,
	})
	require.NoError(t, err)

	assert.Greater(t, got.ToolsTokens, 0)
	assert.Len(t, got.PerTool, 2)
	assert.Greater(t, got.PerTool["get_weather"], 0)
	assert.Greater(t, got.PerTool["search"], 0)

	// PerTool values must sum to ToolsTokens
	sum := 0
	for _, n := range got.PerTool {
		sum += n
	}
	assert.Equal(t, got.ToolsTokens, sum)
}

func TestProvider_CountTokens_InputTokensPositive(t *testing.T) {
	t.Parallel()

	p := New()
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    ModelM27,
		Messages: llm.Messages{&llm.UserMsg{Content: "Hello, how are you?"}},
	})
	require.NoError(t, err)
	assert.Greater(t, got.InputTokens, 0)
}

func TestProvider_CountTokens_NoToolsHiddenSystemPrompt(t *testing.T) {
	t.Parallel()

	p := New()

	// Without a system message, MiniMax injects a hidden default system prompt.
	// The overhead must reflect the measured cost of that hidden prompt.
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    ModelM27,
		Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, minimaxHiddenSystemPromptTokens, got.OverheadTokens,
		"user-only request must account for hidden system prompt overhead")

	// With a system message, the hidden prompt is suppressed — no overhead.
	gotWithSystem, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model: ModelM27,
		Messages: llm.Messages{
			&llm.SystemMsg{Content: "You are a helpful assistant."},
			&llm.UserMsg{Content: "hi"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, gotWithSystem.OverheadTokens,
		"request with system message must not add hidden prompt overhead")
}

func TestProvider_CountTokens_AllModels(t *testing.T) {
	t.Parallel()

	p := New()
	msgs := llm.Messages{&llm.UserMsg{Content: "Hello world"}}

	models := []string{ModelM27, ModelM25, ModelM21, ModelM2}
	for _, model := range models {
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

func TestProvider_CountTokens_WithToolCallInAssistantMessage(t *testing.T) {
	t.Parallel()

	p := New()
	msgs := llm.Messages{
		&llm.UserMsg{Content: "What's the weather?"},
		&llm.AssistantMsg{
			Content:   "",
			ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "get_weather", Arguments: map[string]any{"location": "Berlin"}}},
		},
		&llm.ToolCallResult{ToolCallID: "call_1", Output: "Sunny, 20C"},
	}
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    ModelM27,
		Messages: msgs,
	})
	require.NoError(t, err)

	assert.Greater(t, got.AssistantTokens, 0, "assistant tokens should include tool call text")
	assert.Greater(t, got.ToolResultTokens, 0, "tool result tokens should be counted")
}

func TestProvider_CountTokens_EmptyMessages(t *testing.T) {
	t.Parallel()

	p := New()
	got, err := p.CountTokens(context.Background(), llm.TokenCountRequest{
		Model:    ModelM27,
		Messages: llm.Messages{},
	})
	require.NoError(t, err)

	assert.Equal(t, 0, got.SystemTokens)
	assert.Equal(t, 0, got.UserTokens)
	assert.Equal(t, 0, got.AssistantTokens)
	assert.Equal(t, 0, got.ToolResultTokens)
	assert.Equal(t, 0, got.ToolsTokens)
	// Empty messages with no system message still triggers the hidden system prompt
	// overhead — the API would inject its default system prompt even for an empty request.
	assert.Equal(t, minimaxHiddenSystemPromptTokens, got.OverheadTokens)
	assert.Equal(t, minimaxHiddenSystemPromptTokens, got.InputTokens)
}
