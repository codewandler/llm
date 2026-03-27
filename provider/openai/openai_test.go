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
	"github.com/codewandler/llm/tool"
)

// testMeta returns a ccStreamMeta for testing.
func testMeta(model string) ccStreamMeta {
	return ccStreamMeta{
		requestedModel: model,
		startTime:      time.Now(),
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
		Messages: llm.Messages{
			llm.User("What's the weather?"),
			llm.Assistant("", tool.NewToolCall("call_123", "get_weather", map[string]any{"location": "Paris"})),
		},
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
		Messages: llm.Messages{
			llm.User("What's the weather?"),
			llm.Assistant("", tool.NewToolCall("call_123", "get_weather", map[string]any{"location": "Paris"})),
			llm.Tool("call_123", `{"temp": 72, "conditions": "sunny"}`),
		},
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

	var usage *llm.Usage
	for event := range ch {
		if event.Type == llm.StreamEventCompleted {
			if ce, ok := event.Data.(*llm.CompletedEvent); ok {
				_ = ce.StopReason
			}
		}
		if event.Type == llm.StreamEventUsageUpdated {
			if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
				usage = &ue.Usage
			}
		}
	}

	require.NotNil(t, usage)
	assert.Equal(t, 10, usage.InputTokens)
	assert.Equal(t, 5, usage.OutputTokens)
	assert.Equal(t, 15, usage.TotalTokens)
}

func TestParseStream_UsageWithDetails(t *testing.T) {
	sseData := `data: {"choices":[{"delta":{"content":"test"}}]}
data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150,"prompt_tokens_details":{"cached_tokens":80},"completion_tokens_details":{"reasoning_tokens":30}}}
data: [DONE]
`
	pub, ch := llm.NewEventPublisher()
	go ccParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-5"))

	var usage *llm.Usage
	for event := range ch {
		if event.Type == llm.StreamEventUsageUpdated {
			if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
				usage = &ue.Usage
			}
		}
	}

	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.InputTokens)
	assert.Equal(t, 50, usage.OutputTokens)
	assert.Equal(t, 150, usage.TotalTokens)
	assert.Equal(t, 80, usage.CacheReadTokens)
	assert.Equal(t, 30, usage.ReasoningTokens)
}

// --- Unit tests for calculateCost ---

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		name              string
		model             string
		usage             *llm.Usage
		wantCost          float64
		wantInputCost     float64
		wantCacheReadCost float64
		wantOutputCost    float64
	}{
		{
			name:  "gpt-4o basic",
			model: "gpt-4o",
			usage: &llm.Usage{
				InputTokens:  1_000_000,
				OutputTokens: 1_000_000,
			},
			// $2.50/1M input + $10.00/1M output = $12.50
			wantCost:          12.50,
			wantInputCost:     2.50,
			wantCacheReadCost: 0,
			wantOutputCost:    10.00,
		},
		{
			name:  "gpt-4o with cache",
			model: "gpt-4o",
			usage: &llm.Usage{
				InputTokens:     1_000_000,
				OutputTokens:    500_000,
				CacheReadTokens: 800_000,
			},
			// (200k regular * $2.50/1M) + (800k cached * $1.25/1M) + (500k output * $10.00/1M)
			// = $0.50 + $1.00 + $5.00 = $6.50
			wantCost:          6.50,
			wantInputCost:     0.50,
			wantCacheReadCost: 1.00,
			wantOutputCost:    5.00,
		},
		{
			name:  "gpt-4o-mini cheap",
			model: "gpt-4o-mini",
			usage: &llm.Usage{
				InputTokens:  1_000_000,
				OutputTokens: 1_000_000,
			},
			// $0.15/1M input + $0.60/1M output = $0.75
			wantCost:          0.75,
			wantInputCost:     0.15,
			wantCacheReadCost: 0,
			wantOutputCost:    0.60,
		},
		{
			name:  "o1-pro expensive",
			model: "o1-pro",
			usage: &llm.Usage{
				InputTokens:  1_000_000,
				OutputTokens: 1_000_000,
			},
			// $150/1M input + $600/1M output = $750
			wantCost:          750.0,
			wantInputCost:     150.0,
			wantCacheReadCost: 0,
			wantOutputCost:    600.0,
		},
		{
			name:     "unknown model returns zero",
			model:    "unknown-model",
			usage:    &llm.Usage{InputTokens: 1000, OutputTokens: 1000},
			wantCost: 0,
		},
		{
			name:     "nil usage returns zero",
			model:    "gpt-4o",
			usage:    nil,
			wantCost: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy so we can check mutation
			var u *llm.Usage
			if tt.usage != nil {
				c := *tt.usage
				u = &c
			}
			calculateCost(tt.model, u)
			if u == nil {
				return // nil case; no further checks
			}
			assert.InDelta(t, tt.wantCost, u.Cost, 0.001, "Cost mismatch")
			assert.InDelta(t, tt.wantInputCost, u.InputCost, 0.001, "InputCost mismatch")
			assert.InDelta(t, tt.wantCacheReadCost, u.CacheReadCost, 0.001, "CacheReadCost mismatch")
			assert.InDelta(t, tt.wantOutputCost, u.OutputCost, 0.001, "OutputCost mismatch")
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
			assert.Greater(t, info.InputPrice, 0.0, "model %s should have input price", id)
			assert.Greater(t, info.OutputPrice, 0.0, "model %s should have output price", id)
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

// --- Unit tests for mapReasoningEffort ---

func TestMapReasoningEffort_NonReasoning(t *testing.T) {
	models := []string{"gpt-4o", "gpt-4o-mini", "gpt-4", "gpt-3.5-turbo", "gpt-4.1"}
	efforts := []llm.ReasoningEffort{"", llm.ReasoningEffortNone, llm.ReasoningEffortMinimal, llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh}

	for _, model := range models {
		for _, effort := range efforts {
			t.Run(model+"_"+string(effort), func(t *testing.T) {
				result, err := mapReasoningEffort(model, effort)
				require.NoError(t, err)
				assert.Empty(t, result, "non-reasoning models should return empty")
			})
		}
	}
}

func TestMapReasoningEffort_PreGPT51(t *testing.T) {
	models := []string{"gpt-5", "gpt-5-mini", "gpt-5-nano", "o1", "o3"}

	tests := []struct {
		effort  llm.ReasoningEffort
		want    string
		wantErr bool
	}{
		{"", "", false},
		{llm.ReasoningEffortMinimal, "minimal", false},
		{llm.ReasoningEffortLow, "low", false},
		{llm.ReasoningEffortMedium, "medium", false},
		{llm.ReasoningEffortHigh, "high", false},
		{llm.ReasoningEffortNone, "", true},
		{llm.ReasoningEffortXHigh, "", true},
	}

	for _, model := range models {
		for _, tt := range tests {
			t.Run(model+"_"+string(tt.effort), func(t *testing.T) {
				result, err := mapReasoningEffort(model, tt.effort)
				if tt.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tt.want, result)
				}
			})
		}
	}
}

func TestMapReasoningEffort_GPT51(t *testing.T) {
	model := "gpt-5.1"

	tests := []struct {
		effort  llm.ReasoningEffort
		want    string
		wantErr bool
	}{
		{"", "", false},
		{llm.ReasoningEffortNone, "none", false},
		{llm.ReasoningEffortMinimal, "low", false},
		{llm.ReasoningEffortLow, "low", false},
		{llm.ReasoningEffortMedium, "medium", false},
		{llm.ReasoningEffortHigh, "high", false},
		{llm.ReasoningEffortXHigh, "", true},
	}

	for _, tt := range tests {
		t.Run(string(tt.effort), func(t *testing.T) {
			result, err := mapReasoningEffort(model, tt.effort)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, result)
			}
		})
	}
}

func TestMapReasoningEffort_Pro(t *testing.T) {
	models := []string{"gpt-5-pro", "gpt-5.2-pro", "o1-pro", "o3-pro"}

	for _, model := range models {
		t.Run(model+"_empty", func(t *testing.T) {
			result, err := mapReasoningEffort(model, "")
			require.NoError(t, err)
			assert.Empty(t, result)
		})

		t.Run(model+"_high", func(t *testing.T) {
			result, err := mapReasoningEffort(model, llm.ReasoningEffortHigh)
			require.NoError(t, err)
			assert.Equal(t, "high", result)
		})

		for _, effort := range []llm.ReasoningEffort{llm.ReasoningEffortNone, llm.ReasoningEffortMinimal, llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortXHigh} {
			t.Run(model+"_"+string(effort), func(t *testing.T) {
				_, err := mapReasoningEffort(model, effort)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "must be")
			})
		}
	}
}

func TestMapReasoningEffort_CodexMax(t *testing.T) {
	models := []string{"gpt-5.1-codex", "gpt-5.2-codex", "gpt-5.1-codex-max"}

	tests := []struct {
		effort  llm.ReasoningEffort
		want    string
		wantErr bool
	}{
		{"", "", false},
		{llm.ReasoningEffortNone, "none", false},
		{llm.ReasoningEffortMinimal, "low", false},
		{llm.ReasoningEffortLow, "low", false},
		{llm.ReasoningEffortMedium, "medium", false},
		{llm.ReasoningEffortHigh, "high", false},
		{llm.ReasoningEffortXHigh, "xhigh", false},
	}

	for _, model := range models {
		for _, tt := range tests {
			t.Run(model+"_"+string(tt.effort), func(t *testing.T) {
				result, err := mapReasoningEffort(model, tt.effort)
				if tt.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tt.want, result)
				}
			})
		}
	}
}

// --- enrichOpts + ccBuildRequest integration tests ---
// These test the full pipeline: enrichOpts (reasoning mapping, cache retention)
// feeding into ccBuildRequest.

func TestBuildRequest_ReasoningEffortOmitted(t *testing.T) {
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

func TestBuildRequest_ReasoningEffortSet(t *testing.T) {
	// enrichOpts passes the raw effort string through for pre-GPT51 models.
	opts, err := enrichOpts(llm.Request{
		Model:           "gpt-5",
		Messages:        llm.Messages{llm.User("Hello")},
		ReasoningEffort: llm.ReasoningEffortLow,
	})
	require.NoError(t, err)

	body, err := ccBuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))
	assert.Equal(t, "low", req["reasoning_effort"])
}

func TestBuildRequest_ReasoningEffortMapped(t *testing.T) {
	// enrichOpts maps "minimal" → "low" for gpt-5.1.
	opts, err := enrichOpts(llm.Request{
		Model:           "gpt-5.1",
		Messages:        llm.Messages{llm.User("Hello")},
		ReasoningEffort: llm.ReasoningEffortMinimal,
	})
	require.NoError(t, err)

	body, err := ccBuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))
	assert.Equal(t, "low", req["reasoning_effort"], "minimal should be mapped to low for gpt-5.1")
}

func TestBuildRequest_ReasoningEffortError(t *testing.T) {
	_, err := enrichOpts(llm.Request{
		Model:           "gpt-5-pro",
		Messages:        llm.Messages{llm.User("Hello")},
		ReasoningEffort: llm.ReasoningEffortLow,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be")
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

func TestEnrichOpts_ReasoningEffortOmitted(t *testing.T) {
	opts := llm.Request{Model: "gpt-4o", Messages: llm.Messages{llm.User("hi")}}
	out, err := enrichOpts(opts)
	require.NoError(t, err)
	assert.Empty(t, out.ReasoningEffort)
}

func TestEnrichOpts_ReasoningEffortMappedMinimalToLow(t *testing.T) {
	opts := llm.Request{
		Model:           "gpt-5.1",
		ReasoningEffort: llm.ReasoningEffortMinimal,
		Messages:        llm.Messages{llm.User("hi")},
	}
	out, err := enrichOpts(opts)
	require.NoError(t, err)
	assert.Equal(t, llm.ReasoningEffort("low"), out.ReasoningEffort)
}

func TestEnrichOpts_ReasoningEffortErrorProModel(t *testing.T) {
	opts := llm.Request{
		Model:           "gpt-5-pro",
		ReasoningEffort: llm.ReasoningEffortLow,
		Messages:        llm.Messages{llm.User("hi")},
	}
	_, err := enrichOpts(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be")
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

func TestMapReasoningEffort_Codex(t *testing.T) {
	tests := []struct {
		effort  llm.ReasoningEffort
		want    string
		wantErr bool
	}{
		{"", "", false},
		{llm.ReasoningEffortNone, "none", false},
		{llm.ReasoningEffortMinimal, "low", false},
		{llm.ReasoningEffortLow, "low", false},
		{llm.ReasoningEffortMedium, "medium", false},
		{llm.ReasoningEffortHigh, "high", false},
		{llm.ReasoningEffortXHigh, "xhigh", false},
	}

	for _, model := range []string{"gpt-5.1-codex", "gpt-5.2-codex", "gpt-5.1-codex-max"} {
		for _, tt := range tests {
			t.Run(model+"_"+string(tt.effort), func(t *testing.T) {
				result, err := mapReasoningEffort(model, tt.effort)
				if tt.wantErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tt.want, result)
				}
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

	var usage *llm.Usage
	for event := range ch {
		if event.Type == llm.StreamEventUsageUpdated {
			if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
				usage = &ue.Usage
			}
		}
	}

	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.InputTokens)
	assert.Equal(t, 50, usage.OutputTokens)

	// Expected cost: (100/1M * $2.50) + (50/1M * $10.00) = $0.00025 + $0.0005 = $0.00075
	expectedCost := 0.00075
	assert.InDelta(t, expectedCost, usage.Cost, 0.0000001)
}

func TestParseStream_CostCalculation_WithCache(t *testing.T) {
	// gpt-4o: $2.50/1M input, $1.25/1M cached, $10.00/1M output
	sseData := `data: {"id":"chatcmpl-123","model":"gpt-4o","choices":[{"delta":{"content":"Hi"}}]}
data: {"id":"chatcmpl-123","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":500,"total_tokens":1500,"prompt_tokens_details":{"cached_tokens":800}}}
data: [DONE]
`
	pub, ch := llm.NewEventPublisher()
	go ccParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-4o"))

	var usage *llm.Usage
	for event := range ch {
		if event.Type == llm.StreamEventUsageUpdated {
			if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
				usage = &ue.Usage
			}
		}
	}

	require.NotNil(t, usage)
	assert.Equal(t, 1000, usage.InputTokens)
	assert.Equal(t, 800, usage.CacheReadTokens)
	assert.Equal(t, 500, usage.OutputTokens)

	// Expected cost:
	// Regular input: (200/1M * $2.50) = $0.0005
	// Cached input: (800/1M * $1.25) = $0.001
	// Output: (500/1M * $10.00) = $0.005
	// Total: $0.0065
	expectedCost := 0.0065
	assert.InDelta(t, expectedCost, usage.Cost, 0.0000001)

	// Verify granular cost breakdown
	assert.InDelta(t, 0.0005, usage.InputCost, 0.0000001, "InputCost")
	assert.InDelta(t, 0.001, usage.CacheReadCost, 0.0000001, "CacheReadCost")
	assert.InDelta(t, 0.0, usage.CacheWriteCost, 0.0000001, "CacheWriteCost should be zero for OpenAI")
	assert.InDelta(t, 0.005, usage.OutputCost, 0.0000001, "OutputCost")
	// Sanity: breakdown sums to total
	assert.InDelta(t, usage.Cost, usage.InputCost+usage.CacheReadCost+usage.CacheWriteCost+usage.OutputCost, 0.0000001, "breakdown should sum to Cost")
}

// --- Unit tests for Responses API request building ---

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
		Messages: llm.Messages{
			llm.User("Run tests"),
			llm.Assistant("I'll run the tests.", tool.NewToolCall("call_abc", "run_tests", tool.Args{"verbose": true})),
			llm.ToolResult(tool.NewResult("call_abc", "All 42 tests passed", false)),
		},
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

func TestRespBuildRequest_ReasoningEffort(t *testing.T) {
	opts := llm.Request{
		Model:           "gpt-5.1-codex",
		Messages:        llm.Messages{llm.User("test")},
		ReasoningEffort: llm.ReasoningEffortHigh,
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
	go respParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-5.1-codex"))

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
	go respParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-5.1-codex"))

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
	go respParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-5.1-codex"))

	var usage *llm.Usage
	for event := range ch {
		if event.Type == llm.StreamEventUsageUpdated {
			if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
				usage = &ue.Usage
			}
		}
	}

	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.InputTokens)
	assert.Equal(t, 50, usage.OutputTokens)
	assert.Equal(t, 150, usage.TotalTokens)
	assert.Equal(t, 80, usage.CacheReadTokens)
	assert.Equal(t, 30, usage.ReasoningTokens)
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
	go respParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-5.1-codex"))

	var usage *llm.Usage
	for event := range ch {
		if event.Type == llm.StreamEventUsageUpdated {
			if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
				usage = &ue.Usage
			}
		}
	}

	require.NotNil(t, usage)
	// Expected cost: (1000/1M * $1.25) + (500/1M * $10.00) = $0.00125 + $0.005 = $0.00625
	expectedCost := 0.00625
	assert.InDelta(t, expectedCost, usage.Cost, 0.0000001)
}

func TestRespParseStream_Error(t *testing.T) {
	sseData := `event: error
data: {"error":{"message":"Rate limit exceeded","code":"rate_limit_exceeded"}}
`
	pub, ch := llm.NewEventPublisher()
	go respParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-5.1-codex"))

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
	go respParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-5.1-codex"))

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

func TestEnrichOpts_ReasoningEffortMapping(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		effort     llm.ReasoningEffort
		wantEffort llm.ReasoningEffort
		wantErr    bool
	}{
		{"non-reasoning model ignores effort", "gpt-4o", llm.ReasoningEffortHigh, "", false},
		{"gpt-5.1 accepts high", "gpt-5.1", llm.ReasoningEffortHigh, "high", false},
		{"gpt-5.1 maps minimal to low", "gpt-5.1", llm.ReasoningEffortMinimal, "low", false},
		{"gpt-5.1 rejects xhigh", "gpt-5.1", llm.ReasoningEffortXHigh, "", true},
		{"codex accepts xhigh", "gpt-5.1-codex", llm.ReasoningEffortXHigh, "xhigh", false},
		{"codex maps minimal to low", "gpt-5.1-codex", llm.ReasoningEffortMinimal, "low", false},
		{"pro only accepts high", "o1-pro", llm.ReasoningEffortHigh, "high", false},
		{"pro rejects medium", "o1-pro", llm.ReasoningEffortMedium, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := llm.Request{
				Model:           tt.model,
				Messages:        llm.Messages{llm.User("test")},
				ReasoningEffort: tt.effort,
			}

			enriched, err := enrichOpts(opts)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantEffort, enriched.ReasoningEffort)
			}
		})
	}
}

// --- Unit tests for isCodexModel ---

func TestIsCodexModel(t *testing.T) {
	tests := []struct {
		model   string
		isCodex bool
	}{
		{"gpt-5.1-codex", true},
		{"gpt-5.1-codex-mini", true},
		{"gpt-5.1-codex-max", true},
		{"gpt-5.2-codex", true},
		{"gpt-5.3-codex", true},
		{"gpt-5-codex", true},
		{"gpt-5.1", false},
		{"gpt-5.4", false},
		{"gpt-4o", false},
		{"gpt-4o-mini", false},
		{"o3", false},
		{"o1-pro", false},
		{"unknown-model", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.isCodex, isCodexModel(tt.model))
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
	go respParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-5.1-codex"))

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
	go respParseStream(context.Background(), io.NopCloser(strings.NewReader(sseData)), pub, testMeta("gpt-5.1-codex"))

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
