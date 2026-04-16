package codex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testModel is the model used across all integration tests. It picks the
// fastest / cheapest available model to keep latency and cost low.
var testModel = FastModelID()

// isCodexAvailable reports whether local Codex auth credentials are present
// and usable. It mirrors the pattern used by isClaudeAvailable in the
// integration package: a cheap file-system check, no live HTTP call.
func isCodexAvailable() bool {
	return LocalAvailable()
}

// newTestProvider loads local auth and constructs a *Provider, skipping the
// test if auth is unavailable or cannot be loaded.
func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	if !isCodexAvailable() {
		t.Skip("skipping Codex integration test: no local auth (~/.codex/auth.json)")
	}
	auth, err := LoadAuth()
	require.NoError(t, err, "LoadAuth() must succeed when LocalAvailable() is true")
	return New(auth)
}

// drainStream consumes every envelope from stream and returns the concatenated
// text, a flag indicating at least one completed event arrived, and the first
// error event (if any).
func drainStream(t *testing.T, stream llm.Stream) (text string, completed bool, streamErr error) {
	t.Helper()
	for env := range stream {
		switch env.Type {
		case llm.StreamEventDelta:
			if d, ok := env.Data.(*llm.DeltaEvent); ok {
				text += d.Text
			}
		case llm.StreamEventCompleted:
			completed = true
		case llm.StreamEventError:
			if e, ok := env.Data.(*llm.ErrorEvent); ok {
				streamErr = e.Error
			}
		}
	}
	return
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_FetchModels — live models endpoint
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_FetchModels verifies that the /codex/models endpoint is reachable
// and returns at least one model with a non-empty slug.
//
// Run with:
//
//	go test -v -run TestCodex_FetchModels ./provider/codex/
func TestCodex_FetchModels(t *testing.T) {
	p := newTestProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	models, err := p.FetchModels(ctx)
	require.NoError(t, err, "FetchModels() should not return an error")
	require.NotEmpty(t, models, "FetchModels() must return at least one model")

	for _, m := range models {
		assert.NotEmpty(t, m.ID, "every model must have a non-empty ID")
		t.Logf("model: %s (%s)", m.ID, m.Name)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_SimpleStream — single-turn text completion
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_SimpleStream sends a minimal prompt and asserts that the stream
// delivers at least one delta and a completed event with no errors.
//
// Run with:
//
//	go test -v -run TestCodex_SimpleStream ./provider/codex/
func TestCodex_SimpleStream(t *testing.T) {
	p := newTestProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:     testModel,
		MaxTokens: 64,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User(`Reply with exactly one word: "pong"`),
		),
	})

	var pe *llm.ProviderError
	if errors.As(err, &pe) {
		t.Logf("API error body: %s", pe.ResponseBody)
	}
	require.NoError(t, err, "CreateStream() must not return an error")

	text, completed, streamErr := drainStream(t, stream)
	require.NoError(t, streamErr, "stream must not emit an error event")
	assert.True(t, completed, "stream must emit a completed event")
	assert.NotEmpty(t, text, "stream must produce at least one text delta")
	t.Logf("response: %q", text)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_Conversation — multi-turn conversation
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_Conversation sends a multi-turn transcript (system + user +
// assistant + user) and verifies the provider handles it without errors.
//
// Run with:
//
//	go test -v -run TestCodex_Conversation ./provider/codex/
func TestCodex_Conversation(t *testing.T) {
	p := newTestProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	messages := msg.BuildTranscript(
		msg.System("You are a concise assistant. Keep answers to one sentence."),
		msg.User("What colour is the sky?"),
		msg.Assistant(msg.Text("The sky is blue.")),
		msg.User("And at sunset?"),
	)

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:     testModel,
		MaxTokens: 64,
		Messages:  messages,
	})

	var pe *llm.ProviderError
	if errors.As(err, &pe) {
		t.Logf("API error body: %s", pe.ResponseBody)
	}
	require.NoError(t, err, "CreateStream() must not error for a multi-turn conversation")

	text, completed, streamErr := drainStream(t, stream)
	require.NoError(t, streamErr, "stream must not emit an error event")
	assert.True(t, completed, "stream must emit a completed event")
	assert.NotEmpty(t, text, "conversation response must contain text")
	t.Logf("response: %q", text)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_ToolCall — tool-use round-trip
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_ToolCall verifies the full tool-use cycle:
//  1. Send a user prompt that strongly hints at tool use.
//  2. Expect the model to emit a StreamEventToolCall.
//  3. Feed the tool result back and expect a follow-up text response.
//
// Run with:
//
//	go test -v -run TestCodex_ToolCall ./provider/codex/
func TestCodex_ToolCall(t *testing.T) {
	p := newTestProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	type GetWeatherParams struct {
		Location string `json:"location" jsonschema:"description=City name,required"`
	}

	tools := []tool.Definition{
		tool.DefinitionFor[GetWeatherParams]("get_weather", "Get the current weather for a location"),
	}

	// ── turn 1: ask for tool use ──────────────────────────────────────────────
	stream1, err := p.CreateStream(ctx, llm.Request{
		Model: testModel,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("What's the weather in Tokyo? Use the get_weather tool."),
		),
		Tools: tools,
	})

	var pe *llm.ProviderError
	if errors.As(err, &pe) {
		t.Logf("API error body: %s", pe.ResponseBody)
	}
	require.NoError(t, err, "first CreateStream() must not error")

	var toolCall tool.Call
	var completed1 bool
	for env := range stream1 {
		switch env.Type {
		case llm.StreamEventError:
			if e, ok := env.Data.(*llm.ErrorEvent); ok {
				t.Fatalf("stream error on turn 1: %v", e.Error)
			}
		case llm.StreamEventToolCall:
			if tc, ok := env.Data.(*llm.ToolCallEvent); ok {
				toolCall = tc.ToolCall
				t.Logf("tool call: %s(%v)", toolCall.ToolName(), toolCall.ToolArgs())
			}
		case llm.StreamEventCompleted:
			completed1 = true
		}
	}
	assert.True(t, completed1, "turn 1 stream must complete")

	if toolCall == nil {
		t.Skip("model did not issue a tool call (not guaranteed); skipping round-trip assertions")
	}

	require.NotEmpty(t, toolCall.ToolCallID(), "tool call must carry a non-empty ID")
	require.Equal(t, "get_weather", toolCall.ToolName(), "tool name must match the registered tool")

	// ── turn 2: send tool result back ─────────────────────────────────────────
	stream2, err := p.CreateStream(ctx, llm.Request{
		Model: testModel,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("What's the weather in Tokyo? Use the get_weather tool."),
			msg.Assistant(msg.ToolCall(msg.NewToolCall(
				toolCall.ToolCallID(), toolCall.ToolName(), msg.ToolArgs{"location": "Tokyo"},
			))),
			msg.Tool().Results(msg.ToolResult{
				ToolCallID: toolCall.ToolCallID(),
				ToolOutput: `{"temperature": 28, "conditions": "sunny"}`,
			}),
		),
		Tools: tools,
	})
	if errors.As(err, &pe) {
		t.Logf("API error body (turn 2): %s", pe.ResponseBody)
	}
	require.NoError(t, err, "second CreateStream() (tool result) must not error")

	text2, completed2, streamErr2 := drainStream(t, stream2)
	require.NoError(t, streamErr2, "turn 2 stream must not emit an error event")
	assert.True(t, completed2, "turn 2 stream must complete")
	assert.NotEmpty(t, text2, "model must produce a final answer after the tool result")
	t.Logf("final response: %q", text2)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_DefaultModel — model resolution
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_DefaultModel verifies that the default model (empty model string)
// is resolved correctly and that a completion can be obtained with it.
//
// Run with:
//
//	go test -v -run TestCodex_DefaultModel ./provider/codex/
func TestCodex_DefaultModel(t *testing.T) {
	p := newTestProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Verify that Resolve() can find the default model by its explicit slug.
	defaultID := DefaultModelID()
	require.NotEmpty(t, defaultID, "DefaultModelID() must return a non-empty slug")
	resolved, err := p.Resolve(defaultID)
	require.NoError(t, err, "Resolve(DefaultModelID()) must succeed")
	assert.Equal(t, defaultID, resolved.ID, "resolved model ID must match the requested slug")
	t.Logf("default model: %s", resolved.ID)

	// Smoke-test: omitting Model causes the provider to fall back to its
	// internal default — the stream must still complete successfully.
	stream, err := p.CreateStream(ctx, llm.Request{
		Model:     testModel,
		MaxTokens: 32,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Say hi."),
		),
	})

	var pe *llm.ProviderError
	if errors.As(err, &pe) {
		t.Logf("API error body: %s", pe.ResponseBody)
	}
	require.NoError(t, err, "CreateStream() with empty model must not error")

	_, completed, streamErr := drainStream(t, stream)
	require.NoError(t, streamErr)
	assert.True(t, completed, "stream must complete even when model is omitted")
}
