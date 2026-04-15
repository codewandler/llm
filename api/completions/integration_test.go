package completions_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codewandler/llm/api/completions"
	"github.com/stretchr/testify/require"
)

// Requires:
//
//	OPENROUTER_API_KEY
//	OPENROUTER_COMPLETIONS_FREE_MODEL (optional; default google/gemma-3-27b-it:free)
//
// OpenRouter exposes an OpenAI-compatible Chat Completions endpoint and offers
// free models, making this test runnable without paid OpenAI credits.
func TestIntegration_CompletionsStream(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set — skipping integration test")
	}

	model := os.Getenv("OPENROUTER_COMPLETIONS_FREE_MODEL")
	if model == "" {
		model = "google/gemma-3-27b-it:free"
	}

	client := completions.NewClient(
		completions.WithBaseURL("https://openrouter.ai/api"),
		completions.WithHeaderFunc(completions.BearerAuthFunc(apiKey)),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	handle, err := client.Stream(ctx, &completions.Request{
		Model:  model,
		Stream: true,
		Messages: []completions.Message{
			{Role: "user", Content: "Reply with exactly: ok"},
		},
		StreamOptions: &completions.StreamOptions{IncludeUsage: true},
	})
	if shouldSkipOpenRouterError(err) {
		t.Skipf("skipping integration test due environment/account constraints: %v", err)
	}
	require.NoError(t, err)

	var sawDone bool
	for ev := range handle.Events {
		if shouldSkipOpenRouterError(ev.Err) {
			t.Skipf("skipping integration test due environment/account constraints: %v", ev.Err)
		}
		if ev.Err != nil {
			t.Fatalf("stream error: %v", ev.Err)
		}
		if ev.Done {
			sawDone = true
		}
	}
	require.True(t, sawDone)
}

func shouldSkipOpenRouterError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "guardrail restrictions") ||
		strings.Contains(msg, "data policy") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "insufficient credits") ||
		strings.Contains(msg, "quota") ||
		strings.Contains(msg, "provider returned error (http 429)") {
		return true
	}
	return false
}
