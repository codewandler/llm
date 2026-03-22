package llm_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	anthropicdirect "github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/bedrock"
)

// largeCacheablePrompt returns a system prompt large enough to reliably exceed
// the minimum caching threshold for all providers (Anthropic: 1024 tokens,
// Bedrock: 2048 tokens). Content is varied enough to avoid tokenizer deduplication.
func largeCacheablePrompt() string {
	const paragraph = `You are an expert software engineer specialising in distributed systems, ` +
		`concurrent programming, and large-scale infrastructure. Your role is to provide ` +
		`precise, actionable technical guidance. When answering questions you consider ` +
		`trade-offs carefully, cite relevant patterns (CQRS, event sourcing, saga, etc.), ` +
		`and always ground recommendations in operational experience. You write clear, ` +
		`idiomatic Go code and favour composition over inheritance. You are familiar with ` +
		`Kubernetes, Kafka, gRPC, PostgreSQL, Redis, and modern observability stacks. `

	// Repeat ~80× to comfortably exceed 2048 tokens (~2500 tokens total)
	return strings.Repeat(paragraph, 80)
}

// drainCacheStream consumes all events from stream and returns the done-event usage.
// It fails the test immediately on any StreamEventError.
func drainCacheStream(t *testing.T, stream <-chan llm.StreamEvent) *llm.Usage {
	t.Helper()
	var usage *llm.Usage
	for ev := range stream {
		switch ev.Type {
		case llm.StreamEventError:
			t.Fatalf("stream error: %v", ev.Error)
		case llm.StreamEventDone:
			usage = ev.Usage
		}
	}
	return usage
}

// envOrEmpty returns the value of an environment variable or empty string.
func envOrEmpty(key string) string {
	return os.Getenv(key)
}

// isAnthropicDirectAvailable checks if a direct Anthropic API key is available.
func isAnthropicDirectAvailable() bool {
	return envOrEmpty("ANTHROPIC_API_KEY") != ""
}

// --- Claude OAuth provider ---

func TestCacheIntegration_Claude_TopLevel(t *testing.T) {
	if !isClaudeAvailable() {
		t.Skip("requires local Claude credentials (~/.claude/.credentials.json)")
	}

	ctx := context.Background()
	p := claude.New()
	model := "claude-haiku-4-5-20251001" // cheapest model that supports caching

	opts := llm.StreamRequest{
		Model: model,
		Messages: llm.Messages{
			&llm.SystemMsg{Content: largeCacheablePrompt()},
			&llm.UserMsg{Content: "Summarise your role in one sentence."},
		},
		CacheHint: &llm.CacheHint{Enabled: true},
	}

	// Call 1: may write (cold) or read (warm if run recently)
	t.Log("Call 1...")
	stream1, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	usage1 := drainCacheStream(t, stream1)

	require.NotNil(t, usage1, "expected usage on first call")
	t.Logf("Call 1 — input: %d, write: %d, read: %d, cost: $%.6f",
		usage1.InputTokens, usage1.CacheWriteTokens, usage1.CacheReadTokens, usage1.Cost)

	assert.True(t, usage1.CacheWriteTokens > 0 || usage1.CacheReadTokens > 0,
		"first call should involve cache (write on cold start, read if cache is warm)")

	// Call 2: same prefix, should always hit cache
	t.Log("Call 2: expecting cache read...")
	stream2, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	usage2 := drainCacheStream(t, stream2)

	require.NotNil(t, usage2, "expected usage on second call")
	t.Logf("Call 2 — input: %d, write: %d, read: %d, cost: $%.6f",
		usage2.InputTokens, usage2.CacheWriteTokens, usage2.CacheReadTokens, usage2.Cost)

	assert.Greater(t, usage2.CacheReadTokens, 0,
		"second call should always serve tokens from cache")
}

func TestCacheIntegration_Claude_PerMessageHint(t *testing.T) {
	if !isClaudeAvailable() {
		t.Skip("requires local Claude credentials (~/.claude/.credentials.json)")
	}

	ctx := context.Background()
	p := claude.New()
	model := "claude-haiku-4-5-20251001"

	makeOpts := func(question string) llm.StreamRequest {
		return llm.StreamRequest{
			Model: model,
			Messages: llm.Messages{
				// Explicit breakpoint on the system message only — user question varies
				&llm.SystemMsg{
					Content:   largeCacheablePrompt(),
					CacheHint: &llm.CacheHint{Enabled: true},
				},
				&llm.UserMsg{Content: question},
			},
		}
	}

	// Call 1: write system prompt to cache (or read if already warm)
	stream1, err := p.CreateStream(ctx, makeOpts("What is a distributed system?"))
	require.NoError(t, err)
	usage1 := drainCacheStream(t, stream1)
	require.NotNil(t, usage1)
	t.Logf("Call 1 — write: %d, read: %d", usage1.CacheWriteTokens, usage1.CacheReadTokens)
	assert.True(t, usage1.CacheWriteTokens > 0 || usage1.CacheReadTokens > 0,
		"first call should involve cache (write on cold start, read if cache already warm)")

	// Call 2: different user question but same system prefix — must read from cache
	stream2, err := p.CreateStream(ctx, makeOpts("What is event sourcing?"))
	require.NoError(t, err)
	usage2 := drainCacheStream(t, stream2)
	require.NotNil(t, usage2)
	t.Logf("Call 2 — write: %d, read: %d", usage2.CacheWriteTokens, usage2.CacheReadTokens)
	assert.Greater(t, usage2.CacheReadTokens, 0,
		"second call with same system prefix should read from cache despite different user question")
}

func TestCacheIntegration_Claude_ExtendedTTL(t *testing.T) {
	if !isClaudeAvailable() {
		t.Skip("requires local Claude credentials (~/.claude/.credentials.json)")
	}

	ctx := context.Background()
	p := claude.New()

	// claude-haiku-4-5 supports 1h TTL
	opts := llm.StreamRequest{
		Model: "claude-haiku-4-5-20251001",
		Messages: llm.Messages{
			&llm.SystemMsg{Content: largeCacheablePrompt()},
			&llm.UserMsg{Content: "Hello"},
		},
		CacheHint: &llm.CacheHint{Enabled: true, TTL: "1h"},
	}

	stream, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	usage := drainCacheStream(t, stream)

	require.NotNil(t, usage)
	t.Logf("Extended TTL — write: %d, read: %d", usage.CacheWriteTokens, usage.CacheReadTokens)

	// Either a write (first call) or a read (repeated run) is acceptable;
	// the key assertion is: no error, and cache was involved.
	assert.True(t, usage.CacheWriteTokens > 0 || usage.CacheReadTokens > 0,
		"extended TTL request should produce either cache write or read tokens")
}

// --- Anthropic direct (API key) provider ---

func TestCacheIntegration_AnthropicDirect_TopLevel(t *testing.T) {
	if !isAnthropicDirectAvailable() {
		t.Skip("requires ANTHROPIC_API_KEY environment variable")
	}

	ctx := context.Background()
	p := anthropicdirect.New()

	opts := llm.StreamRequest{
		Model: "claude-haiku-4-5-20251001",
		Messages: llm.Messages{
			&llm.SystemMsg{Content: largeCacheablePrompt()},
			&llm.UserMsg{Content: "Summarise your role in one sentence."},
		},
		CacheHint: &llm.CacheHint{Enabled: true},
	}

	// Call 1: write to cache
	stream1, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	usage1 := drainCacheStream(t, stream1)
	require.NotNil(t, usage1)
	t.Logf("Call 1 — write: %d, read: %d, cost: $%.6f",
		usage1.CacheWriteTokens, usage1.CacheReadTokens, usage1.Cost)
	assert.True(t, usage1.CacheWriteTokens > 0 || usage1.CacheReadTokens > 0,
		"first call should involve cache (write on cold start, read if cache already warm)")

	// Call 2: read from cache
	stream2, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	usage2 := drainCacheStream(t, stream2)
	require.NotNil(t, usage2)
	t.Logf("Call 2 — write: %d, read: %d, cost: $%.6f",
		usage2.CacheWriteTokens, usage2.CacheReadTokens, usage2.Cost)
	assert.Greater(t, usage2.CacheReadTokens, 0, "second call should serve tokens from cache")
}

// --- Bedrock provider ---

func TestCacheIntegration_Bedrock_TopLevel(t *testing.T) {
	if !isBedrockAvailable() {
		t.Skip("requires AWS credentials (AWS_ACCESS_KEY_ID or ~/.aws/credentials)")
	}

	ctx := context.Background()
	p := bedrock.New(bedrock.WithRegion(getAWSRegion()))
	model := bedrock.ModelHaikuLatest

	opts := llm.StreamRequest{
		Model: model,
		Messages: llm.Messages{
			&llm.SystemMsg{Content: largeCacheablePrompt()},
			&llm.UserMsg{Content: "Summarise your role in one sentence."},
		},
		CacheHint: &llm.CacheHint{Enabled: true},
	}

	// Call 1: may write (cold) or read (warm if run recently)
	t.Log("Call 1...")
	stream1, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	usage1 := drainCacheStream(t, stream1)
	require.NotNil(t, usage1)
	t.Logf("Call 1 — input: %d, write: %d, read: %d, cost: $%.6f",
		usage1.InputTokens, usage1.CacheWriteTokens, usage1.CacheReadTokens, usage1.Cost)
	assert.True(t, usage1.CacheWriteTokens > 0 || usage1.CacheReadTokens > 0,
		"first call should involve cache (write on cold start, read if cache already warm)")

	// Call 2: same prefix, must always hit cache
	t.Log("Call 2: expecting cache read...")
	stream2, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	usage2 := drainCacheStream(t, stream2)
	require.NotNil(t, usage2)
	t.Logf("Call 2 — input: %d, write: %d, read: %d, cost: $%.6f",
		usage2.InputTokens, usage2.CacheWriteTokens, usage2.CacheReadTokens, usage2.Cost)
	assert.Greater(t, usage2.CacheReadTokens, 0,
		"second call should serve tokens from cache")
}

func TestCacheIntegration_Bedrock_PerMessageHint(t *testing.T) {
	if !isBedrockAvailable() {
		t.Skip("requires AWS credentials (AWS_ACCESS_KEY_ID or ~/.aws/credentials)")
	}

	ctx := context.Background()
	p := bedrock.New(bedrock.WithRegion(getAWSRegion()))
	model := bedrock.ModelHaikuLatest

	makeOpts := func(question string) llm.StreamRequest {
		return llm.StreamRequest{
			Model: model,
			Messages: llm.Messages{
				&llm.SystemMsg{
					Content:   largeCacheablePrompt(),
					CacheHint: &llm.CacheHint{Enabled: true},
				},
				&llm.UserMsg{Content: question},
			},
		}
	}

	// Call 1: write system prompt to cache (or read if already warm)
	stream1, err := p.CreateStream(ctx, makeOpts("What is a distributed system?"))
	require.NoError(t, err)
	usage1 := drainCacheStream(t, stream1)
	require.NotNil(t, usage1)
	t.Logf("Call 1 — write: %d, read: %d", usage1.CacheWriteTokens, usage1.CacheReadTokens)
	assert.True(t, usage1.CacheWriteTokens > 0 || usage1.CacheReadTokens > 0,
		"first call should involve cache (write on cold start, read if cache already warm)")

	// Call 2: different user question, same system prefix — must read from cache
	stream2, err := p.CreateStream(ctx, makeOpts("What is the saga pattern?"))
	require.NoError(t, err)
	usage2 := drainCacheStream(t, stream2)
	require.NotNil(t, usage2)
	t.Logf("Call 2 — write: %d, read: %d", usage2.CacheWriteTokens, usage2.CacheReadTokens)
	assert.Greater(t, usage2.CacheReadTokens, 0,
		"second call should read system prompt from cache despite different user question")
}

func TestCacheIntegration_Bedrock_NonClaudeModel_NoError(t *testing.T) {
	if !isBedrockAvailable() {
		t.Skip("requires AWS credentials (AWS_ACCESS_KEY_ID or ~/.aws/credentials)")
	}

	// CacheHint on a non-Claude Bedrock model should be silently ignored —
	// no cachePoint injected, no API error, zero cache token counts.
	ctx := context.Background()
	p := bedrock.New(bedrock.WithRegion(getAWSRegion()))

	opts := llm.StreamRequest{
		Model: bedrock.ModelNovaMicro, // non-Claude model
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Hello"},
		},
		CacheHint: &llm.CacheHint{Enabled: true},
	}

	stream, err := p.CreateStream(ctx, opts)
	require.NoError(t, err, "CacheHint on non-Claude model must not cause an API error")

	usage := drainCacheStream(t, stream)
	require.NotNil(t, usage)
	assert.Equal(t, 0, usage.CacheReadTokens, "non-Claude model should have no cached read tokens")
	assert.Equal(t, 0, usage.CacheWriteTokens, "non-Claude model should have no cache write tokens")
}
