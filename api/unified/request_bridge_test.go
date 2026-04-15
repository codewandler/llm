package unified

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	llmtool "github.com/codewandler/llm/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     Request
		wantErr string
	}{
		{
			name: "missing model",
			req: Request{Messages: []Message{{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}}}},
			wantErr: "model is required",
		},
		{
			name: "missing messages",
			req: Request{Model: "gpt-4o"},
			wantErr: "messages are required",
		},
		{
			name: "ok",
			req: Request{Model: "gpt-4o", Messages: []Message{{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}}}},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestRequestFromLLM_AndBack(t *testing.T) {
	llmReq := llm.Request{
		Model:       "claude-sonnet-4-6",
		MaxTokens:   512,
		Temperature: 0.2,
		TopP:        0.9,
		TopK:        40,
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

func TestRequestToMessages(t *testing.T) {
	uReq := Request{
		Model:        "claude-sonnet-4-6",
		MaxTokens:    256,
		Temperature:  0.1,
		TopP:         0.8,
		TopK:         10,
		OutputFormat: OutputFormatJSON,
		Tools: []Tool{{
			Name:        "search",
			Description: "Search docs",
			Parameters:  map[string]any{"type": "object"},
		}},
		ToolChoice: llm.ToolChoiceTool{Name: "search"},
		Effort:     EffortLow,
		Thinking:   ThinkingOn,
		UserID:     "user-123",
		Messages: []Message{
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "system"}}},
			{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hello"}}},
			{Role: RoleAssistant, Parts: []Part{{Type: PartTypeThinking, Thinking: &ThinkingPart{Text: "think", Signature: "sig"}}, {Type: PartTypeText, Text: "answer"}, {Type: PartTypeToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "search", Args: map[string]any{"q": "x"}}}}},
			{Role: RoleTool, Parts: []Part{{Type: PartTypeToolResult, ToolResult: &ToolResult{ToolCallID: "call-1", ToolOutput: "result"}}}},
		},
	}

	wire, err := RequestToMessages(uReq)
	require.NoError(t, err)

	assert.Equal(t, uReq.Model, wire.Model)
	assert.True(t, wire.Stream)
	assert.Equal(t, 256, wire.MaxTokens)
	require.Len(t, wire.System, 1)
	require.Len(t, wire.Messages, 3)
	require.NotNil(t, wire.Thinking)
	assert.Equal(t, "enabled", wire.Thinking.Type)
	assert.Equal(t, 1024, wire.Thinking.BudgetTokens)
	require.NotNil(t, wire.Metadata)
	assert.Equal(t, "user-123", wire.Metadata.UserID)
	require.NotNil(t, wire.OutputConfig)
	require.NotNil(t, wire.OutputConfig.Format)
	assert.Equal(t, "json_schema", wire.OutputConfig.Format.Type)
	require.Len(t, wire.Tools, 1)
	require.NotNil(t, wire.ToolChoice)
}

func TestRequestToCompletions(t *testing.T) {
	uReq := Request{
		Model:      "gpt-4o",
		MaxTokens:  128,
		Effort:     EffortMedium,
		CacheHint:  &msg.CacheHint{Enabled: true, TTL: "1h"},
		ToolChoice: llm.ToolChoiceRequired{},
		Tools: []Tool{{Name: "search", Description: "Search", Parameters: map[string]any{"type": "object"}}},
		Messages: []Message{
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "sys"}}},
			{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}},
			{Role: RoleAssistant, Parts: []Part{{Type: PartTypeText, Text: "working"}, {Type: PartTypeToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "search", Args: map[string]any{"q": "golang"}}}}},
			{Role: RoleTool, Parts: []Part{{Type: PartTypeToolResult, ToolResult: &ToolResult{ToolCallID: "call-1", ToolOutput: "done"}}}},
		},
	}

	wire, err := RequestToCompletions(uReq)
	require.NoError(t, err)

	assert.Equal(t, "gpt-4o", wire.Model)
	assert.True(t, wire.Stream)
	require.NotNil(t, wire.StreamOptions)
	assert.True(t, wire.StreamOptions.IncludeUsage)
	assert.Equal(t, "required", wire.ToolChoice)
	assert.Equal(t, "24h", wire.PromptCacheRetention)
	require.Len(t, wire.Messages, 4)
}

func TestRequestToResponses(t *testing.T) {
	uReq := Request{
		Model:      "gpt-5.4",
		MaxTokens:  256,
		Effort:     EffortHigh,
		ToolChoice: llm.ToolChoiceTool{Name: "search"},
		Tools:      []Tool{{Name: "search", Description: "Search", Parameters: map[string]any{"type": "object"}}},
		Messages: []Message{
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "primary system"}}},
			{Role: RoleSystem, Parts: []Part{{Type: PartTypeText, Text: "secondary system"}}},
			{Role: RoleUser, Parts: []Part{{Type: PartTypeText, Text: "hi"}}},
			{Role: RoleAssistant, Parts: []Part{{Type: PartTypeText, Text: "ok"}, {Type: PartTypeToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "search", Args: map[string]any{"q": "golang"}}}}},
			{Role: RoleTool, Parts: []Part{{Type: PartTypeToolResult, ToolResult: &ToolResult{ToolCallID: "call-1", ToolOutput: "result"}}}},
		},
	}

	wire, err := RequestToResponses(uReq)
	require.NoError(t, err)

	assert.Equal(t, "gpt-5.4", wire.Model)
	assert.True(t, wire.Stream)
	assert.Equal(t, "primary system", wire.Instructions)
	require.NotEmpty(t, wire.Input)
	assert.Equal(t, "developer", wire.Input[0].Role)
	assert.Equal(t, "secondary system", wire.Input[0].Content)
	require.NotNil(t, wire.Reasoning)
	assert.Equal(t, "high", wire.Reasoning.Effort)
	require.Len(t, wire.Tools, 1)
	require.NotNil(t, wire.ToolChoice)
}
