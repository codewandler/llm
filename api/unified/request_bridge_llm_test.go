package unified

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	llmtool "github.com/codewandler/llm/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestFromLLM_AndBack(t *testing.T) {
	llmReq := llm.Request{
		Model:        "claude-sonnet-4-6",
		MaxTokens:    512,
		Temperature:  0.2,
		TopP:         0.9,
		TopK:         40,
		OutputFormat: llm.OutputFormatJSON,
		Tools: []llmtool.Definition{{
			Name:        "search",
			Description: "Search docs",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
			},
		}},
		ToolChoice: llm.ToolChoiceTool{Name: "search"},
		Effort:     llm.EffortHigh,
		Thinking:   llm.ThinkingOn,
		CacheHint:  msg.NewCacheHint(),
		Messages: llm.Messages{
			msg.System("You are a helpful assistant").Build(),
			msg.User("What is Go?").Build(),
			msg.Assistant(
				msg.Thinking("reasoning", "sig-1"),
				msg.Text("Calling tool..."),
				msg.NewToolCall("call-1", "search", msg.ToolArgs{"query": "golang"}),
			).Build(),
			msg.Tool().Results(msg.ToolResult{ToolCallID: "call-1", ToolOutput: "Go is a language."}).Build(),
		},
	}

	uReq, err := RequestFromLLM(llmReq)
	require.NoError(t, err)

	require.Equal(t, llmReq.Model, uReq.Model)
	require.Len(t, uReq.Messages, 4)
	require.Len(t, uReq.Tools, 1)
	require.NotNil(t, uReq.ToolChoice)
	assert.Equal(t, EffortHigh, uReq.Effort)
	assert.Equal(t, ThinkingOn, uReq.Thinking)
	assert.Equal(t, OutputFormatJSON, uReq.OutputFormat)

	back, err := RequestToLLM(uReq)
	require.NoError(t, err)

	assert.Equal(t, llmReq.Model, back.Model)
	assert.Equal(t, llmReq.MaxTokens, back.MaxTokens)
	assert.Equal(t, llmReq.OutputFormat, back.OutputFormat)
	require.Len(t, back.Messages, 4)
	require.Len(t, back.Tools, 1)
}
