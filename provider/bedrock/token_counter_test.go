package bedrock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/tool"
)

func TestProvider_CountTokens_MissingModel(t *testing.T) {
	p := New()
	_, err := p.CountTokens(context.Background(), tokencount.TokenCountRequest{
		Messages: msg.BuildTranscript(msg.User("hello")),
	})
	require.Error(t, err)
}

func TestProvider_CountTokens_PerMessageLen(t *testing.T) {
	p := New()
	msgs := msg.BuildTranscript(
		msg.System("You are helpful."),
		msg.User("What is 2+2?"),
	)
	got, err := p.CountTokens(context.Background(), tokencount.TokenCountRequest{
		Model:    "anthropic.claude-3-5-sonnet-20241022-v2:0",
		Messages: msgs,
	})
	require.NoError(t, err)
	assert.Len(t, got.PerMessage, 2)
}

func TestProvider_CountTokens_RoleBreakdown(t *testing.T) {
	p := New()
	msgs := msg.BuildTranscript(
		msg.System("Be concise."),
		msg.User("Hello"),
		msg.Assistant(msg.Text("Hi there!")),
		msg.Tool().Results(msg.ToolResult{ToolCallID: "t1", ToolOutput: "result"}),
	)
	got, err := p.CountTokens(context.Background(), tokencount.TokenCountRequest{
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
	got, err := p.CountTokens(context.Background(), tokencount.TokenCountRequest{
		Model:    "anthropic.claude-3-5-sonnet-20241022-v2:0",
		Messages: msg.BuildTranscript(msg.User("fetch this")),
		Tools:    tools,
	})
	require.NoError(t, err)
	assert.Greater(t, got.ToolsTokens, 0)
	assert.Equal(t, got.ToolsTokens, got.PerTool["fetch"])
	assert.Greater(t, got.OverheadTokens, 0, "Anthropic tool preamble must appear in OverheadTokens")
}
