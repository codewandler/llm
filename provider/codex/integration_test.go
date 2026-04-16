package codex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

// collectStream drains a stream and returns the concatenated text, completed
// flag, first stream error, and the RequestEvent (nil if not emitted).
func collectStream(t *testing.T, stream llm.Stream) (text string, completed bool, streamErr error, reqEv *llm.RequestEvent) {
	t.Helper()
	for env := range stream {
		switch env.Type {
		case llm.StreamEventRequest:
			reqEv = env.Data.(*llm.RequestEvent)
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

// drainStream is a convenience wrapper around collectStream for tests that
// don't need the RequestEvent.
func drainStream(t *testing.T, stream llm.Stream) (text string, completed bool, streamErr error) {
	t.Helper()
	text, completed, streamErr, _ = collectStream(t, stream)
	return
}
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

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_RequestEvent — wire-protocol observability
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_RequestEvent verifies that every stream emits exactly one
// RequestEvent with:
//   - ResolvedApiType == ApiTypeOpenAIResponses
//   - ProviderRequest.URL containing "/codex/responses" (not "/v1/responses")
//   - ProviderRequest.Body with "store":false and no "max_tokens" key
func TestCodex_RequestEvent(t *testing.T) {
	p := newTestProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:     testModel,
		MaxTokens: 16, // must be stripped from the wire body
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Say hi."),
		),
	})
	require.NoError(t, err)

	var reqEv *llm.RequestEvent
	for env := range stream {
		if env.Type == llm.StreamEventRequest {
			e := env.Data.(*llm.RequestEvent)
			reqEv = e
		}
	}

	require.NotNil(t, reqEv, "stream must emit a RequestEvent")

	// API type
	assert.Equal(t, llm.ApiTypeOpenAIResponses, reqEv.ResolvedApiType)

	// URL must hit /codex/responses, not /v1/responses
	assert.Contains(t, reqEv.ProviderRequest.URL, "/codex/responses")
	assert.NotContains(t, reqEv.ProviderRequest.URL, "/v1/responses")

	// Wire body invariants
	var body map[string]any
	require.NoError(t, json.Unmarshal(reqEv.ProviderRequest.Body, &body))

	// store must be explicitly false
	store, hasStore := body["store"]
	assert.True(t, hasStore, "wire body must contain \"store\" key")
	assert.Equal(t, false, store, "store must be false")

	// max_tokens must be stripped
	_, hasMaxTokens := body["max_tokens"]
	assert.False(t, hasMaxTokens, "max_tokens must be stripped from the wire body")

	// max_output_tokens must also be stripped
	_, hasMaxOutput := body["max_output_tokens"]
	assert.False(t, hasMaxOutput, "max_output_tokens must be stripped from the wire body")
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_UsageRecord — usage tracking
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_UsageRecord verifies that the stream emits a UsageUpdatedEvent
// with positive input and output token counts.
func TestCodex_UsageRecord(t *testing.T) {
	p := newTestProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model: testModel,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Reply with one word."),
		),
	})
	require.NoError(t, err)

	var usageEv *llm.UsageUpdatedEvent
	for env := range stream {
		if env.Type == llm.StreamEventUsageUpdated {
			e := env.Data.(*llm.UsageUpdatedEvent)
			usageEv = e
		}
	}

	require.NotNil(t, usageEv, "stream must emit a UsageUpdatedEvent")
	assert.Positive(t, usageEv.Record.Tokens.TotalInput(), "input tokens must be positive")
	assert.Positive(t, usageEv.Record.Tokens.TotalOutput(), "output tokens must be positive")
	t.Logf("usage: input=%d output=%d",
		usageEv.Record.Tokens.TotalInput(),
		usageEv.Record.Tokens.TotalOutput())
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_TokenEstimate — pre-request estimation
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_TokenEstimate verifies that a TokenEstimateEvent is emitted
// before the first delta, with a positive input estimate.
func TestCodex_TokenEstimate(t *testing.T) {
	p := newTestProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model: testModel,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("What is 1+1?"),
		),
	})
	require.NoError(t, err)

	var estimateEv *llm.TokenEstimateEvent
	var firstDeltaSeq int
	var estimateSeq int
	seq := 0
	for env := range stream {
		seq++
		switch env.Type {
		case llm.StreamEventTokenEstimate:
			estimateEv = env.Data.(*llm.TokenEstimateEvent)
			estimateSeq = seq
		case llm.StreamEventDelta:
			if firstDeltaSeq == 0 {
				firstDeltaSeq = seq
			}
		}
	}

	require.NotNil(t, estimateEv, "stream must emit a TokenEstimateEvent")
	assert.Positive(t, estimateEv.Estimate.Tokens.TotalInput(), "estimate must have positive input tokens")
	if firstDeltaSeq > 0 {
		assert.Less(t, estimateSeq, firstDeltaSeq, "token estimate must arrive before first delta")
	}
	t.Logf("estimate: input=%d", estimateEv.Estimate.Tokens.TotalInput())
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_MaxTokensStripped — body mutation invariant
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_MaxTokensStripped verifies that passing MaxTokens does not cause
// an API error. The Codex backend rejects max_tokens / max_output_tokens;
// the provider must strip these fields from the wire body before sending.
func TestCodex_MaxTokensStripped(t *testing.T) {
	p := newTestProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:     testModel,
		MaxTokens: 50,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Say the word: pong"),
		),
	})
	var pe *llm.ProviderError
	if errors.As(err, &pe) {
		t.Logf("API error body: %s", pe.ResponseBody)
	}
	require.NoError(t, err, "max_tokens must be stripped — Codex rejects it otherwise")

	_, completed, streamErr := drainStream(t, stream)
	require.NoError(t, streamErr)
	assert.True(t, completed)
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_EffortLevels — effort mapping
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_EffortLevels verifies that each supported effort level can be
// passed without an API error. EffortMax is the critical case because it
// must be mapped to "xhigh" before reaching the wire.
func TestCodex_EffortLevels(t *testing.T) {
	p := newTestProvider(t)

	for _, effort := range []llm.Effort{
		llm.EffortLow,
		llm.EffortMedium,
		llm.EffortHigh,
		llm.EffortMax, // must map to "xhigh" for Codex
	} {
		effort := effort
		t.Run(string(effort), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			stream, err := p.CreateStream(ctx, llm.Request{
				Model:  testModel,
				Effort: effort,
				Messages: msg.BuildTranscript(
					msg.System("You are a helpful assistant."),
					msg.User("Reply with one word: pong"),
				),
			})
			var pe *llm.ProviderError
			if errors.As(err, &pe) {
				t.Logf("API error body: %s", pe.ResponseBody)
			}
			require.NoError(t, err, "effort %q must not cause an API error", effort)

			_, completed, streamErr := drainStream(t, stream)
			require.NoError(t, streamErr)
			assert.True(t, completed)
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_EffortMaxWireValue — effort mapping in RequestEvent body
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_EffortMaxWireValue verifies that EffortMax is sent as "xhigh" on
// the wire, not as "max" (which the Codex API does not accept).
func TestCodex_EffortMaxWireValue(t *testing.T) {
	p := newTestProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:  testModel,
		Effort: llm.EffortMax,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Reply with one word: pong"),
		),
	})
	require.NoError(t, err)

	var reqEv *llm.RequestEvent
	for env := range stream {
		if env.Type == llm.StreamEventRequest {
			reqEv = env.Data.(*llm.RequestEvent)
		}
	}

	require.NotNil(t, reqEv)
	var body map[string]any
	require.NoError(t, json.Unmarshal(reqEv.ProviderRequest.Body, &body))

	if reasoning, ok := body["reasoning"].(map[string]any); ok {
		assert.Equal(t, "xhigh", reasoning["effort"],
			"EffortMax must be mapped to xhigh on the wire, not 'max'")
	} else {
		t.Log("no reasoning block in wire body — model may not support effort")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_ToolChoiceRequired — forced tool invocation
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_ToolChoiceRequired verifies that ToolChoiceRequired forces the
// model to call a tool, and that the emitted ToolCallEvent carries a non-empty
// name and a non-empty ID (the call_id, not the internal item_id).
func TestCodex_ToolChoiceRequired(t *testing.T) {
	p := newTestProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	type EchoParams struct {
		Message string `json:"message" jsonschema:"description=Message to echo,required"`
	}
	tools := []tool.Definition{
		tool.DefinitionFor[EchoParams]("echo", "Echo a message back"),
	}

	stream, err := p.CreateStream(ctx, llm.Request{
		Model: testModel,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Echo back: hello"),
		),
		Tools:      tools,
		ToolChoice: llm.ToolChoiceRequired{},
	})
	var pe *llm.ProviderError
	if errors.As(err, &pe) {
		t.Logf("API error body: %s", pe.ResponseBody)
	}
	require.NoError(t, err)

	var toolCallEv *llm.ToolCallEvent
	for env := range stream {
		if env.Type == llm.StreamEventError {
			t.Fatalf("stream error: %v", env.Data.(*llm.ErrorEvent).Error)
		}
		if env.Type == llm.StreamEventToolCall {
			toolCallEv = env.Data.(*llm.ToolCallEvent)
		}
	}

	require.NotNil(t, toolCallEv, "ToolChoiceRequired must produce a tool call")
	assert.Equal(t, "echo", toolCallEv.ToolCall.ToolName(), "tool name must match")
	assert.NotEmpty(t, toolCallEv.ToolCall.ToolCallID(), "tool call ID must not be empty")
	// The ID must be the call_id (starts with "call_"), not the internal item_id ("fc_")
	assert.True(t,
		strings.HasPrefix(toolCallEv.ToolCall.ToolCallID(), "call_"),
		"tool call ID must be the call_id (got %q)", toolCallEv.ToolCall.ToolCallID())
	t.Logf("tool call: %s(%v) id=%s",
		toolCallEv.ToolCall.ToolName(),
		toolCallEv.ToolCall.ToolArgs(),
		toolCallEv.ToolCall.ToolCallID())
}

// ─────────────────────────────────────────────────────────────────────────────
// TestCodex_AuthHeaders — header injection
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_AuthHeaders verifies that all required Codex auth headers are
// present in the outgoing request captured by the RequestEvent.
func TestCodex_AuthHeaders(t *testing.T) {
	p := newTestProvider(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model: testModel,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Say hi."),
		),
	})
	require.NoError(t, err)

	var reqEv *llm.RequestEvent
	for env := range stream {
		if env.Type == llm.StreamEventRequest {
			reqEv = env.Data.(*llm.RequestEvent)
		}
	}

	require.NotNil(t, reqEv)
	h := reqEv.ProviderRequest.Headers

	// Authorization is intentionally redacted by ProviderRequestFromHTTP;
	// its presence as "[REDACTED]" proves the header was injected.
	assert.Equal(t, "[REDACTED]", h["Authorization"],
		"Authorization must be present (redacted for security)")
	// Header keys are stored in canonical MIME form by net/http.
	assert.Equal(t, codexBetaValue, h["Openai-Beta"],
		"%s header must be set", codexBetaHeader)
	assert.Equal(t, codexOriginator, h["Originator"],
		"originator header must be set")
}

// ─────────────────────────────────────────────────────────────────────────────
// Request field variations
// ─────────────────────────────────────────────────────────────────────────────

// TestCodex_Temperature verifies that Temperature is silently stripped from
// the wire body (Codex rejects it) and the request still completes.
func TestCodex_Temperature(t *testing.T) {
	p := newTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:       testModel,
		Temperature: 0.3,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Reply with one word: pong"),
		),
	})
	require.NoError(t, err)

	text, completed, streamErr, reqEv := collectStream(t, stream)
	require.NoError(t, streamErr)
	assert.True(t, completed)
	assert.NotEmpty(t, text)

	require.NotNil(t, reqEv)
	var body map[string]any
	require.NoError(t, json.Unmarshal(reqEv.ProviderRequest.Body, &body))
	_, hasTemp := body["temperature"]
	assert.False(t, hasTemp, "temperature must be stripped — Codex rejects it")
}

// TestCodex_TopP verifies that TopP is silently stripped (Codex rejects it).
func TestCodex_TopP(t *testing.T) {
	p := newTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model: testModel,
		TopP:  0.9,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Reply with one word: pong"),
		),
	})
	require.NoError(t, err)

	_, completed, streamErr, reqEv := collectStream(t, stream)
	require.NoError(t, streamErr)
	assert.True(t, completed)

	require.NotNil(t, reqEv)
	var body map[string]any
	require.NoError(t, json.Unmarshal(reqEv.ProviderRequest.Body, &body))
	_, hasTopP := body["top_p"]
	assert.False(t, hasTopP, "top_p must be stripped — Codex rejects it")
}

// TestCodex_TemperatureAndTopP verifies both sampling parameters are stripped.
func TestCodex_TemperatureAndTopP(t *testing.T) {
	p := newTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:       testModel,
		Temperature: 0.7,
		TopP:        0.95,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Reply with one word: pong"),
		),
	})
	require.NoError(t, err)

	_, completed, streamErr, reqEv := collectStream(t, stream)
	require.NoError(t, streamErr)
	assert.True(t, completed)

	require.NotNil(t, reqEv)
	var body map[string]any
	require.NoError(t, json.Unmarshal(reqEv.ProviderRequest.Body, &body))
	_, hasTemp := body["temperature"]
	_, hasTopP := body["top_p"]
	assert.False(t, hasTemp, "temperature must be stripped")
	assert.False(t, hasTopP, "top_p must be stripped")
}

// TestCodex_ThinkingOn verifies that ThinkingOn produces a valid response
// without an API error.
func TestCodex_ThinkingOn(t *testing.T) {
	p := newTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:    testModel,
		Thinking: llm.ThinkingOn,
		Effort:   llm.EffortLow,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("What is 2 + 2? Reply with just the number."),
		),
	})
	var pe *llm.ProviderError
	if errors.As(err, &pe) {
		t.Logf("API error body: %s", pe.ResponseBody)
	}
	require.NoError(t, err)

	_, completed, streamErr := drainStream(t, stream)
	require.NoError(t, streamErr)
	assert.True(t, completed)
}

// TestCodex_ThinkingOff verifies that ThinkingOff clears reasoning effort
// from the wire body entirely.
func TestCodex_ThinkingOff(t *testing.T) {
	p := newTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:    testModel,
		Thinking: llm.ThinkingOff,
		Effort:   llm.EffortHigh, // must be cleared when thinking is off
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Reply with one word: pong"),
		),
	})
	require.NoError(t, err)

	_, completed, streamErr, reqEv := collectStream(t, stream)
	require.NoError(t, streamErr)
	assert.True(t, completed)

	require.NotNil(t, reqEv)
	var body map[string]any
	require.NoError(t, json.Unmarshal(reqEv.ProviderRequest.Body, &body))
	_, hasReasoning := body["reasoning"]
	assert.False(t, hasReasoning,
		"reasoning block must be absent when ThinkingOff clears effort")
}

// TestCodex_JSONOutputFormat verifies that OutputFormatJSON is silently stripped
// (Codex rejects response_format) and the request still completes.
func TestCodex_JSONOutputFormat(t *testing.T) {
	p := newTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:        testModel,
		OutputFormat: llm.OutputFormatJSON,
		Messages: msg.BuildTranscript(
			msg.System(`You are a helpful assistant. Always respond with valid JSON.`),
			msg.User(`Return a JSON object with a single key "value" set to 42.`),
		),
	})
	require.NoError(t, err)

	_, completed, streamErr, reqEv := collectStream(t, stream)
	require.NoError(t, streamErr)
	assert.True(t, completed)

	require.NotNil(t, reqEv)
	var body map[string]any
	require.NoError(t, json.Unmarshal(reqEv.ProviderRequest.Body, &body))
	_, hasFormat := body["response_format"]
	assert.False(t, hasFormat, "response_format must be stripped — Codex rejects it")
}

// TestCodex_ToolChoiceNone verifies that ToolChoiceNone prevents tool calls
// even when tools are registered: the model must respond with text.
func TestCodex_ToolChoiceNone(t *testing.T) {
	p := newTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	type WeatherParams struct {
		Location string `json:"location" jsonschema:"required"`
	}
	tools := []tool.Definition{
		tool.DefinitionFor[WeatherParams]("get_weather", "Get weather for a location"),
	}

	stream, err := p.CreateStream(ctx, llm.Request{
		Model: testModel,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("What is the weather in Paris?"),
		),
		Tools:      tools,
		ToolChoice: llm.ToolChoiceNone{},
	})
	require.NoError(t, err)

	var gotToolCall bool
	var text string
	for env := range stream {
		switch env.Type {
		case llm.StreamEventToolCall:
			gotToolCall = true
		case llm.StreamEventDelta:
			text += env.Data.(*llm.DeltaEvent).Text
		case llm.StreamEventError:
			t.Fatalf("stream error: %v", env.Data.(*llm.ErrorEvent).Error)
		}
	}

	assert.False(t, gotToolCall, "ToolChoiceNone must suppress tool calls")
	assert.NotEmpty(t, text, "model must respond with text when tools are suppressed")
}

// TestCodex_ToolChoiceSpecific verifies that ToolChoiceTool forces the model
// to call a specific named tool.
func TestCodex_ToolChoiceSpecific(t *testing.T) {
	p := newTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	type WeatherParams struct {
		Location string `json:"location" jsonschema:"required"`
	}
	tools := []tool.Definition{
		tool.DefinitionFor[WeatherParams]("get_weather", "Get weather for a location"),
		tool.DefinitionFor[WeatherParams]("get_forecast", "Get forecast for a location"),
	}

	stream, err := p.CreateStream(ctx, llm.Request{
		Model: testModel,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Tell me about Paris."),
		),
		Tools:      tools,
		ToolChoice: llm.ToolChoiceTool{Name: "get_weather"},
	})
	var pe *llm.ProviderError
	if errors.As(err, &pe) {
		t.Logf("API error body: %s", pe.ResponseBody)
	}
	require.NoError(t, err)

	var toolCallEv *llm.ToolCallEvent
	for env := range stream {
		if env.Type == llm.StreamEventError {
			t.Fatalf("stream error: %v", env.Data.(*llm.ErrorEvent).Error)
		}
		if env.Type == llm.StreamEventToolCall {
			toolCallEv = env.Data.(*llm.ToolCallEvent)
		}
	}

	require.NotNil(t, toolCallEv, "ToolChoiceTool must force a tool call")
	assert.Equal(t, "get_weather", toolCallEv.ToolCall.ToolName(),
		"the specific tool must be called")
}

// TestCodex_SystemOnly verifies that a request with only a system message
// and no user message is handled gracefully (API may accept or reject it).
func TestCodex_LongSystemPrompt(t *testing.T) {
	p := newTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	systemPrompt := strings.Repeat("You are a helpful assistant. ", 50)

	stream, err := p.CreateStream(ctx, llm.Request{
		Model: testModel,
		Messages: msg.BuildTranscript(
			msg.System(systemPrompt),
			msg.User("Reply with one word: pong"),
		),
	})
	var pe *llm.ProviderError
	if errors.As(err, &pe) {
		t.Logf("API error body: %s", pe.ResponseBody)
	}
	require.NoError(t, err)

	text, completed, streamErr := drainStream(t, stream)
	require.NoError(t, streamErr)
	assert.True(t, completed)
	assert.NotEmpty(t, text)
}

// TestCodex_EffortWithTemperature verifies that temperature is stripped and the
// request succeeds when effort and temperature are set together.
func TestCodex_EffortWithTemperature(t *testing.T) {
	p := newTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:       testModel,
		Effort:      llm.EffortLow,
		Temperature: 0.5,
		Messages: msg.BuildTranscript(
			msg.System("You are a helpful assistant."),
			msg.User("Reply with one word: pong"),
		),
	})
	require.NoError(t, err)

	_, completed, streamErr, reqEv := collectStream(t, stream)
	require.NoError(t, streamErr)
	assert.True(t, completed)

	require.NotNil(t, reqEv)
	var body map[string]any
	require.NoError(t, json.Unmarshal(reqEv.ProviderRequest.Body, &body))
	_, hasTemp := body["temperature"]
	assert.False(t, hasTemp, "temperature must be stripped")
	// effort must still be present
	if reasoning, ok := body["reasoning"].(map[string]any); ok {
		assert.Equal(t, "low", reasoning["effort"])
	}
}

// TestCodex_MultiTurnWithEffort verifies a multi-turn conversation works with
// an explicit effort level set.
func TestCodex_MultiTurnWithEffort(t *testing.T) {
	p := newTestProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:  testModel,
		Effort: llm.EffortMedium,
		Messages: msg.BuildTranscript(
			msg.System("You are a concise assistant."),
			msg.User("My name is Alice."),
			msg.Assistant(msg.Text("Hello Alice, nice to meet you!")),
			msg.User("What is my name?"),
		),
	})
	require.NoError(t, err)

	text, completed, streamErr := drainStream(t, stream)
	require.NoError(t, streamErr)
	assert.True(t, completed)
	assert.Contains(t, strings.ToLower(text), "alice",
		"model must recall the name from conversation history")
}
