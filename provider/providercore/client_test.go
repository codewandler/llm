package providercore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

func TestClientStream_CompletionsFlow(t *testing.T) {
	t.Parallel()

	var (
		mu          sync.Mutex
		gotPath     string
		gotHeaders  http.Header
		gotBodyJSON map[string]any
	)

	sseBody := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`,
		"",
		`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		mu.Lock()
		gotPath = r.URL.Path
		gotHeaders = r.Header.Clone()
		_ = json.Unmarshal(bodyBytes, &gotBodyJSON)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Org", "org-123")
		w.Header().Set("X-Request-ID", "req-456")
		_, _ = io.WriteString(w, sseBody)
	}))
	defer server.Close()

	var (
		costProvider string
		costModel    string
	)

	cfg := Config{
		ProviderName: "test-provider",
		DefaultModel: "real-model",
		BaseURL:      server.URL,
		APIHint:      llm.ApiTypeOpenAIChatCompletion,
		DefaultHeaders: http.Header{
			"X-Default": {"true"},
		},
		RateLimitParser: func(resp *http.Response) *llm.RateLimits {
			return &llm.RateLimits{
				OrganizationID: resp.Header.Get("X-Org"),
				RequestID:      resp.Header.Get("X-Request-ID"),
			}
		},
		UsageExtras: func(*http.Response) map[string]any {
			return map[string]any{"source": "test"}
		},
		TokenCounter: tokencount.TokenCounterFunc(func(context.Context, tokencount.TokenCountRequest) (*tokencount.TokenCount, error) {
			return &tokencount.TokenCount{InputTokens: 21}, nil
		}),
		HeaderFunc: func(context.Context, *llm.Request) (http.Header, error) {
			return http.Header{"Authorization": {"Bearer test-token"}}, nil
		},
		MutateRequest: func(r *http.Request) {
			r.Header.Set("X-Mutated", "yes")
		},
		PreprocessRequest: func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			req.Model = "real-model"
			return req, original, nil
		},
		ResolveAPIHint: func(llm.Request) llm.ApiType {
			return llm.ApiTypeOpenAIChatCompletion
		},
		ResolveUpstreamProvider: func(llm.Request) string {
			return "openai"
		},
		ResolveCostTargets: func(llm.Request) (string, string) {
			return "openai", "gpt-real"
		},
	}

	WithCostCalculator(usage.CostCalculatorFunc(func(provider, model string, tokens usage.TokenItems) (usage.Cost, bool) {
		costProvider, costModel = provider, model
		return usage.Cost{Total: 0.42, Source: "calculated"}, true
	}))(&cfg)

	client := New(cfg, llm.WithBaseURL(server.URL))

	stream, err := client.Stream(context.Background(), llm.Request{
		Model:    "alias-model",
		Messages: llm.Messages{llm.User("hi")},
	})
	require.NoError(t, err)

	var (
		sawModelResolved bool
		sawTokenEstimate bool
		sawRequest       bool
		sawStarted       bool
		sawDelta         bool
		sawUsage         bool
		sawCompleted     bool
		startedProvider  string
		usageRecord      *usage.Record
	)

	for ev := range stream {
		switch ev.Type {
		case llm.StreamEventModelResolved:
			mr := ev.Data.(*llm.ModelResolvedEvent)
			sawModelResolved = true
			assert.Equal(t, "alias-model", mr.Name)
			assert.Equal(t, "real-model", mr.Resolved)
		case llm.StreamEventTokenEstimate:
			sawTokenEstimate = true
		case llm.StreamEventRequest:
			sawRequest = true
		case llm.StreamEventStarted:
			sawStarted = true
			started := ev.Data.(*llm.StreamStartedEvent)
			startedProvider = started.Provider
			assert.Equal(t, "chatcmpl-1", started.RequestID)
			if assert.NotNil(t, started.Extra) {
				rl, ok := started.Extra["rate_limits"].(*llm.RateLimits)
				if assert.True(t, ok) && assert.NotNil(t, rl) {
					assert.Equal(t, "org-123", rl.OrganizationID)
					assert.Equal(t, "req-456", rl.RequestID)
				}
			}
		case llm.StreamEventDelta:
			sawDelta = true
			delta := ev.Data.(*llm.DeltaEvent)
			assert.Equal(t, "hello", delta.Text)
		case llm.StreamEventUsageUpdated:
			sawUsage = true
			ue := ev.Data.(*llm.UsageUpdatedEvent)
			tmp := ue.Record
			usageRecord = &tmp
		case llm.StreamEventCompleted:
			sawCompleted = true
			completed := ev.Data.(*llm.CompletedEvent)
			assert.Equal(t, llm.StopReasonEndTurn, completed.StopReason)
		}
	}

	require.True(t, sawModelResolved)
	require.True(t, sawTokenEstimate)
	require.True(t, sawRequest)
	require.True(t, sawStarted)
	require.True(t, sawDelta)
	require.True(t, sawUsage)
	require.True(t, sawCompleted)
	assert.Equal(t, "openai", startedProvider)

	if assert.NotNil(t, usageRecord) {
		if assert.Equal(t, "calculated", usageRecord.Cost.Source) {
			assert.InDelta(t, 0.42, usageRecord.Cost.Total, 1e-9)
		}
		if assert.NotNil(t, usageRecord.Extras) {
			assert.Equal(t, "test", usageRecord.Extras["source"])
			_, ok := usageRecord.Extras["rate_limits"].(*llm.RateLimits)
			assert.True(t, ok)
		}
		assert.False(t, usageRecord.Cost.IsZero())
		assert.Equal(t, "test-provider", usageRecord.Dims.Provider)
		assert.Equal(t, "real-model", usageRecord.Dims.Model)
	}

	assert.Equal(t, "openai", costProvider)
	assert.Equal(t, "gpt-real", costModel)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "/v1/chat/completions", gotPath)
	require.NotNil(t, gotBodyJSON)
	assert.Equal(t, true, gotBodyJSON["stream"])
	assert.Equal(t, "Bearer test-token", gotHeaders.Get("Authorization"))
	assert.Equal(t, "true", gotHeaders.Get("X-Default"))
	assert.Equal(t, "yes", gotHeaders.Get("X-Mutated"))
}

func TestClientStream_HeaderFuncMissingKey(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ProviderName: "test",
		DefaultModel: "m",
		BaseURL:      "http://invalid",
		APIHint:      llm.ApiTypeOpenAIChatCompletion,
		HeaderFunc: func(context.Context, *llm.Request) (http.Header, error) {
			return nil, llm.NewErrMissingAPIKey("test")
		},
	}

	client := New(cfg)
	_, err := client.Stream(context.Background(), llm.Request{
		Model:    "alias",
		Messages: llm.Messages{llm.User("hi")},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrMissingAPIKey)
}

func TestClientStream_ErrorParser(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		http.Error(w, "{\"error\":\"quota\"}", http.StatusTooManyRequests)
	}))
	defer server.Close()

	cfg := Config{
		ProviderName: "test",
		DefaultModel: "m",
		BaseURL:      server.URL,
		APIHint:      llm.ApiTypeOpenAIChatCompletion,
		ErrorParser: func(code int, body []byte) error {
			return fmt.Errorf("custom %d %s", code, body)
		},
	}

	client := New(cfg, llm.WithBaseURL(server.URL))
	_, err := client.Stream(context.Background(), llm.Request{
		Model:    "m",
		Messages: llm.Messages{llm.User("hi")},
	})
	require.Error(t, err)
	assert.EqualError(t, err, "custom 429 {\"error\":\"quota\"}\n")
}

func TestClientStream_APIHintResolver(t *testing.T) {
	t.Parallel()

	var gotPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"response\":{\"id\":\"r1\",\"model\":\"openai/gpt-4o\",\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()

	cfg := Config{
		ProviderName: "test",
		DefaultModel: "openai/gpt-4o",
		BaseURL:      server.URL,
		APIHint:      llm.ApiTypeOpenAIChatCompletion,
		ResolveAPIHint: func(req llm.Request) llm.ApiType {
			if strings.HasPrefix(req.Model, "openai/") {
				return llm.ApiTypeOpenAIResponses
			}
			return llm.ApiTypeOpenAIChatCompletion
		},
	}

	client := New(cfg, llm.WithBaseURL(server.URL))
	stream, err := client.Stream(context.Background(), llm.Request{
		Messages: msg.BuildTranscript(msg.User("hi")),
	})
	require.NoError(t, err)
	for range stream {
	}

	assert.Equal(t, "/v1/responses", gotPath)
}

func TestClientStream_MutateRequestMessages(t *testing.T) {
	t.Parallel()

	var capturedHeaders http.Header
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		capturedHeaders = r.Header.Clone()
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(bodyBytes, &capturedBody)

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			"event: message_start\ndata: {\"message\":{\"id\":\"m1\",\"model\":\"anthropic/claude\",\"usage\":{\"input_tokens\":1}}}\n\n"+
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		)
	}))
	defer server.Close()

	cfg := Config{
		ProviderName: "test",
		DefaultModel: "anthropic/claude",
		BaseURL:      server.URL,
		APIHint:      llm.ApiTypeOpenAIChatCompletion,
		PreprocessRequest: func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			req.Model = strings.TrimPrefix(original, "anthropic/")
			return req, original, nil
		},
		ResolveAPIHint: func(llm.Request) llm.ApiType {
			return llm.ApiTypeAnthropicMessages
		},
		MutateRequest: func(r *http.Request) {
			r.Header.Set("Anthropic-Version", "2023-06-01")
		},
	}

	client := New(cfg, llm.WithBaseURL(server.URL))
	stream, err := client.Stream(context.Background(), llm.Request{
		Messages: msg.BuildTranscript(msg.User("hi")),
	})
	require.NoError(t, err)
	for range stream {
	}

	assert.Equal(t, "2023-06-01", capturedHeaders.Get("Anthropic-Version"))
	require.NotNil(t, capturedBody)
	assert.Equal(t, "claude", capturedBody["model"])
}
