//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/openrouter"
)

func TestSmokeOpenRouterResponses(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") != "1" {
		t.Skip("set RUN_INTEGRATION=1 to run integration smoke tests")
	}
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("set OPENROUTER_API_KEY to run OpenRouter smoke tests")
	}

	model := os.Getenv("OPENROUTER_MODEL")
	if model == "" {
		model = "openai/gpt-4o-mini"
	}

	opts := []llm.Option{llm.WithAPIKey(apiKey)}
	if baseURL := os.Getenv("OPENROUTER_BASE_URL"); baseURL != "" {
		opts = append(opts, llm.WithBaseURL(baseURL))
	}

	provider := openrouter.New(opts...)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stream, err := provider.CreateStream(ctx, llm.Request{
		Model: model,
		Messages: msg.BuildTranscript(
			msg.User("Reply with exactly the word pong."),
		),
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}

	var (
		sawRequest   bool
		sawStarted   bool
		sawDelta     bool
		sawCompleted bool
		text         strings.Builder
	)

	for env := range stream {
		switch env.Type {
		case llm.StreamEventRequest:
			sawRequest = true
			reqEv, ok := env.Data.(*llm.RequestEvent)
			if !ok {
				t.Fatalf("request event payload = %T, want *llm.RequestEvent", env.Data)
			}
			if reqEv.ResolvedApiType != llm.ApiTypeOpenAIResponses {
				t.Fatalf("ResolvedApiType = %q, want %q", reqEv.ResolvedApiType, llm.ApiTypeOpenAIResponses)
			}
		case llm.StreamEventStarted:
			sawStarted = true
		case llm.StreamEventDelta:
			sawDelta = true
			delta, ok := env.Data.(*llm.DeltaEvent)
			if !ok {
				t.Fatalf("delta payload = %T, want *llm.DeltaEvent", env.Data)
			}
			text.WriteString(delta.Text)
		case llm.StreamEventCompleted:
			sawCompleted = true
		case llm.StreamEventError:
			errEv, ok := env.Data.(*llm.ErrorEvent)
			if !ok {
				t.Fatalf("error payload = %T, want *llm.ErrorEvent", env.Data)
			}
			t.Fatalf("unexpected stream error: %v", errEv.Error)
		}
	}

	if !sawRequest {
		t.Fatalf("expected a request event")
	}
	if !sawStarted {
		t.Fatalf("expected a started event")
	}
	if !sawDelta {
		t.Fatalf("expected at least one delta event")
	}
	if !sawCompleted {
		t.Fatalf("expected a completed event")
	}
	if !strings.Contains(strings.ToLower(text.String()), "pong") {
		t.Fatalf("expected streamed text to contain pong, got %q", text.String())
	}
}

func TestSmokeClaudeMessages(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") != "1" {
		t.Skip("set RUN_INTEGRATION=1 to run integration smoke tests")
	}
	if !claude.LocalTokenProviderAvailable() {
		t.Skip("Claude local token provider not available")
	}

	provider := claude.New()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stream, err := provider.CreateStream(ctx, llm.Request{
		Model: "sonnet",
		Messages: msg.BuildTranscript(
			msg.User("Reply with exactly the word pong."),
		),
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}

	var (
		sawRequest   bool
		sawStarted   bool
		sawDelta     bool
		sawCompleted bool
		text         strings.Builder
	)

	for env := range stream {
		switch env.Type {
		case llm.StreamEventRequest:
			sawRequest = true
			reqEv, ok := env.Data.(*llm.RequestEvent)
			if !ok {
				t.Fatalf("request event payload = %T, want *llm.RequestEvent", env.Data)
			}
			if reqEv.ResolvedApiType != llm.ApiTypeAnthropicMessages {
				t.Fatalf("ResolvedApiType = %q, want %q", reqEv.ResolvedApiType, llm.ApiTypeAnthropicMessages)
			}
		case llm.StreamEventStarted:
			sawStarted = true
		case llm.StreamEventDelta:
			sawDelta = true
			delta, ok := env.Data.(*llm.DeltaEvent)
			if !ok {
				t.Fatalf("delta payload = %T, want *llm.DeltaEvent", env.Data)
			}
			text.WriteString(delta.Text)
		case llm.StreamEventCompleted:
			sawCompleted = true
		case llm.StreamEventError:
			errEv, ok := env.Data.(*llm.ErrorEvent)
			if !ok {
				t.Fatalf("error payload = %T, want *llm.ErrorEvent", env.Data)
			}
			t.Fatalf("unexpected stream error: %v", errEv.Error)
		}
	}

	if !sawRequest {
		t.Fatalf("expected a request event")
	}
	if !sawStarted {
		t.Fatalf("expected a started event")
	}
	if !sawDelta {
		t.Fatalf("expected at least one delta event")
	}
	if !sawCompleted {
		t.Fatalf("expected a completed event")
	}
	if !strings.Contains(strings.ToLower(text.String()), "pong") {
		t.Fatalf("expected streamed text to contain pong, got %q", text.String())
	}
}
