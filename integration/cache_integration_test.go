package integration

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	anthropicdirect "github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/usage"
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

// drainCacheStream consumes all events from stream and returns the done-event usage record.
// It fails the test immediately on any StreamEventError.
func drainCacheStream(t *testing.T, stream <-chan llm.Envelope) *usage.Record {
	t.Helper()
	var rec *usage.Record
	for ev := range stream {
		switch ev.Type {
		case llm.StreamEventError:
			if errEv, ok := ev.Data.(*llm.ErrorEvent); ok {
				t.Fatalf("stream error: %v", errEv.Error)
			}
		case llm.StreamEventCompleted:
			// completed event received, but usage comes via UsageUpdatedEvent
		case llm.StreamEventUsageUpdated:
			if usageEv, ok := ev.Data.(*llm.UsageUpdatedEvent); ok {
				r := usageEv.Record
				rec = &r
			}
		}
	}
	return rec
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

	opts := llm.Request{
		Messages: msg.BuildTranscript(
			msg.System(largeCacheablePrompt()),
			msg.User("Summarise your role in one sentence."),
		),
		CacheHint: &llm.CacheHint{Enabled: true},
	}

	// Call 1: may write (cold) or read (warm if run recently)
	t.Log("Call 1...")
	stream1, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	rec1 := drainCacheStream(t, stream1)

	require.NotNil(t, rec1, "expected usage on first call")
	t.Logf("Call 1 — input: %d, write: %d, read: %d, cost: $%.6f",
		rec1.Tokens.TotalInput(), rec1.Tokens.Count(usage.KindCacheWrite), rec1.Tokens.Count(usage.KindCacheRead), rec1.Cost.Total)

	assert.True(t, rec1.Tokens.Count(usage.KindCacheWrite) > 0 || rec1.Tokens.Count(usage.KindCacheRead) > 0,
		"first call should involve cache (write on cold start, read if cache is warm)")

	// Call 2: same prefix, should always hit cache
	t.Log("Call 2: expecting cache read...")
	stream2, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	rec2 := drainCacheStream(t, stream2)

	require.NotNil(t, rec2, "expected usage on second call")
	t.Logf("Call 2 — input: %d, write: %d, read: %d, cost: $%.6f",
		rec2.Tokens.TotalInput(), rec2.Tokens.Count(usage.KindCacheWrite), rec2.Tokens.Count(usage.KindCacheRead), rec2.Cost.Total)

	assert.Greater(t, rec2.Tokens.Count(usage.KindCacheRead), 0,
		"second call should always serve tokens from cache")
}

func TestCacheIntegration_Claude_PerMessageHint(t *testing.T) {
	if !isClaudeAvailable() {
		t.Skip("requires local Claude credentials (~/.claude/.credentials.json)")
	}

	ctx := context.Background()
	p := claude.New()

	makeOpts := func(question string) llm.Request {
		systemMsg := msg.System(largeCacheablePrompt()).Build()
		systemMsg.CacheHint = &llm.CacheHint{Enabled: true}
		return llm.Request{
			Messages: msg.BuildTranscript(
				systemMsg,
				msg.User(question),
			),
		}
	}

	// Call 1: write system prompt to cache (or read if already warm)
	stream1, err := p.CreateStream(ctx, makeOpts("What is a distributed system?"))
	require.NoError(t, err)
	rec1 := drainCacheStream(t, stream1)
	require.NotNil(t, rec1)
	t.Logf("Call 1 — write: %d, read: %d", rec1.Tokens.Count(usage.KindCacheWrite), rec1.Tokens.Count(usage.KindCacheRead))
	assert.True(t, rec1.Tokens.Count(usage.KindCacheWrite) > 0 || rec1.Tokens.Count(usage.KindCacheRead) > 0,
		"first call should involve cache (write on cold start, read if cache already warm)")

	// Call 2: different user question but same system prefix — must read from cache
	stream2, err := p.CreateStream(ctx, makeOpts("What is event sourcing?"))
	require.NoError(t, err)
	rec2 := drainCacheStream(t, stream2)
	require.NotNil(t, rec2)
	t.Logf("Call 2 — write: %d, read: %d", rec2.Tokens.Count(usage.KindCacheWrite), rec2.Tokens.Count(usage.KindCacheRead))
	assert.Greater(t, rec2.Tokens.Count(usage.KindCacheRead), 0,
		"second call with same system prefix should read from cache despite different user question")
}

func TestCacheIntegration_Claude_ExtendedTTL(t *testing.T) {
	if !isClaudeAvailable() {
		t.Skip("requires local Claude credentials (~/.claude/.credentials.json)")
	}

	ctx := context.Background()
	p := claude.New()

	// claude-haiku-4-5 supports 1h TTL
	opts := llm.Request{
		Messages: msg.BuildTranscript(
			msg.System(largeCacheablePrompt()),
			msg.User("Hello"),
		),
		CacheHint: &llm.CacheHint{Enabled: true, TTL: "1h"},
	}

	stream, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	rec := drainCacheStream(t, stream)

	require.NotNil(t, rec)
	t.Logf("Extended TTL — write: %d, read: %d", rec.Tokens.Count(usage.KindCacheWrite), rec.Tokens.Count(usage.KindCacheRead))

	// Either a write (first call) or a read (repeated run) is acceptable;
	// the key assertion is: no error, and cache was involved.
	assert.True(t, rec.Tokens.Count(usage.KindCacheWrite) > 0 || rec.Tokens.Count(usage.KindCacheRead) > 0,
		"extended TTL request should produce either cache write or read tokens")
}

// --- Anthropic direct (API key) provider ---

func TestCacheIntegration_AnthropicDirect_TopLevel(t *testing.T) {
	if !isAnthropicDirectAvailable() {
		t.Skip("requires ANTHROPIC_API_KEY environment variable")
	}

	ctx := context.Background()
	p := anthropicdirect.New()

	opts := llm.Request{
		Messages: msg.BuildTranscript(
			msg.System(largeCacheablePrompt()),
			msg.User("Summarise your role in one sentence."),
		),
		CacheHint: &llm.CacheHint{Enabled: true},
	}

	// Call 1: write to cache
	stream1, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	rec1 := drainCacheStream(t, stream1)
	require.NotNil(t, rec1)
	t.Logf("Call 1 — write: %d, read: %d, cost: $%.6f",
		rec1.Tokens.Count(usage.KindCacheWrite), rec1.Tokens.Count(usage.KindCacheRead), rec1.Cost.Total)
	assert.True(t, rec1.Tokens.Count(usage.KindCacheWrite) > 0 || rec1.Tokens.Count(usage.KindCacheRead) > 0,
		"first call should involve cache (write on cold start, read if cache already warm)")

	// Call 2: read from cache
	stream2, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	rec2 := drainCacheStream(t, stream2)
	require.NotNil(t, rec2)
	t.Logf("Call 2 — write: %d, read: %d, cost: $%.6f",
		rec2.Tokens.Count(usage.KindCacheWrite), rec2.Tokens.Count(usage.KindCacheRead), rec2.Cost.Total)
	assert.Greater(t, rec2.Tokens.Count(usage.KindCacheRead), 0, "second call should serve tokens from cache")
}

// --- Bedrock provider ---

func TestCacheIntegration_Bedrock_TopLevel(t *testing.T) {
	if !isBedrockAvailable() {
		t.Skip("requires AWS credentials (AWS_ACCESS_KEY_ID or ~/.aws/credentials)")
	}

	ctx := context.Background()
	p := bedrock.New(bedrock.WithRegion(getAWSRegion()))
	model := bedrock.ModelHaikuLatest

	opts := llm.Request{
		Model: model,
		Messages: msg.BuildTranscript(
			msg.System(largeCacheablePrompt()),
			msg.User("Summarise your role in one sentence."),
		),
		CacheHint: &llm.CacheHint{Enabled: true},
	}

	// Call 1: may write (cold) or read (warm if run recently)
	t.Log("Call 1...")
	stream1, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	rec1 := drainCacheStream(t, stream1)
	require.NotNil(t, rec1)
	t.Logf("Call 1 — input: %d, write: %d, read: %d, cost: $%.6f",
		rec1.Tokens.TotalInput(), rec1.Tokens.Count(usage.KindCacheWrite), rec1.Tokens.Count(usage.KindCacheRead), rec1.Cost.Total)
	assert.True(t, rec1.Tokens.Count(usage.KindCacheWrite) > 0 || rec1.Tokens.Count(usage.KindCacheRead) > 0,
		"first call should involve cache (write on cold start, read if cache is warm)")

	// Call 2: same prefix, must always hit cache
	t.Log("Call 2: expecting cache read...")
	stream2, err := p.CreateStream(ctx, opts)
	require.NoError(t, err)
	rec2 := drainCacheStream(t, stream2)
	require.NotNil(t, rec2)
	t.Logf("Call 2 — input: %d, write: %d, read: %d, cost: $%.6f",
		rec2.Tokens.TotalInput(), rec2.Tokens.Count(usage.KindCacheWrite), rec2.Tokens.Count(usage.KindCacheRead), rec2.Cost.Total)
	assert.Greater(t, rec2.Tokens.Count(usage.KindCacheRead), 0,
		"second call should serve tokens from cache")
}

func TestCacheIntegration_Bedrock_PerMessageHint(t *testing.T) {
	if !isBedrockAvailable() {
		t.Skip("requires AWS credentials (AWS_ACCESS_KEY_ID or ~/.aws/credentials)")
	}

	ctx := context.Background()
	p := bedrock.New(bedrock.WithRegion(getAWSRegion()))
	model := bedrock.ModelHaikuLatest

	makeOpts := func(question string) llm.Request {
		systemMsg := msg.System(largeCacheablePrompt()).Build()
		systemMsg.CacheHint = &llm.CacheHint{Enabled: true}
		return llm.Request{
			Model: model,
			Messages: msg.BuildTranscript(
				systemMsg,
				msg.User(question),
			),
		}
	}

	// Call 1: write system prompt to cache (or read if already warm)
	stream1, err := p.CreateStream(ctx, makeOpts("What is a distributed system?"))
	require.NoError(t, err)
	rec1 := drainCacheStream(t, stream1)
	require.NotNil(t, rec1)
	t.Logf("Call 1 — write: %d, read: %d", rec1.Tokens.Count(usage.KindCacheWrite), rec1.Tokens.Count(usage.KindCacheRead))
	assert.True(t, rec1.Tokens.Count(usage.KindCacheWrite) > 0 || rec1.Tokens.Count(usage.KindCacheRead) > 0,
		"first call should involve cache (write on cold start, read if cache already warm)")

	// Call 2: different user question, same system prefix — must read from cache
	stream2, err := p.CreateStream(ctx, makeOpts("What is the saga pattern?"))
	require.NoError(t, err)
	rec2 := drainCacheStream(t, stream2)
	require.NotNil(t, rec2)
	t.Logf("Call 2 — write: %d, read: %d", rec2.Tokens.Count(usage.KindCacheWrite), rec2.Tokens.Count(usage.KindCacheRead))
	assert.Greater(t, rec2.Tokens.Count(usage.KindCacheRead), 0,
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

	opts := llm.Request{
		Model: bedrock.ModelNovaMicro, // non-Claude model
		Messages: msg.BuildTranscript(
			msg.User("Hello"),
		),
		CacheHint: &llm.CacheHint{Enabled: true},
	}

	stream, err := p.CreateStream(ctx, opts)
	require.NoError(t, err, "CacheHint on non-Claude model must not cause an API error")

	rec := drainCacheStream(t, stream)
	require.NotNil(t, rec)
	assert.Equal(t, 0, rec.Tokens.Count(usage.KindCacheRead), "non-Claude model should have no cached read tokens")
	assert.Equal(t, 0, rec.Tokens.Count(usage.KindCacheWrite), "non-Claude model should have no cache write tokens")
}
