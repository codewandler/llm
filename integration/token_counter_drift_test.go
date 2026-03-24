package integration_test

// token_counter_drift_test.go — integration tests that verify the token count
// estimates produced by TokenCounter are close to the actual input_tokens
// reported by the provider API.
//
// Tests are skipped when the required credentials are not present, so they
// never block CI that lacks API keys.

import (
	"context"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	anthropicprovider "github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/bedrock"
	minimaxprovider "github.com/codewandler/llm/provider/minimax"
	"github.com/codewandler/llm/provider/openai"
)

// driftPct returns the absolute percentage difference between estimated and
// actual token counts: abs(estimated-actual)/actual * 100.
func driftPct(estimated, actual int) float64 {
	if actual == 0 {
		return 0
	}
	return math.Abs(float64(estimated-actual)) / float64(actual) * 100
}

// testTokenCounterDrift is a shared helper that:
//  1. Calls CountTokens on the provider (local estimate)
//  2. Sends the same request via CreateStream and reads the actual usage
//  3. Asserts the drift is within maxDriftPct
//  4. Logs the estimate, actual, and drift for visibility
func testTokenCounterDrift(t *testing.T, provider llm.Provider, model string, msgs llm.Messages, tools []llm.ToolDefinition, maxDriftPct float64) {
	t.Helper()
	ctx := context.Background()

	// Step 1 — estimate
	tc, ok := provider.(llm.TokenCounter)
	require.True(t, ok, "provider %q does not implement llm.TokenCounter", provider.Name())

	est, err := tc.CountTokens(ctx, llm.TokenCountRequest{
		Model:    model,
		Messages: msgs,
		Tools:    tools,
	})
	require.NoError(t, err, "CountTokens failed")
	require.NotNil(t, est)

	// Step 2 — actual from API
	stream, err := provider.CreateStream(ctx, llm.Request{
		Model:    model,
		Messages: msgs,
		Tools:    tools,
	})
	require.NoError(t, err)

	result := <-llm.Process(ctx, stream).Result()
	require.NoError(t, result.Error())
	require.NotNil(t, result.Usage, "provider returned no usage data")

	actual := result.Usage.InputTokens
	drift := driftPct(est.InputTokens, actual)

	t.Logf("model=%s  estimated=%d  actual=%d  drift=%.1f%%", model, est.InputTokens, actual, drift)
	t.Logf("  PerMessage=%v  SystemTokens=%d  UserTokens=%d  AssistantTokens=%d  ToolResultTokens=%d",
		est.PerMessage, est.SystemTokens, est.UserTokens, est.AssistantTokens, est.ToolResultTokens)
	if len(est.PerTool) > 0 {
		t.Logf("  ToolsTokens=%d  PerTool=%v", est.ToolsTokens, est.PerTool)
	}

	// Step 3 — assert drift is within tolerance
	assert.LessOrEqual(t, drift, maxDriftPct,
		"token count drift %.1f%% exceeds %.1f%% threshold (estimated=%d, actual=%d)",
		drift, maxDriftPct, est.InputTokens, actual)
}

// --- Anthropic (direct API key) ---

func TestTokenCounterDrift_Anthropic(t *testing.T) {
	key := requireEnv(t, "ANTHROPIC_API_KEY")
	p := anthropicprovider.New(llm.WithAPIKey(key))
	model := "claude-haiku-4-5-20251001"

	t.Run("simple", func(t *testing.T) {
		msgs := llm.Messages{
			&llm.SystemMsg{Content: "You are a helpful assistant."},
			&llm.UserMsg{Content: "What is the capital of France?"},
		}
		// cl100k_base is an approximation; Anthropic's tokenizer is proprietary.
		// Per-message framing overhead adds ~5-10 tokens. 40% covers both factors.
		testTokenCounterDrift(t, p, model, msgs, nil, 40.0)
	})

	t.Run("multi_turn", func(t *testing.T) {
		msgs := llm.Messages{
			&llm.SystemMsg{Content: "You are a helpful assistant."},
			&llm.UserMsg{Content: "What is 2+2?"},
			&llm.AssistantMsg{Content: "2+2 equals 4."},
			&llm.UserMsg{Content: "What about 3+3?"},
		}
		testTokenCounterDrift(t, p, model, msgs, nil, 40.0)
	})

	t.Run("with_tools", func(t *testing.T) {
		msgs := llm.Messages{
			&llm.UserMsg{Content: "What is the weather in Berlin?"},
		}
		tools := []llm.ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get the current weather for a location.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type":        "string",
							"description": "City name",
						},
					},
					"required": []string{"location"},
				},
			},
		}
		// Tool schemas are tokenized very differently by Anthropic's tokenizer vs
		// cl100k_base. 90% threshold documents the known large divergence.
		testTokenCounterDrift(t, p, model, msgs, tools, 90.0)
	})
}

// --- OpenAI ---

func TestTokenCounterDrift_OpenAI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	key := requireEnv(t, "OPENAI_KEY")
	p := openai.New(llm.WithAPIKey(key))
	model := openai.ModelGPT4oMini

	t.Run("simple", func(t *testing.T) {
		msgs := llm.Messages{
			&llm.SystemMsg{Content: "You are a helpful assistant."},
			&llm.UserMsg{Content: "What is the capital of France?"},
		}
		testTokenCounterDrift(t, p, model, msgs, nil, 5.0) // tiktoken should be near-exact
	})

	t.Run("multi_turn", func(t *testing.T) {
		msgs := llm.Messages{
			&llm.SystemMsg{Content: "You are a helpful assistant."},
			&llm.UserMsg{Content: "What is 2+2?"},
			&llm.AssistantMsg{Content: "2+2 equals 4."},
			&llm.UserMsg{Content: "What about 3+3?"},
		}
		testTokenCounterDrift(t, p, model, msgs, nil, 5.0)
	})

	t.Run("with_tools", func(t *testing.T) {
		msgs := llm.Messages{
			&llm.UserMsg{Content: "What is the weather in Berlin?"},
		}
		tools := []llm.ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get the current weather for a location.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type":        "string",
							"description": "City name",
						},
					},
					"required": []string{"location"},
				},
			},
		}
		testTokenCounterDrift(t, p, model, msgs, tools, 10.0) // tools have more serialisation variance
	})
}

// --- Bedrock ---

func TestTokenCounterDrift_Bedrock(t *testing.T) {
	if !isBedrockAvailable() {
		t.Skip("requires AWS credentials (AWS_ACCESS_KEY_ID or ~/.aws/credentials)")
	}
	p := bedrock.New(bedrock.WithRegion(getAWSRegion()))
	model := bedrock.ModelHaikuLatest

	t.Run("simple", func(t *testing.T) {
		msgs := llm.Messages{
			&llm.SystemMsg{Content: "You are a helpful assistant."},
			&llm.UserMsg{Content: "What is the capital of France?"},
		}
		testTokenCounterDrift(t, p, model, msgs, nil, 40.0)
	})

	t.Run("multi_turn", func(t *testing.T) {
		msgs := llm.Messages{
			&llm.SystemMsg{Content: "You are a helpful assistant."},
			&llm.UserMsg{Content: "What is 2+2?"},
			&llm.AssistantMsg{Content: "2+2 equals 4."},
			&llm.UserMsg{Content: "What about 3+3?"},
		}
		testTokenCounterDrift(t, p, model, msgs, nil, 40.0)
	})
}

// requireEnv skips the test if the named env var is not set, otherwise returns it.
func requireEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Skipf("requires %s env var", name)
	}
	return v
}

// --- MiniMax ---

func TestTokenCounterDrift_MiniMax(t *testing.T) {
	key := requireEnv(t, "MINIMAX_API_KEY")
	p := minimaxprovider.New(minimaxprovider.WithLLMOpts(llm.WithAPIKey(key)))
	model := minimaxprovider.ModelM27

	t.Run("simple", func(t *testing.T) {
		msgs := llm.Messages{
			&llm.SystemMsg{Content: "You are a helpful assistant."},
			&llm.UserMsg{Content: "What is the capital of France?"},
		}
		// MiniMax uses a proprietary BPE (200K vocab). With a system message present
		// the hidden default system prompt is suppressed, so the only unaccounted
		// overhead is minor per-message framing rounding. Measured: 7.4% drift.
		// 15% gives a comfortable safety margin above the observed maximum.
		testTokenCounterDrift(t, p, model, msgs, nil, 15.0)
	})

	t.Run("multi_turn", func(t *testing.T) {
		msgs := llm.Messages{
			&llm.SystemMsg{Content: "You are a helpful assistant."},
			&llm.UserMsg{Content: "What is 2+2?"},
			&llm.AssistantMsg{Content: "2+2 equals 4."},
			&llm.UserMsg{Content: "What about 3+3?"},
		}
		// Perfect match in calibration (0.0% drift). 10% safety margin.
		testTokenCounterDrift(t, p, model, msgs, nil, 10.0)
	})

	t.Run("with_tools", func(t *testing.T) {
		msgs := llm.Messages{
			&llm.UserMsg{Content: "What is the weather in Berlin?"},
		}
		tools := []llm.ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get the current weather for a location.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{
							"type":        "string",
							"description": "City name",
						},
					},
					"required": []string{"location"},
				},
			},
		}
		// Tool framing is fully accounted for (measured: 0.0% drift).
		// 5% safety margin for minor API non-determinism.
		testTokenCounterDrift(t, p, model, msgs, tools, 5.0)
	})
}
