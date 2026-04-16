package openai

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

// testMeta returns a ccStreamMeta for testing.
func testMeta(model string) ccStreamMeta {
	return ccStreamMeta{
		requestedModel: model,
		startTime:      time.Now(),
	}
}

// testRespMeta returns a RespStreamMeta for testing.
func testRespMeta(model string) RespStreamMeta {
	return RespStreamMeta{
		RequestedModel: model,
		StartTime:      time.Now(),
	}
}

// --- Unit tests for ccBuildRequest ---

func TestBuildRequest_Basic(t *testing.T) {
	opts := llm.Request{
		Model: "gpt-4o",
		Messages: llm.Messages{
			llm.User("Hello"),
		},
	}

	body, err := ccBuildRequest(opts)
	require.NoError(t, err)

	var req ccRequest
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	assert.Equal(t, "gpt-4o", req.Model)
	assert.True(t, req.Stream)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, "user", req.Messages[0].Role)
	assert.Equal(t, "Hello", req.Messages[0].Content)

	require.NotNil(t, req.StreamOptions)
	assert.True(t, req.StreamOptions.IncludeUsage)
}

func TestBuildRequest_GenerationParameters(t *testing.T) {
	tests := []struct {
		name    string
		opts    llm.Request
		checkFn func(*ccRequest)
	}{
		{
			name: "MaxTokens set when positive",
			opts: llm.Request{
				Model: "gpt-4o",
				Messages: llm.Messages{
					llm.User("Hello"),
				},
				MaxTokens: 1000,
			},
			checkFn: func(r *ccRequest) {
				assert.Equal(t, 1000, r.MaxTokens)
			},
		},
		{
			name: "Temperature set when positive",
			opts: llm.Request{
				Model: "gpt-4o",
				Messages: llm.Messages{
					llm.User("Hello"),
				},
				Temperature: 0.7,
			},
			checkFn: func(r *ccRequest) {
				assert.Equal(t, 0.7, r.Temperature)
			},
		},
		{
			name: "TopP set when positive",
			opts: llm.Request{
				Model: "gpt-4o",
				Messages: llm.Messages{
					llm.User("Hello"),
				},
				TopP: 0.9,
			},
			checkFn: func(r *ccRequest) {
				assert.Equal(t, 0.9, r.TopP)
			},
		},
		{
			name: "TopK set when positive",
			opts: llm.Request{
				Model: "gpt-4o",
				Messages: llm.Messages{
					llm.User("Hello"),
				},
				TopK: 40,
			},
			checkFn: func(r *ccRequest) {
				assert.Equal(t, 40, r.TopK)
			},
		},
		{
			name: "all parameters set together",
			opts: llm.Request{
				Model: "gpt-4o",
				Messages: llm.Messages{
					llm.User("Hello"),
				},
				MaxTokens:   2000,
				Temperature: 0.5,
				TopP:        0.8,
				TopK:        50,
			},
			checkFn: func(r *ccRequest) {
				assert.Equal(t, 2000, r.MaxTokens)
				assert.Equal(t, 0.5, r.Temperature)
				assert.Equal(t, 0.8, r.TopP)
				assert.Equal(t, 50, r.TopK)
			},
		},
		{
			name: "zero values omitted",
			opts: llm.Request{
				Model: "gpt-4o",
				Messages: llm.Messages{
					llm.User("Hello"),
				},
				MaxTokens:   0,
				Temperature: 0,
				TopP:        0,
				TopK:        0,
			},
			checkFn: func(r *ccRequest) {
				// Unmarshal to map to check field presence
				var reqMap map[string]any
				body, _ := json.Marshal(r)
				_ = json.Unmarshal(body, &reqMap)
				_, hasMaxTokens := reqMap["max_tokens"]
				_, hasTemperature := reqMap["temperature"]
				_, hasTopP := reqMap["top_p"]
				_, hasTopK := reqMap["top_k"]
				assert.False(t, hasMaxTokens, "MaxTokens should be omitted when 0")
				assert.False(t, hasTemperature, "Temperature should be omitted when 0")
				assert.False(t, hasTopP, "TopP should be omitted when 0")
				assert.False(t, hasTopK, "TopK should be omitted when 0")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := ccBuildRequest(tt.opts)
			require.NoError(t, err)

			var req ccRequest
			err = json.Unmarshal(body, &req)
			require.NoError(t, err)

			tt.checkFn(&req)
		})
	}
}

func TestBuildRequest_OutputFormat(t *testing.T) {
	tests := []struct {
		name    string
		opts    llm.Request
		checkFn func(*testing.T, map[string]any)
	}{
		{
			name: "JSON format",
			opts: llm.Request{
				Model: "gpt-4o",
				Messages: llm.Messages{
					llm.User("Return JSON"),
				},
				OutputFormat: llm.OutputFormatJSON,
			},
			checkFn: func(t *testing.T, req map[string]any) {
				respFormat, ok := req["response_format"].(map[string]any)
				require.True(t, ok, "response_format should be present")
				assert.Equal(t, "json_object", respFormat["type"])
			},
		},
		{
			name: "text format",
			opts: llm.Request{
				Model: "gpt-4o",
				Messages: llm.Messages{
					llm.User("Return text"),
				},
				OutputFormat: llm.OutputFormatText,
			},
			checkFn: func(t *testing.T, req map[string]any) {
				// Text format should not include response_format (it's the default)
				_, hasFormat := req["response_format"]
				assert.False(t, hasFormat, "response_format should be omitted for text format")
			},
		},
		{
			name: "no format specified",
			opts: llm.Request{
				Model: "gpt-4o",
				Messages: llm.Messages{
					llm.User("Hello"),
				},
			},
			checkFn: func(t *testing.T, req map[string]any) {
				_, hasFormat := req["response_format"]
				assert.False(t, hasFormat, "response_format should be omitted when not specified")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := ccBuildRequest(tt.opts)
			require.NoError(t, err)

			var req map[string]any
			require.NoError(t, json.Unmarshal(body, &req))

			tt.checkFn(t, req)
		})
	}
}

func TestBuildRequest_WithTools(t *testing.T) {
	type GetWeatherParams struct {
		Location string `json:"location" jsonschema:"description=City name,required"`
	}

	opts := llm.Request{
		Model: "gpt-4o",
		Messages: llm.Messages{
			llm.User("test"),
		},
		Tools: []tool.Definition{
			tool.DefinitionFor[GetWeatherParams]("get_weather", "Get weather for a location"),
		},
	}

	body, err := ccBuildRequest(opts)
	require.NoError(t, err)

	var req ccRequest
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	require.Len(t, req.Tools, 1)
	tool := req.Tools[0]
	assert.Equal(t, "function", tool.Type)
	assert.Equal(t, "get_weather", tool.Function.Name)
	assert.Equal(t, "Get weather for a location", tool.Function.Description)
}

func TestBuildRequest_AssistantWithToolCalls(t *testing.T) {
	opts := llm.Request{
		Model: "gpt-4o",
		Messages: msg.BuildTranscript(
			msg.User("What's the weather?"),
			msg.Assistant(msg.ToolCall(msg.NewToolCall("call_123", "get_weather", msg.ToolArgs{"location": "Paris"}))),
		),
	}

	body, err := ccBuildRequest(opts)
	require.NoError(t, err)

	var req ccRequest
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	require.Len(t, req.Messages, 2)
	assistantMsg := req.Messages[1]
	assert.Equal(t, "assistant", assistantMsg.Role)
	require.Len(t, assistantMsg.ToolCalls, 1)

	toolCall := assistantMsg.ToolCalls[0]
	assert.Equal(t, "call_123", toolCall.ID)
	assert.Equal(t, "function", toolCall.Type)
	assert.Equal(t, "get_weather", toolCall.Function.Name)
}

func TestBuildRequest_ToolResults(t *testing.T) {
	opts := llm.Request{
		Model: "gpt-4o",
		Messages: msg.BuildTranscript(
			msg.User("What's the weather?"),
			msg.Assistant(msg.ToolCall(msg.NewToolCall("call_123", "get_weather", msg.ToolArgs{"location": "Paris"}))),
			msg.Tool().Results(msg.ToolResult{ToolCallID: "call_123", ToolOutput: `{"temp": 72, "conditions": "sunny"}`}),
		),
	}

	body, err := ccBuildRequest(opts)
	require.NoError(t, err)

	var req ccRequest
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	require.Len(t, req.Messages, 3)
	toolMsg := req.Messages[2]
	assert.Equal(t, "tool", toolMsg.Role)
	assert.Equal(t, `{"temp": 72, "conditions": "sunny"}`, toolMsg.Content)
	assert.Equal(t, "call_123", toolMsg.ToolCallID)
}

// --- Unit tests for ccParseStream ---

func TestParseStream_TextDeltas(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"Hello"}}]}
data: {"choices":[{"delta":{"content":" world"}}]}
data: {"choices":[{"finish_reason":"stop"}]}
data: [DONE]
`
	pub, ch := llm.NewEventPublisher()
	go ccParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-4o"))

	var deltas []string
	var gotDone bool
	for event := range ch {
		switch event.Type {
		case llm.StreamEventDelta:
			if de, ok := event.Data.(*llm.DeltaEvent); ok {
				deltas = append(deltas, de.Text)
			}
		case llm.StreamEventCompleted:
			gotDone = true
		case llm.StreamEventError:
			if err, ok := event.Data.(*llm.ErrorEvent); ok {
				t.Fatalf("Unexpected error: %v", err.Error)
			}
		}
	}

	assert.Equal(t, []string{"Hello", " world"}, deltas)
	assert.True(t, gotDone)
}

func TestParseStream_ToolCallAccumulation(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_123","type":"function","function":{"name":"get_weather"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location\""}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"Paris\"}"}}]}}]}
data: {"choices":[{"finish_reason":"tool_calls"}]}
data: [DONE]
`
	pub, ch := llm.NewEventPublisher()
	go ccParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-4o"))

	var toolCalls []*llm.DeltaEvent
	for event := range ch {
		if event.Type == llm.StreamEventDelta {
			if de, ok := event.Data.(*llm.DeltaEvent); ok && de.Kind == llm.DeltaKindTool {
				toolCalls = append(toolCalls, de)
			}
		}
	}

	require.Len(t, toolCalls, 2)
}

func TestParseStream_ParallelToolCallsOrder(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"tool_alpha"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_1","type":"function","function":{"name":"tool_beta"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":2,"id":"call_2","type":"function","function":{"name":"tool_gamma"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"x\":1}"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":2,"function":{"arguments":"{\"z\":3}"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"y\":2}"}}]}}]}
data: {"choices":[{"finish_reason":"tool_calls"}]}
data: [DONE]
`
	pub, ch := llm.NewEventPublisher()
	go ccParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-4o"))

	var toolCalls []tool.Call
	for event := range ch {
		if event.Type == llm.StreamEventToolCall {
			if tc, ok := event.Data.(*llm.ToolCallEvent); ok {
				toolCalls = append(toolCalls, tc.ToolCall)
			}
		}
	}

	require.Len(t, toolCalls, 3)
	assert.Equal(t, "call_0", toolCalls[0].ToolCallID())
	assert.Equal(t, "tool_alpha", toolCalls[0].ToolName())
	assert.Equal(t, float64(1), toolCalls[0].ToolArgs()["x"])

	assert.Equal(t, "call_1", toolCalls[1].ToolCallID())
	assert.Equal(t, "tool_beta", toolCalls[1].ToolName())
	assert.Equal(t, float64(2), toolCalls[1].ToolArgs()["y"])

	assert.Equal(t, "call_2", toolCalls[2].ToolCallID())
	assert.Equal(t, "tool_gamma", toolCalls[2].ToolName())
	assert.Equal(t, float64(3), toolCalls[2].ToolArgs()["z"])
}

func TestParseStream_Usage(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"test"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}
data: {"choices":[{"finish_reason":"stop"}]}
data: [DONE]
`
	pub, ch := llm.NewEventPublisher()
	go ccParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-4o"))

	var usageRec *usage.Record
	for event := range ch {
		if event.Type == llm.StreamEventCompleted {
			if ce, ok := event.Data.(*llm.CompletedEvent); ok {
				_ = ce.StopReason
			}
		}
		if event.Type == llm.StreamEventUsageUpdated {
			if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
				usageRec = &ue.Record
			}
		}
	}

	require.NotNil(t, usageRec)
	assert.Equal(t, 10, usageRec.Tokens.Count(usage.KindInput))
	assert.Equal(t, 5, usageRec.Tokens.Count(usage.KindOutput))
}

func TestParseStream_UsageWithDetails(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"test"}}]}
data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150,"prompt_tokens_details":{"cached_tokens":80},"completion_tokens_details":{"reasoning_tokens":30}}}
data: [DONE]
`
	pub, ch := llm.NewEventPublisher()
	go ccParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-5"))

	var usageRec *usage.Record
	for event := range ch {
		if event.Type == llm.StreamEventUsageUpdated {
			if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
				usageRec = &ue.Record
			}
		}
	}

	require.NotNil(t, usageRec)
	// 100 prompt - 80 cached = 20 regular input; 50 - 30 reasoning = 20 output
	assert.Equal(t, 20, usageRec.Tokens.Count(usage.KindInput))
	assert.Equal(t, 20, usageRec.Tokens.Count(usage.KindOutput))
	assert.Equal(t, 80, usageRec.Tokens.Count(usage.KindCacheRead))
	assert.Equal(t, 30, usageRec.Tokens.Count(usage.KindReasoning))
}

// --- Unit tests for calculateCost ---

// TestCalculateCost tests cost calculation via usage.Static() — the centralised
// pricing table that replaced the per-provider calculateCost function.
func TestCalculateCost(t *testing.T) {
	calc := usage.Static()
	tests := []struct {
		name     string
		model    string
		tokens   usage.TokenItems
		wantCost float64
	}{
		{
			name:   "gpt-4o basic",
			model:  "gpt-4o",
			tokens: usage.TokenItems{{Kind: usage.KindInput, Count: 1_000_000}, {Kind: usage.KindOutput, Count: 1_000_000}},
			// $2.50/1M input + $10.00/1M output = $12.50
			wantCost: 12.50,
		},
		{
			name:   "gpt-4o with cache",
			model:  "gpt-4o",
			tokens: usage.TokenItems{{Kind: usage.KindInput, Count: 200_000}, {Kind: usage.KindCacheRead, Count: 800_000}, {Kind: usage.KindOutput, Count: 500_000}},
			// (200k * $2.50) + (800k * $1.25) + (500k * $10.00) / 1M = $0.50 + $1.00 + $5.00 = $6.50
			wantCost: 6.50,
		},
		{
			name:     "gpt-4o-mini cheap",
			model:    "gpt-4o-mini",
			tokens:   usage.TokenItems{{Kind: usage.KindInput, Count: 1_000_000}, {Kind: usage.KindOutput, Count: 1_000_000}},
			wantCost: 0.75,
		},
		{
			name:     "o1-pro expensive",
			model:    "o1-pro",
			tokens:   usage.TokenItems{{Kind: usage.KindInput, Count: 1_000_000}, {Kind: usage.KindOutput, Count: 1_000_000}},
			wantCost: 750.0,
		},
		{
			name:     "unknown model returns zero",
			model:    "unknown-model",
			tokens:   usage.TokenItems{{Kind: usage.KindInput, Count: 1000}, {Kind: usage.KindOutput, Count: 1000}},
			wantCost: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost, ok := calc.Calculate("openai", tt.model, tt.tokens)
			if tt.wantCost == 0 {
				assert.False(t, ok)
				return
			}
			require.True(t, ok)
			assert.InDelta(t, tt.wantCost, cost.Total, 0.001)
		})
	}
}

// --- Unit tests for model registry ---

func TestGetModelInfo(t *testing.T) {
	for _, id := range modelOrder {
		t.Run(id, func(t *testing.T) {
			info, err := getModelInfo(id)
			require.NoError(t, err, "model %s should be in registry", id)
			assert.Equal(t, id, info.ID)
			assert.NotEmpty(t, info.Name)
		})
	}

	t.Run("unknown_model", func(t *testing.T) {
		_, err := getModelInfo("unknown-model-xyz")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnknownModel)
	})
}

func TestGetModelInfo_Categories(t *testing.T) {
	tests := []struct {
		model    string
		category modelCategory
	}{
		{"gpt-4o", categoryNonReasoning},
		{"gpt-4o-mini", categoryNonReasoning},
		{"gpt-4", categoryNonReasoning},
		{"gpt-4-turbo", categoryNonReasoning},
		{"gpt-3.5-turbo", categoryNonReasoning},
		{"gpt-4.1", categoryNonReasoning},
		{"gpt-4.1-mini", categoryNonReasoning},
		{"gpt-4.1-nano", categoryNonReasoning},

		{"gpt-5", categoryPreGPT51},
		{"gpt-5-mini", categoryPreGPT51},
		{"gpt-5-nano", categoryPreGPT51},
		{"gpt-5.2", categoryPreGPT51},
		{"o1", categoryPreGPT51},
		{"o1-mini", categoryPreGPT51},
		{"o3", categoryPreGPT51},
		{"o3-mini", categoryPreGPT51},
		{"o4-mini", categoryPreGPT51},

		{"gpt-5.1", categoryGPT51},

		{"gpt-5-pro", categoryPro},
		{"gpt-5.2-pro", categoryPro},
		{"o1-pro", categoryPro},
		{"o3-pro", categoryPro},

		{"gpt-5-codex", categoryCodex},
		{"gpt-5.1-codex", categoryCodex},
		{"gpt-5.2-codex", categoryCodex},
		{"gpt-5.1-codex-max", categoryCodex},
		{"gpt-5.1-codex-mini", categoryCodex},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			info, err := getModelInfo(tt.model)
			require.NoError(t, err)
			assert.Equal(t, tt.category, info.Category, "model %s should be category %d", tt.model, tt.category)
		})
	}
}

// --- Unit tests for mapEffortAndThinking ---

func TestMapEffortAndThinking(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		effort   llm.Effort
		thinking llm.ThinkingMode
		want     string
	}{
		// Non-reasoning models: always empty
		{"non-reasoning/high/auto", "gpt-4o", llm.EffortHigh, llm.ThinkingAuto, ""},
		{"non-reasoning/high/off", "gpt-4o", llm.EffortHigh, llm.ThinkingOff, ""},
		{"non-reasoning/unset/on", "gpt-4o", "", llm.ThinkingOn, ""},

		// Pre-GPT-5.1 reasoning models
		{"pre51/low/auto", "gpt-5", llm.EffortLow, llm.ThinkingAuto, "low"},
		{"pre51/high/auto", "o3", llm.EffortHigh, llm.ThinkingAuto, "high"},
		{"pre51/medium/auto", "gpt-5-mini", llm.EffortMedium, llm.ThinkingAuto, "medium"},
		{"pre51/unset/auto", "gpt-5", "", llm.ThinkingAuto, ""},
		{"pre51/any/off", "gpt-5", llm.EffortHigh, llm.ThinkingOff, ""},
		{"pre51/max/auto", "gpt-5", llm.EffortMax, llm.ThinkingAuto, "high"},
		{"pre51/unset/on", "gpt-5", "", llm.ThinkingOn, "high"},

		// GPT-5.1
		{"gpt51/low/auto", "gpt-5.1", llm.EffortLow, llm.ThinkingAuto, "low"},
		{"gpt51/high/auto", "gpt-5.1", llm.EffortHigh, llm.ThinkingAuto, "high"},
		{"gpt51/any/off", "gpt-5.1", llm.EffortHigh, llm.ThinkingOff, "none"},
		{"gpt51/unset/on", "gpt-5.1", "", llm.ThinkingOn, "high"},
		{"gpt51/unset/auto", "gpt-5.1", "", llm.ThinkingAuto, ""},
		{"gpt51/max/auto", "gpt-5.1", llm.EffortMax, llm.ThinkingAuto, "high"},

		// Codex
		{"codex/max/auto", "gpt-5.1-codex", llm.EffortMax, llm.ThinkingAuto, "xhigh"},
		{"codex/high/auto", "gpt-5.1-codex", llm.EffortHigh, llm.ThinkingAuto, "high"},
		{"codex/low/auto", "gpt-5.1-codex", llm.EffortLow, llm.ThinkingAuto, "low"},
		{"codex/any/off", "gpt-5.1-codex", llm.EffortHigh, llm.ThinkingOff, ""},
		{"codex-mini/any/off", "gpt-5.1-codex-mini", llm.EffortHigh, llm.ThinkingOff, ""},
		{"codex/unset/on", "gpt-5.1-codex", "", llm.ThinkingOn, "high"},
		{"codex/unset/auto", "gpt-5.1-codex", "", llm.ThinkingAuto, ""},

		// Pro — accepts any effort level (no longer restricted to high-only)
		{"pro/unset/auto", "gpt-5-pro", "", llm.ThinkingAuto, ""},
		{"pro/low/auto", "gpt-5-pro", llm.EffortLow, llm.ThinkingAuto, "low"},
		{"pro/medium/auto", "gpt-5-pro", llm.EffortMedium, llm.ThinkingAuto, "medium"},
		{"pro/high/auto", "gpt-5-pro", llm.EffortHigh, llm.ThinkingAuto, "high"},
		{"pro/any/off", "gpt-5-pro", llm.EffortHigh, llm.ThinkingOff, ""},
		{"pro/unset/on", "gpt-5-pro", "", llm.ThinkingOn, "high"},
		{"pro/max/auto", "o3-pro", llm.EffortMax, llm.ThinkingAuto, "high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mapEffortAndThinking(tt.model, tt.effort, tt.thinking)
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestMapEffortAndThinking_UnknownModel(t *testing.T) {
	result, err := mapEffortAndThinking("unknown-model", llm.EffortHigh, llm.ThinkingAuto)
	require.NoError(t, err)
	assert.Equal(t, "", result)
}

// --- enrichOpts + ccBuildRequest integration tests ---
// These test the full pipeline: enrichOpts (reasoning mapping, cache retention)
// feeding into ccBuildRequest.

func TestBuildRequest_EffortOmitted(t *testing.T) {
	opts := llm.Request{
		Model:    "gpt-4o",
		Messages: llm.Messages{llm.User("Hello")},
	}

	body, err := ccBuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))
	_, exists := req["reasoning_effort"]
	assert.False(t, exists, "reasoning_effort should be omitted when not specified")
}

func TestBuildRequest_EffortSet(t *testing.T) {
	// enrichOpts passes the raw effort string through for pre-GPT51 models.
	opts, err := enrichOpts(llm.Request{
		Model:    "gpt-5",
		Messages: llm.Messages{llm.User("Hello")},
		Effort:   llm.EffortLow,
	})
	require.NoError(t, err)

	body, err := ccBuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))
	assert.Equal(t, "low", req["reasoning_effort"])
}

func TestBuildRequest_EffortMapped(t *testing.T) {
	// enrichOpts maps max → high for gpt-5.1.
	opts, err := enrichOpts(llm.Request{
		Model:    "gpt-5.1",
		Messages: llm.Messages{llm.User("Hello")},
		Effort:   llm.EffortMax,
	})
	require.NoError(t, err)

	body, err := ccBuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))
	assert.Equal(t, "high", req["reasoning_effort"], "max should be mapped to high for gpt-5.1")
}

func TestBuildRequest_ThinkingOff(t *testing.T) {
	opts, err := enrichOpts(llm.Request{
		Model:    "gpt-5.1",
		Messages: llm.Messages{llm.User("Hello")},
		Thinking: llm.ThinkingOff,
	})
	require.NoError(t, err)

	body, err := ccBuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))
	assert.Equal(t, "none", req["reasoning_effort"], "thinking off should map to none for gpt-5.1")
}

func TestBuildRequest_PromptCacheRetention_ExtendedSupported(t *testing.T) {
	models := []string{
		"gpt-5", "gpt-5-mini", "gpt-5-nano", "gpt-5.1", "gpt-5.2",
		"gpt-5-codex", "gpt-5.1-codex", "gpt-5.1-codex-max", "gpt-5.1-codex-mini", "gpt-5.2-codex",
		"gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano",
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			opts := llm.Request{
				Model:    model,
				Messages: llm.Messages{llm.User("Hello")},
			}

			body, err := ccBuildRequest(opts)
			require.NoError(t, err)

			var req map[string]any
			require.NoError(t, json.Unmarshal(body, &req))
			assert.Equal(t, "24h", req["prompt_cache_retention"], "model %s should have extended cache enabled", model)
		})
	}
}

func TestBuildRequest_PromptCacheRetention_NotSupported(t *testing.T) {
	models := []string{
		"gpt-4o", "gpt-4o-mini",
		"gpt-4", "gpt-4-turbo",
		"gpt-3.5-turbo",
		"o1", "o1-mini", "o1-pro",
		"o3", "o3-mini", "o3-pro",
		"o4-mini",
		"gpt-5-pro", "gpt-5.2-pro",
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			opts := llm.Request{
				Model:    model,
				Messages: llm.Messages{llm.User("Hello")},
			}

			body, err := ccBuildRequest(opts)
			require.NoError(t, err)

			var req map[string]any
			require.NoError(t, json.Unmarshal(body, &req))
			_, exists := req["prompt_cache_retention"]
			assert.False(t, exists, "model %s should not have prompt_cache_retention set", model)
		})
	}
}

// --- Unit tests for model registry ---

func TestEnrichOpts_EffortOmitted(t *testing.T) {
	opts := llm.Request{Model: "gpt-4o", Messages: llm.Messages{llm.User("hi")}}
	out, err := enrichOpts(opts)
	require.NoError(t, err)
	assert.Empty(t, out.Effort)
}

func TestEnrichOpts_EffortMappedMaxToHigh(t *testing.T) {
	opts := llm.Request{
		Model:    "gpt-5.1",
		Effort:   llm.EffortMax,
		Messages: llm.Messages{llm.User("hi")},
	}
	out, err := enrichOpts(opts)
	require.NoError(t, err)
	assert.Equal(t, llm.Effort("high"), out.Effort)
}

func TestEnrichOpts_ThinkingOffOmitsOnCodex(t *testing.T) {
	opts := llm.Request{
		Model:    "gpt-5.1-codex",
		Thinking: llm.ThinkingOff,
		Messages: llm.Messages{llm.User("hi")},
	}
	out, err := enrichOpts(opts)
	require.NoError(t, err)
	assert.Equal(t, llm.EffortUnspecified, out.Effort, "codex ThinkingOff should omit effort")
}

func TestWantsExtendedCache_Set(t *testing.T) {
	extended := []string{
		"gpt-5", "gpt-5-mini", "gpt-5-nano",
		"gpt-5.1", "gpt-5.2",
		"gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano",
		"gpt-5-codex", "gpt-5.1-codex", "gpt-5.1-codex-max", "gpt-5.1-codex-mini", "gpt-5.2-codex",
	}
	for _, model := range extended {
		t.Run(model, func(t *testing.T) {
			opts := llm.Request{Model: model, Messages: llm.Messages{llm.User("hi")}}
			assert.True(t, wantsExtendedCache(opts), "model %s should want extended cache", model)
		})
	}
}

func TestWantsExtendedCache_NotSet(t *testing.T) {
	notExtended := []string{"gpt-4o", "gpt-4o-mini", "o1", "o1-pro", "o3", "o3-mini", "o4-mini"}
	for _, model := range notExtended {
		t.Run(model, func(t *testing.T) {
			opts := llm.Request{Model: model, Messages: llm.Messages{llm.User("hi")}}
			assert.False(t, wantsExtendedCache(opts), "model %s should not want extended cache", model)
		})
	}
}

func TestMapEffortAndThinking_Codex(t *testing.T) {
	tests := []struct {
		effort   llm.Effort
		thinking llm.ThinkingMode
		want     string
	}{
		{"", llm.ThinkingAuto, ""},
		{llm.EffortLow, llm.ThinkingAuto, "low"},
		{llm.EffortMedium, llm.ThinkingAuto, "medium"},
		{llm.EffortHigh, llm.ThinkingAuto, "high"},
		{llm.EffortMax, llm.ThinkingAuto, "xhigh"},
		{llm.EffortHigh, llm.ThinkingOff, ""}, // codex: can't reliably disable reasoning
		{"", llm.ThinkingOn, "high"},
	}

	for _, model := range []string{"gpt-5.1-codex", "gpt-5.2-codex", "gpt-5.1-codex-max"} {
		for _, tt := range tests {
			t.Run(model+"_"+string(tt.effort)+"_"+string(tt.thinking), func(t *testing.T) {
				result, err := mapEffortAndThinking(model, tt.effort, tt.thinking)
				require.NoError(t, err)
				assert.Equal(t, tt.want, result)
			})
		}
	}
}

// --- Unit tests for cost calculation in streams ---

func TestParseStream_CostCalculation(t *testing.T) {
	// gpt-4o: $2.50/1M input, $10.00/1M output
	sseData := `data: {"id":"chatcmpl-123","model":"gpt-4o","choices":[{"delta":{"content":"Hi"}}]}
data: {"id":"chatcmpl-123","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}
data: [DONE]
`
	pub, ch := llm.NewEventPublisher()
	go ccParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-4o"))

	var usageRec *usage.Record
	for event := range ch {
		if event.Type == llm.StreamEventUsageUpdated {
			if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
				usageRec = &ue.Record
			}
		}
	}

	require.NotNil(t, usageRec)
	assert.Equal(t, 100, usageRec.Tokens.Count(usage.KindInput))
	assert.Equal(t, 50, usageRec.Tokens.Count(usage.KindOutput))

	// Expected cost: (100/1M * $2.50) + (50/1M * $10.00) = $0.00025 + $0.0005 = $0.00075
	expectedCost := 0.00075
	assert.InDelta(t, expectedCost, usageRec.Cost.Total, 0.0000001)
}

func TestParseStream_CostCalculation_WithCache(t *testing.T) {
	// gpt-4o: $2.50/1M input, $1.25/1M cached, $10.00/1M output
	sseData := `data: {"id":"chatcmpl-123","model":"gpt-4o","choices":[{"delta":{"content":"Hi"}}]}
data: {"id":"chatcmpl-123","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500,"prompt_tokens_details":{"cached_tokens":800}}}
data: [DONE]
`
	pub, ch := llm.NewEventPublisher()
	go ccParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-4o"))

	var usageRec *usage.Record
	for event := range ch {
		if event.Type == llm.StreamEventUsageUpdated {
			if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
				usageRec = &ue.Record
			}
		}
	}

	require.NotNil(t, usageRec)
	// 1000 prompt - 800 cached = 200 regular input
	assert.Equal(t, 200, usageRec.Tokens.Count(usage.KindInput))
	assert.Equal(t, 800, usageRec.Tokens.Count(usage.KindCacheRead))
	assert.Equal(t, 500, usageRec.Tokens.Count(usage.KindOutput))

	// Expected cost: Regular input: (200/1M * $2.50) = $0.0005
	// Cached input: (800/1M * $1.25) = $0.001; Output: (500/1M * $10.00) = $0.005; Total: $0.0065
	expectedCost := 0.0065
	assert.InDelta(t, expectedCost, usageRec.Cost.Total, 0.0000001)

	// Verify granular cost breakdown
	assert.InDelta(t, 0.0005, usageRec.Cost.Input, 0.0000001, "Input cost")
	assert.InDelta(t, 0.001, usageRec.Cost.CacheRead, 0.0000001, "CacheRead cost")
	assert.InDelta(t, 0.0, usageRec.Cost.CacheWrite, 0.0000001, "CacheWrite should be zero for OpenAI")
	assert.InDelta(t, 0.005, usageRec.Cost.Output, 0.0000001, "Output cost")
	// Sanity: breakdown sums to total
	assert.InDelta(t, usageRec.Cost.Total, usageRec.Cost.Input+usageRec.Cost.CacheRead+usageRec.Cost.CacheWrite+usageRec.Cost.Output, 0.0000001, "breakdown should sum to Total")
}

// --- Unit tests for Responses API request building ---

func TestRespBuildRequest_PromptCacheRetention_ExtendedSupported(t *testing.T) {
	// gpt-5.4 series uses the Responses API and has SupportsExtendedCache=true.
	// Verify that prompt_cache_retention: "24h" is included in the request body.
	models := []string{
		"gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano",
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			opts := llm.Request{
				Model:    model,
				Messages: llm.Messages{llm.User("Hello")},
			}

			body, err := respBuildRequest(opts)
			require.NoError(t, err)

			var req map[string]any
			require.NoError(t, json.Unmarshal(body, &req))
			assert.Equal(t, "24h", req["prompt_cache_retention"],
				"model %s should have prompt_cache_retention: 24h", model)
		})
	}
}

func TestRespBuildRequest_PromptCacheRetention_NotSupported(t *testing.T) {
	// gpt-5.4-pro has CachedInputPrice=0 (SupportsExtendedCache=false).
	// Codex-category models (gpt-5.3-codex etc.) use a different backend that
	// rejects prompt_cache_retention — they must never get the field.
	models := []string{
		"gpt-5.4-pro",
		"gpt-5.3-codex", // Codex-category: uses streamResponses but rejects the field
		"gpt-5.1-codex", // Codex-category: same
	}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			opts := llm.Request{
				Model:    model,
				Messages: llm.Messages{llm.User("Hello")},
			}

			body, err := respBuildRequest(opts)
			require.NoError(t, err)

			var req map[string]any
			require.NoError(t, json.Unmarshal(body, &req))
			_, exists := req["prompt_cache_retention"]
			assert.False(t, exists,
				"model %s should not have prompt_cache_retention set", model)
		})
	}
}

func TestRespBuildRequest_Basic(t *testing.T) {
	opts := llm.Request{
		Model: "gpt-5.1-codex",
		Messages: llm.Messages{
			llm.User("Write a function"),
		},
	}

	body, err := respBuildRequest(opts)
	require.NoError(t, err)

	var req respRequest
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	assert.Equal(t, "gpt-5.1-codex", req.Model)
	assert.True(t, req.Stream)
	require.Len(t, req.Input, 1)
	assert.Equal(t, "user", req.Input[0].Role)
	assert.Equal(t, "Write a function", req.Input[0].Content)
}

func TestRespBuildRequest_SystemAsInstructions(t *testing.T) {
	opts := llm.Request{
		Model: "gpt-5.1-codex",
		Messages: llm.Messages{
			llm.System("You are a coding assistant."),
			llm.User("Hello"),
		},
	}

	body, err := respBuildRequest(opts)
	require.NoError(t, err)

	var req respRequest
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	// First system message should become top-level instructions
	assert.Equal(t, "You are a coding assistant.", req.Instructions)
	// Only user message in input array
	require.Len(t, req.Input, 1)
	assert.Equal(t, "user", req.Input[0].Role)
}

func TestRespBuildRequest_MultipleSystemMessages(t *testing.T) {
	opts := llm.Request{
		Model: "gpt-5.1-codex",
		Messages: llm.Messages{
			llm.System("Primary instructions."),
			llm.User("Hello"),
			llm.System("Additional context."),
		},
	}

	body, err := respBuildRequest(opts)
	require.NoError(t, err)

	var req respRequest
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	// First system message -> instructions
	assert.Equal(t, "Primary instructions.", req.Instructions)
	// Second system message -> developer role in input
	require.Len(t, req.Input, 2)
	assert.Equal(t, "user", req.Input[0].Role)
	assert.Equal(t, "developer", req.Input[1].Role)
	assert.Equal(t, "Additional context.", req.Input[1].Content)
}

func TestRespBuildRequest_WithTools(t *testing.T) {
	opts := llm.Request{
		Model: "gpt-5.1-codex",
		Messages: llm.Messages{
			llm.User("test"),
		},
		Tools: []tool.Definition{
			{
				Name:        "run_tests",
				Description: "Run the test suite",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
	}

	body, err := respBuildRequest(opts)
	require.NoError(t, err)

	var req respRequest
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	require.Len(t, req.Tools, 1)
	tool := req.Tools[0]
	// Responses API has name/description at top level (not nested in "function")
	assert.Equal(t, "function", tool.Type)
	assert.Equal(t, "run_tests", tool.Name)
	assert.Equal(t, "Run the test suite", tool.Description)
}

func TestRespBuildRequest_ToolCallsAndResults(t *testing.T) {
	opts := llm.Request{
		Model: "gpt-5.1-codex",
		Messages: msg.BuildTranscript(
			msg.User("Run tests"),
			msg.Assistant(
				msg.Text("I'll run the tests."),
				msg.ToolCall(msg.NewToolCall("call_abc", "run_tests", msg.ToolArgs{"verbose": true})),
			),
			msg.Tool().Results(msg.ToolResult{ToolCallID: "call_abc", ToolOutput: "All 42 tests passed"}),
		),
	}

	body, err := respBuildRequest(opts)
	require.NoError(t, err)

	var req respRequest
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	require.Len(t, req.Input, 4)

	// User message
	assert.Equal(t, "user", req.Input[0].Role)

	// Assistant text content
	assert.Equal(t, "assistant", req.Input[1].Role)
	assert.Equal(t, "I'll run the tests.", req.Input[1].Content)

	// Tool call as function_call item
	assert.Equal(t, "function_call", req.Input[2].Type)
	assert.Equal(t, "call_abc", req.Input[2].CallID)
	assert.Equal(t, "run_tests", req.Input[2].Name)
	assert.Contains(t, req.Input[2].Arguments, "verbose")

	// Tool result as function_call_output item
	assert.Equal(t, "function_call_output", req.Input[3].Type)
	assert.Equal(t, "call_abc", req.Input[3].CallID)
	assert.Equal(t, "All 42 tests passed", req.Input[3].Output)
}

func TestRespBuildRequest_Effort(t *testing.T) {
	opts := llm.Request{
		Model:    "gpt-5.1-codex",
		Messages: llm.Messages{llm.User("test")},
		Effort:   llm.EffortHigh,
	}

	body, err := respBuildRequest(opts)
	require.NoError(t, err)

	var req respRequest
	err = json.Unmarshal(body, &req)
	require.NoError(t, err)

	// Responses API wraps reasoning effort in {"reasoning": {"effort": "..."}}
	require.NotNil(t, req.Reasoning)
	assert.Equal(t, "high", req.Reasoning.Effort)
}

// --- Unit tests for Responses API stream parsing ---

func TestRespParseStream_TextDeltas(t *testing.T) {
	sseData := `event: response.created
data: {"response":{"id":"resp_123","model":"gpt-5.1-codex"}}
event: response.output_text.delta
data: {"delta":"Hello"}
event: response.output_text.delta
data: {"delta":" world"}
event: response.completed
data: {"response":{"id":"resp_123","model":"gpt-5.1-codex","usage":{"input_tokens":10,"output_tokens":2}}}
`
	pub, ch := llm.NewEventPublisher()
	go RespParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testRespMeta("gpt-5.1-codex"))

	var deltas []string
	var gotDone bool
	var gotStart bool
	for event := range ch {
		switch event.Type {
		case llm.StreamEventStarted:
			gotStart = true
		case llm.StreamEventDelta:
			if de, ok := event.Data.(*llm.DeltaEvent); ok {
				deltas = append(deltas, de.Text)
			}
		case llm.StreamEventCompleted:
			gotDone = true
		case llm.StreamEventError:
			if err, ok := event.Data.(*llm.ErrorEvent); ok {
				t.Fatalf("Unexpected error: %v", err.Error)
			}
		}
	}

	assert.True(t, gotStart)
	assert.Equal(t, []string{"Hello", " world"}, deltas)
	assert.True(t, gotDone)
}

func TestRespParseStream_ToolCallAccumulation(t *testing.T) {
	sseData := `event: response.created
data: {"response":{"id":"resp_123","model":"gpt-5.1-codex"}}
event: response.output_item.added
data: {"output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_xyz","name":"run_tests"}}
event: response.function_call_arguments.delta
data: {"output_index":0,"delta":"{\"path\":"}
event: response.function_call_arguments.delta
data: {"output_index":0,"delta":"\"src/\"}"}
event: response.output_item.done
data: {"output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_xyz","name":"run_tests","arguments":"{\"path\":\"src/\"}"}}
event: response.completed
data: {"response":{"id":"resp_123","model":"gpt-5.1-codex","usage":{"input_tokens":50,"output_tokens":20}}}
`
	pub, ch := llm.NewEventPublisher()
	go RespParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testRespMeta("gpt-5.1-codex"))

	var toolCalls []*llm.ToolCallEvent
	for event := range ch {
		if event.Type == llm.StreamEventToolCall {
			if tc, ok := event.Data.(*llm.ToolCallEvent); ok {
				toolCalls = append(toolCalls, tc)
			}
		}
	}

	require.Len(t, toolCalls, 1)
	tc := toolCalls[0]
	assert.Equal(t, "call_xyz", tc.ToolCall.ToolCallID())
	assert.Equal(t, "run_tests", tc.ToolCall.ToolName())
	assert.Equal(t, "src/", tc.ToolCall.ToolArgs()["path"])
}

func TestRespParseStream_Usage(t *testing.T) {
	sseData := `event: response.created
data: {"response":{"id":"resp_123","model":"gpt-5.1-codex"}}
event: response.output_text.delta
data: {"delta":"test"}
event: response.completed
data: {"response":{"id":"resp_123","model":"gpt-5.1-codex","usage":{"input_tokens":100,"output_tokens":50,"input_tokens_details":{"cached_tokens":80},"output_tokens_details":{"reasoning_tokens":30}}}}
`
	pub, ch := llm.NewEventPublisher()
	go RespParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testRespMeta("gpt-5.1-codex"))

	var usageRec *usage.Record
	for event := range ch {
		if event.Type == llm.StreamEventUsageUpdated {
			if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
				usageRec = &ue.Record
			}
		}
	}

	require.NotNil(t, usageRec)
	// 100 input - 80 cached = 20 regular input; 50 output - 30 reasoning = 20 regular output
	assert.Equal(t, 20, usageRec.Tokens.Count(usage.KindInput))
	assert.Equal(t, 20, usageRec.Tokens.Count(usage.KindOutput))
	assert.Equal(t, 80, usageRec.Tokens.Count(usage.KindCacheRead))
	assert.Equal(t, 30, usageRec.Tokens.Count(usage.KindReasoning))
}

func TestRespParseStream_CostCalculation(t *testing.T) {
	// gpt-5.1-codex: $1.25/1M input, $10.00/1M output
	sseData := `event: response.created
data: {"response":{"id":"resp_123","model":"gpt-5.1-codex"}}
event: response.output_text.delta
data: {"delta":"done"}
event: response.completed
data: {"response":{"id":"resp_123","model":"gpt-5.1-codex","usage":{"input_tokens":1000,"output_tokens":500}}}
`
	pub, ch := llm.NewEventPublisher()
	go RespParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testRespMeta("gpt-5.1-codex"))

	var usageRec2 *usage.Record
	for event := range ch {
		if event.Type == llm.StreamEventUsageUpdated {
			if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
				usageRec2 = &ue.Record
			}
		}
	}

	require.NotNil(t, usageRec2)
	// Expected cost: (1000/1M * $1.25) + (500/1M * $10.00) = $0.00125 + $0.005 = $0.00625
	expectedCost := 0.00625
	assert.InDelta(t, expectedCost, usageRec2.Cost.Total, 0.0000001)
}

func TestRespParseStream_Error(t *testing.T) {
	sseData := `event: error
data: {"error":{"message":"Rate limit exceeded","code":"rate_limit_exceeded"}}
`
	pub, ch := llm.NewEventPublisher()
	go RespParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testRespMeta("gpt-5.1-codex"))

	var gotError bool
	var errMsg string
	for event := range ch {
		if event.Type == llm.StreamEventError {
			gotError = true
			if err, ok := event.Data.(*llm.ErrorEvent); ok {
				errMsg = err.Error.Error()
			}
		}
	}

	assert.True(t, gotError)
	assert.Contains(t, errMsg, "Rate limit exceeded")
	assert.Contains(t, errMsg, "rate_limit_exceeded")
}

func TestRespParseStream_StartEvent(t *testing.T) {
	sseData := `event: response.created
data: {"response":{"id":"resp_abc123","model":"gpt-5.1-codex"}}
event: response.completed
data: {"response":{"id":"resp_abc123","model":"gpt-5.1-codex","usage":{"input_tokens":10,"output_tokens":5}}}
`
	pub, ch := llm.NewEventPublisher()
	go RespParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testRespMeta("gpt-5.1-codex"))

	var start *llm.StreamStartedEvent
	for event := range ch {
		if event.Type == llm.StreamEventStarted {
			if se, ok := event.Data.(*llm.StreamStartedEvent); ok {
				start = se
			}
		}
	}

	require.NotNil(t, start)
	assert.Equal(t, "gpt-5.1-codex", start.Model)
	assert.Equal(t, "resp_abc123", start.RequestID)
}

// --- Unit tests for enrichOpts ---

func TestWantsExtendedCache_TableDriven(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{"gpt-5.4 supports cache", "gpt-5.4", true},
		{"gpt-5.1-codex supports cache", "gpt-5.1-codex", true},
		{"gpt-4.1 supports cache", "gpt-4.1", true},
		{"gpt-4o no extended cache", "gpt-4o", false},
		{"gpt-4o-mini no extended cache", "gpt-4o-mini", false},
		{"unknown model no cache", "unknown-model", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := llm.Request{
				Model:    tt.model,
				Messages: llm.Messages{llm.User("test")},
			}
			assert.Equal(t, tt.want, wantsExtendedCache(opts))
		})
	}
}

func TestEnrichOpts_EffortMapping(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		effort     llm.Effort
		thinking   llm.ThinkingMode
		wantEffort llm.Effort
	}{
		{"non-reasoning model ignores effort", "gpt-4o", llm.EffortHigh, llm.ThinkingAuto, ""},
		{"gpt-5.1 accepts high", "gpt-5.1", llm.EffortHigh, llm.ThinkingAuto, "high"},
		{"gpt-5.1 maps max to high", "gpt-5.1", llm.EffortMax, llm.ThinkingAuto, "high"},
		{"codex accepts max as xhigh", "gpt-5.1-codex", llm.EffortMax, llm.ThinkingAuto, "xhigh"},
		{"pro accepts high", "o1-pro", llm.EffortHigh, llm.ThinkingAuto, "high"},
		{"thinking off on gpt-5.1", "gpt-5.1", "", llm.ThinkingOff, "none"},
		{"thinking on defaults to high", "gpt-5.1-codex", "", llm.ThinkingOn, "high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := llm.Request{
				Model:    tt.model,
				Messages: llm.Messages{llm.User("test")},
				Effort:   tt.effort,
				Thinking: tt.thinking,
			}

			enriched, err := enrichOpts(opts)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEffort, enriched.Effort)
		})
	}
}

// --- Unit tests for useResponsesAPI ---

func TestUseResponsesAPI(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		// Codex models (categoryCodex) — always via Responses API
		{"gpt-5.1-codex", true},
		{"gpt-5.1-codex-mini", true},
		{"gpt-5.1-codex-max", true},
		{"gpt-5.2-codex", true},
		{"gpt-5.3-codex", true},
		{"gpt-5-codex", true},
		// GPT-5.4 series — UseResponsesAPI: true
		{"gpt-5.4", true},
		{"gpt-5.4-mini", true},
		{"gpt-5.4-nano", true},
		{"gpt-5.4-pro", true},
		// Chat Completions models
		{"gpt-5.1", false},
		{"gpt-4o", false},
		{"gpt-4o-mini", false},
		{"o3", false},
		{"o1-pro", false},
		{"unknown-model", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.want, useResponsesAPI(tt.model))
		})
	}
}

func TestRespParseStream_StopReasonMaxTokens(t *testing.T) {
	sseData := `event: response.created
data: {"response":{"id":"resp_123","model":"gpt-5.1-codex"}}
event: response.output_text.delta
data: {"output_index":0,"delta":"Hello"}
event: response.completed
data: {"response":{"id":"resp_123","model":"gpt-5.1-codex","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":10,"output_tokens":5}}}
`
	pub, ch := llm.NewEventPublisher()
	go RespParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testRespMeta("gpt-5.1-codex"))

	var stopReason llm.StopReason
	for event := range ch {
		if event.Type == llm.StreamEventCompleted {
			if ce, ok := event.Data.(*llm.CompletedEvent); ok {
				stopReason = ce.StopReason
			}
		}
	}
	assert.Equal(t, llm.StopReasonMaxTokens, stopReason)
}

func TestRespParseStream_StopReasonContentFilter(t *testing.T) {
	sseData := `event: response.created
data: {"response":{"id":"resp_456","model":"gpt-5.1-codex"}}
event: response.completed
data: {"response":{"id":"resp_456","model":"gpt-5.1-codex","status":"incomplete","incomplete_details":{"reason":"content_filter"},"usage":{"input_tokens":10,"output_tokens":0}}}
`
	pub, ch := llm.NewEventPublisher()
	go RespParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testRespMeta("gpt-5.1-codex"))

	var stopReason llm.StopReason
	for event := range ch {
		if event.Type == llm.StreamEventCompleted {
			if ce, ok := event.Data.(*llm.CompletedEvent); ok {
				stopReason = ce.StopReason
			}
		}
	}
	assert.Equal(t, llm.StopReasonContentFilter, stopReason)
}
