package providercore

import (
	"bytes"
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
	"github.com/codewandler/llm/api/messages"
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
		tokenEstimate    *usage.Record
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
			te := ev.Data.(*llm.TokenEstimateEvent)
			tmp := te.Estimate
			tokenEstimate = &tmp
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
	if assert.NotNil(t, tokenEstimate) {
		assert.False(t, tokenEstimate.Cost.IsZero())
		assert.InDelta(t, 0.42, tokenEstimate.Cost.Total, 1e-9)
	}

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
	stream, err := client.Stream(context.Background(), llm.Request{
		Model:    "m",
		Messages: llm.Messages{llm.User("hi")},
	})
	require.Error(t, err)
	assert.EqualError(t, err, "custom 429 {\"error\":\"quota\"}\n")
	require.NotNil(t, stream)

	var sawRequest bool
	for ev := range stream {
		if ev.Type == llm.StreamEventRequest {
			sawRequest = true
		}
		if ev.Type == llm.StreamEventError {
			t.Fatalf("unexpected stream error event: %#v", ev.Data)
		}
	}
	assert.True(t, sawRequest)
}

func TestClientStream_HTTPErrorActionStream(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	client := New(Config{
		ProviderName: "test",
		DefaultModel: "m",
		BaseURL:      server.URL,
		APIHint:      llm.ApiTypeOpenAIChatCompletion,
		ResolveHTTPErrorAction: func(llm.Request, int, error) HTTPErrorAction {
			return HTTPErrorActionStream
		},
	}, llm.WithBaseURL(server.URL))

	stream, err := client.Stream(context.Background(), llm.Request{
		Model:    "m",
		Messages: llm.Messages{llm.User("hi")},
	})
	require.NoError(t, err)
	require.NotNil(t, stream)

	var (
		sawRequest bool
		sawError   bool
	)
	for ev := range stream {
		switch ev.Type {
		case llm.StreamEventRequest:
			sawRequest = true
		case llm.StreamEventError:
			sawError = true
			assert.ErrorIs(t, ev.Data.(*llm.ErrorEvent).Error, llm.ErrAPIError)
		}
	}

	assert.True(t, sawRequest)
	assert.True(t, sawError)
}

func TestClientStream_APITokenCounterEmitsAdditionalEstimate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`,
			"",
			`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))
	}))
	defer server.Close()

	client := New(Config{
		ProviderName: "test",
		DefaultModel: "m",
		BaseURL:      server.URL,
		APIHint:      llm.ApiTypeOpenAIChatCompletion,
		TokenCounter: tokencount.TokenCounterFunc(func(context.Context, tokencount.TokenCountRequest) (*tokencount.TokenCount, error) {
			return &tokencount.TokenCount{InputTokens: 11}, nil
		}),
		APITokenCounter: func(context.Context, llm.Request, any) (*tokencount.TokenCount, error) {
			return &tokencount.TokenCount{InputTokens: 17}, nil
		},
	}, llm.WithBaseURL(server.URL))

	stream, err := client.Stream(context.Background(), llm.Request{
		Model:    "m",
		Messages: llm.Messages{llm.User("hi")},
	})
	require.NoError(t, err)

	var sources []string
	for ev := range stream {
		if ev.Type != llm.StreamEventTokenEstimate {
			continue
		}
		sources = append(sources, ev.Data.(*llm.TokenEstimateEvent).Estimate.Source)
	}

	assert.Equal(t, []string{"heuristic", "api"}, sources)
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

func TestClientStream_TransformWireRequestUpdatesRequestEventBody(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(bodyBytes, &gotBody))

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			"event: message_start\ndata: {\"message\":{\"id\":\"m1\",\"model\":\"claude\",\"usage\":{\"input_tokens\":1}}}\n\n"+
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		)
	}))
	defer server.Close()

	client := New(Config{
		ProviderName: "test",
		BaseURL:      server.URL,
		APIHint:      llm.ApiTypeAnthropicMessages,
		TransformWireRequest: func(api llm.ApiType, wire any) (any, error) {
			msgReq, ok := wire.(*messages.Request)
			if !ok {
				return nil, fmt.Errorf("unexpected payload %T", wire)
			}
			msgReq.Thinking = nil
			return msgReq, nil
		},
	}, llm.WithBaseURL(server.URL))

	stream, err := client.Stream(context.Background(), llm.Request{
		Model:    "claude",
		Messages: msg.BuildTranscript(msg.User("hi")),
		Thinking: llm.ThinkingOn,
	})
	require.NoError(t, err)

	var requestBody map[string]any
	for ev := range stream {
		if ev.Type == llm.StreamEventRequest {
			reqEv := ev.Data.(*llm.RequestEvent)
			require.NoError(t, json.Unmarshal(reqEv.ProviderRequest.Body, &requestBody))
		}
	}

	require.NotNil(t, gotBody)
	require.NotNil(t, requestBody)
	_, gotThinking := gotBody["thinking"]
	_, reqThinking := requestBody["thinking"]
	assert.False(t, gotThinking)
	assert.False(t, reqThinking)
	assert.Equal(t, gotBody, requestBody)
}

// TestClientStream_MutateRequestBody_ReflectedInRequestEvent verifies that
// body mutations made by Config.MutateRequest appear in the RequestEvent's
// ProviderRequest.Body — i.e. the event reflects the actual wire payload.
func TestClientStream_MutateRequestBody_ReflectedInRequestEvent(t *testing.T) {
	t.Parallel()

	var serverBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &serverBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"cmpl-1","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}`,
			"",
			`data: {"id":"cmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))
	}))
	defer server.Close()

	client := New(Config{
		ProviderName: "test",
		BaseURL:      server.URL,
		APIHint:      llm.ApiTypeOpenAIChatCompletion,
		MutateRequest: func(r *http.Request) {
			// Inject a sentinel field into the JSON body.
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			m["injected_by_mutate"] = true
			encoded, _ := json.Marshal(m)
			r.Body = io.NopCloser(bytes.NewReader(encoded))
			r.ContentLength = int64(len(encoded))
		},
	}, llm.WithBaseURL(server.URL))

	stream, err := client.Stream(context.Background(), llm.Request{
		Model:    "gpt-test",
		Messages: llm.Messages{llm.User("hi")},
	})
	require.NoError(t, err)

	var eventBody map[string]any
	for ev := range stream {
		if ev.Type == llm.StreamEventRequest {
			reqEv := ev.Data.(*llm.RequestEvent)
			require.NoError(t, json.Unmarshal(reqEv.ProviderRequest.Body, &eventBody))
		}
	}

	// The server received the mutated body.
	require.NotNil(t, serverBody)
	assert.Equal(t, true, serverBody["injected_by_mutate"],
		"server must receive the mutated body")

	// The RequestEvent must also carry the mutated body, not the pre-mutation one.
	require.NotNil(t, eventBody)
	assert.Equal(t, true, eventBody["injected_by_mutate"],
		"RequestEvent.Body must reflect the body after MutateRequest")

	// Both must be identical — event body == wire body.
	assert.Equal(t, serverBody, eventBody,
		"RequestEvent.Body must exactly match what was sent on the wire")
}

// TestClientStream_MutateRequestBody_StrippedField verifies that fields
// deleted from the body by Config.MutateRequest are also absent from the
// RequestEvent — covering the strip-field pattern used by the Codex provider.
func TestClientStream_MutateRequestBody_StrippedField(t *testing.T) {
	t.Parallel()

	var serverBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &serverBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`data: {"id":"cmpl-2","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`,
			"",
			`data: {"id":"cmpl-2","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			"",
			"data: [DONE]",
			"",
		}, "\n"))
	}))
	defer server.Close()

	client := New(Config{
		ProviderName: "test",
		BaseURL:      server.URL,
		APIHint:      llm.ApiTypeOpenAIChatCompletion,
		MutateRequest: func(r *http.Request) {
			// Strip a field from the body (mirrors Codex stripping max_tokens).
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			delete(m, "stream") // strip the stream field as a stand-in
			encoded, _ := json.Marshal(m)
			r.Body = io.NopCloser(bytes.NewReader(encoded))
			r.ContentLength = int64(len(encoded))
		},
	}, llm.WithBaseURL(server.URL))

	stream, err := client.Stream(context.Background(), llm.Request{
		Model:    "gpt-test",
		Messages: llm.Messages{llm.User("hi")},
	})
	require.NoError(t, err)

	var eventBody map[string]any
	for ev := range stream {
		if ev.Type == llm.StreamEventRequest {
			reqEv := ev.Data.(*llm.RequestEvent)
			require.NoError(t, json.Unmarshal(reqEv.ProviderRequest.Body, &eventBody))
		}
	}

	require.NotNil(t, serverBody)
	require.NotNil(t, eventBody)

	_, serverHasStream := serverBody["stream"]
	_, eventHasStream := eventBody["stream"]
	assert.False(t, serverHasStream, "server must receive body without stripped field")
	assert.False(t, eventHasStream,
		"RequestEvent.Body must not contain fields stripped by MutateRequest")
}

func TestConfigApplyDefaultsAndValidate(t *testing.T) {
	t.Parallel()

	t.Run("apply defaults", func(t *testing.T) {
		t.Parallel()

		cfg := Config{}
		cfg.ApplyDefaults()

		require.NotNil(t, cfg.DefaultHeaders)
		require.NotNil(t, cfg.CostCalculator)
	})

	t.Run("respect explicit nil cost calculator", func(t *testing.T) {
		t.Parallel()

		cfg := Config{}
		WithCostCalculator(nil)(&cfg)
		cfg.ApplyDefaults()

		require.Nil(t, cfg.CostCalculator)
	})

	t.Run("validate missing provider name", func(t *testing.T) {
		t.Parallel()

		err := (Config{APIHint: llm.ApiTypeOpenAIChatCompletion}).Validate()
		require.EqualError(t, err, "providercore: ProviderName must be set")
	})

	t.Run("validate missing api hint", func(t *testing.T) {
		t.Parallel()

		err := (Config{ProviderName: "test"}).Validate()
		require.EqualError(t, err, "providercore: APIHint must be a concrete API type")
	})

	t.Run("validate success", func(t *testing.T) {
		t.Parallel()

		err := (Config{ProviderName: "test", APIHint: llm.ApiTypeOpenAIChatCompletion}).Validate()
		require.NoError(t, err)
	})
}

func TestClientStream_InvalidConfig(t *testing.T) {
	t.Parallel()

	assert.PanicsWithValue(t, "providercore: ProviderName must be set", func() {
		_ = New(Config{})
	})
}
