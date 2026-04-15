package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

func TestProvider_ResolveDefaultModel(t *testing.T) {
	p := New()
	m, err := p.Resolve(llm.ModelDefault)
	require.NoError(t, err)
	assert.Equal(t, "openrouter/auto", m.ID)
}

func TestProvider_ResolveAutoAlias(t *testing.T) {
	p := New()
	m, err := p.Resolve("auto")
	require.NoError(t, err)
	assert.Equal(t, "openrouter/auto", m.ID)
}

// TestProvider_CreateStream_DefaultModelApplied verifies that when no model is
// set the provider default ("openai/gpt-4o" here) is used in the wire request.
// openai/* routes to /v1/responses; the test server returns a minimal
// response.completed event so the stream terminates cleanly.
func TestProvider_CreateStream_DefaultModelApplied(t *testing.T) {
	var gotPath, gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		gotPath = r.URL.Path
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotModel, _ = body["model"].(string)

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"openai/gpt-4o\",\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()

	p := New(llm.WithBaseURL(server.URL), llm.WithAPIKey("test-key")).
		WithDefaultModel("openai/gpt-4o")

	stream, err := p.CreateStream(t.Context(), llm.Request{
		Messages: msg.BuildTranscript(msg.User("Hello")),
	})
	require.NoError(t, err)
	for range stream {
	}

	assert.Equal(t, "/v1/responses", gotPath, "openai/* must route to /v1/responses")
	assert.Equal(t, "openai/gpt-4o", gotModel)
}

// TestProvider_CreateStream_AnthropicRoutesToMessages verifies that
// anthropic/* models route to /v1/messages.
func TestProvider_CreateStream_AnthropicRoutesToMessages(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			"event: message_start\ndata: {\"message\":{\"id\":\"msg_1\",\"model\":\"anthropic/claude-opus-4-5\",\"usage\":{\"input_tokens\":5}}}\n\n"+
				"event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"+
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		)
	}))
	defer server.Close()

	p := New(llm.WithBaseURL(server.URL), llm.WithAPIKey("test-key"))
	stream, err := p.CreateStream(t.Context(), llm.Request{
		Model:    "anthropic/claude-opus-4-5",
		Messages: msg.BuildTranscript(msg.User("Hello")),
	})
	require.NoError(t, err)
	for range stream {
	}

	assert.Equal(t, "/v1/messages", gotPath, "anthropic/* must route to /v1/messages")
}

// TestProvider_CreateStream_UnknownPrefixRoutesToResponses verifies that
// unknown model prefixes (meta/, mistral/, etc.) route to /v1/responses.
func TestProvider_CreateStream_UnknownPrefixRoutesToResponses(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"response\":{\"id\":\"r1\",\"model\":\"meta/llama-4\",\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()

	p := New(llm.WithBaseURL(server.URL), llm.WithAPIKey("test-key"))
	stream, err := p.CreateStream(t.Context(), llm.Request{
		Model:    "meta/llama-4",
		Messages: msg.BuildTranscript(msg.User("Hello")),
	})
	require.NoError(t, err)
	for range stream {
	}

	assert.Equal(t, "/v1/responses", gotPath, "unknown prefix must route to /v1/responses")
}

// TestProvider_CreateStream_ResponsesEvents verifies that the responses path
// publishes the expected events (started, delta, usage, completed) through
// the unified pipeline.
func TestProvider_CreateStream_ResponsesEvents(t *testing.T) {
	sseBody := strings.Join([]string{
		"event: response.created",
		`data: {"response":{"id":"resp_1","model":"openai/gpt-4o"}}`,
		"",
		"event: response.output_text.delta",
		`data: {"output_index":0,"delta":"hello"}`,
		"",
		"event: response.completed",
		`data: {"response":{"id":"resp_1","model":"openai/gpt-4o","status":"completed","usage":{"input_tokens":10,"output_tokens":3}}}`,
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseBody)
	}))
	defer server.Close()

	p := New(llm.WithBaseURL(server.URL), llm.WithAPIKey("test-key"))
	stream, err := p.CreateStream(t.Context(), llm.Request{
		Model:    "openai/gpt-4o",
		Messages: msg.BuildTranscript(msg.User("Hello")),
	})
	require.NoError(t, err)

	var (
		sawStarted   bool
		sawDelta     bool
		sawCompleted bool
		sawUsage     bool
		inputTok     int
		outputTok    int
	)
	for ev := range stream {
		switch ev.Type {
		case llm.StreamEventStarted:
			sawStarted = true
		case llm.StreamEventDelta:
			sawDelta = true
		case llm.StreamEventCompleted:
			sawCompleted = true
			assert.Equal(t, llm.StopReasonEndTurn, ev.Data.(*llm.CompletedEvent).StopReason)
		case llm.StreamEventUsageUpdated:
			sawUsage = true
			ue := ev.Data.(*llm.UsageUpdatedEvent)
			inputTok = ue.Record.Tokens.Count(usage.KindInput)
			outputTok = ue.Record.Tokens.Count(usage.KindOutput)
			assert.Equal(t, "openrouter", ue.Record.Dims.Provider)
		}
	}

	assert.True(t, sawStarted)
	assert.True(t, sawDelta)
	assert.True(t, sawCompleted)
	assert.True(t, sawUsage)
	assert.Equal(t, 10, inputTok)
	assert.Equal(t, 3, outputTok)
}

// TestProvider_CreateStream_MessagesEvents verifies that the messages path
// publishes the expected events through the unified pipeline.
func TestProvider_CreateStream_MessagesEvents(t *testing.T) {
	sseBody := strings.Join([]string{
		"event: message_start",
		`data: {"message":{"id":"msg_1","model":"anthropic/claude-opus-4-5","usage":{"input_tokens":8}}}`,
		"",
		"event: content_block_start",
		`data: {"index":0,"content_block":{"type":"text"}}`,
		"",
		"event: content_block_delta",
		`data: {"index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		"",
		"event: content_block_stop",
		`data: {"index":0}`,
		"",
		"event: message_delta",
		`data: {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseBody)
	}))
	defer server.Close()

	p := New(llm.WithBaseURL(server.URL), llm.WithAPIKey("test-key"))
	stream, err := p.CreateStream(t.Context(), llm.Request{
		Model:    "anthropic/claude-opus-4-5",
		Messages: msg.BuildTranscript(msg.User("Hello")),
	})
	require.NoError(t, err)

	var sawDelta, sawCompleted, sawUsage bool
	var inputTok, outputTok int
	for ev := range stream {
		switch ev.Type {
		case llm.StreamEventDelta:
			sawDelta = true
		case llm.StreamEventCompleted:
			sawCompleted = true
		case llm.StreamEventUsageUpdated:
			sawUsage = true
			ue := ev.Data.(*llm.UsageUpdatedEvent)
			inputTok = ue.Record.Tokens.Count(usage.KindInput)
			outputTok = ue.Record.Tokens.Count(usage.KindOutput)
		}
	}

	assert.True(t, sawDelta)
	assert.True(t, sawCompleted)
	assert.True(t, sawUsage)
	assert.Equal(t, 8, inputTok)
	assert.Equal(t, 2, outputTok)
}

func TestSelectAPI(t *testing.T) {
	tests := []struct {
		model    string
		hint     llm.ApiType
		wantBack orAPIBackend
		wantType llm.ApiType
	}{
		// Explicit hints
		{"any", llm.ApiTypeOpenAIResponses, orResponses, llm.ApiTypeOpenAIResponses},
		{"any", llm.ApiTypeAnthropicMessages, orMessages, llm.ApiTypeAnthropicMessages},
		// anthropic/* always → messages
		{"anthropic/claude-opus-4-5", llm.ApiTypeAuto, orMessages, llm.ApiTypeAnthropicMessages},
		{"anthropic/claude-haiku-4-5", llm.ApiTypeAuto, orMessages, llm.ApiTypeAnthropicMessages},
		// openai/* always → responses (codex and non-codex alike)
		{"openai/gpt-4o", llm.ApiTypeAuto, orResponses, llm.ApiTypeOpenAIResponses},
		{"openai/gpt-5.4", llm.ApiTypeAuto, orResponses, llm.ApiTypeOpenAIResponses},
		{"openai/gpt-5.3-codex", llm.ApiTypeAuto, orResponses, llm.ApiTypeOpenAIResponses},
		// other prefixes → responses
		{"meta/llama-4-maverick", llm.ApiTypeAuto, orResponses, llm.ApiTypeOpenAIResponses},
		{"mistral/mixtral-8x7b", llm.ApiTypeAuto, orResponses, llm.ApiTypeOpenAIResponses},
		{"auto", llm.ApiTypeAuto, orResponses, llm.ApiTypeOpenAIResponses},
	}
	for _, tc := range tests {
		t.Run(tc.model+"/hint="+string(tc.hint), func(t *testing.T) {
			gotBack, gotType := selectAPI(tc.model, tc.hint)
			assert.Equal(t, tc.wantBack, gotBack, "backend")
			assert.Equal(t, tc.wantType, gotType, "resolved ApiType")
		})
	}
}

func TestUpstreamProviderFromModel(t *testing.T) {
	tests := []struct{ model, want string }{
		{"anthropic/claude-opus-4-5", "anthropic"},
		{"openai/gpt-4o", "openai"},
		{"meta-llama/llama-4-maverick", "meta-llama"},
		{"auto", providerName},
		{"gpt-4o", providerName},
	}
	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			assert.Equal(t, tc.want, upstreamProviderFromModel(tc.model))
		})
	}
}

func TestProvider_CountTokens_NormalizesProviderPrefix(t *testing.T) {
	p := New()
	req := tokencount.TokenCountRequest{
		Model:    "openai/gpt-4o",
		Messages: msg.BuildTranscript(msg.User("Count these tokens carefully.")),
		Tools: []tool.Definition{
			tool.DefinitionFor[struct {
				Location string `json:"location" jsonschema:"required"`
			}]("get_weather", "Get weather"),
		},
	}

	got, err := p.CountTokens(context.Background(), req)
	require.NoError(t, err)

	expected := &tokencount.TokenCount{}
	err = tokencount.CountMessagesAndTools(expected, tokencount.TokenCountRequest{
		Model:    "gpt-4o",
		Messages: req.Messages,
		Tools:    req.Tools,
	}, tokencount.CountOpts{Encoding: tokencount.EncodingO200K})
	require.NoError(t, err)

	assert.Equal(t, expected.InputTokens, got.InputTokens)
}

func TestProvider_CountTokens_UsesDefaultModelWhenEmpty(t *testing.T) {
	p := New().WithDefaultModel("openai/gpt-4o")
	got, err := p.CountTokens(context.Background(), tokencount.TokenCountRequest{
		Messages: msg.BuildTranscript(msg.User("Hello")),
	})
	require.NoError(t, err)
	assert.Positive(t, got.InputTokens)
}
