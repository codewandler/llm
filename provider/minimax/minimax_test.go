package minimax

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
	"github.com/codewandler/llm/provider/anthropic"
)

func TestProviderNameAndModels(t *testing.T) {
	t.Parallel()

	p := New()
	assert.Equal(t, providerName, p.Name())

	models := p.Models()
	require.NotEmpty(t, models)
	assert.Equal(t, providerName, models[0].Provider)
}

func TestProviderModels_HaveCorrectIDs(t *testing.T) {
	t.Parallel()

	p := New()
	models := p.Models()

	expectedIDs := map[string]bool{
		ModelM27: true,
		ModelM25: true,
		ModelM21: true,
		ModelM2:  true,
	}

	for _, m := range models {
		assert.True(t, expectedIDs[m.ID], "unexpected model ToolCallID: %s", m.ID)
		delete(expectedIDs, m.ID)
	}
	assert.Empty(t, expectedIDs, "missing expected models")
}

func TestNew_DefaultOptions(t *testing.T) {
	t.Parallel()

	p := New()
	assert.Equal(t, defaultBaseURL, p.opts.BaseURL)
}

func TestWithLLMOpts(t *testing.T) {
	t.Parallel()

	p := New(WithLLMOpts(
		llm.WithBaseURL("https://custom.api.com"),
		llm.WithAPIKey("custom-key"),
	))

	assert.Equal(t, "https://custom.api.com", p.opts.BaseURL)
	assert.NotNil(t, p.opts.APIKeyFunc, "APIKeyFunc should be set")
}

func TestCreateStream_MissingAPIKey(t *testing.T) {
	t.Parallel()

	p := New(WithLLMOpts(llm.WithAPIKey("")))
	_, err := p.CreateStream(context.Background(), llm.Request{
		Model:    ModelM27,
		Messages: llm.Messages{llm.User("hello")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API key")
}

func TestCreateStream_EmptyModel(t *testing.T) {
	t.Parallel()

	p := New(WithLLMOpts(llm.WithAPIKey("test-key")))
	_, err := p.CreateStream(context.Background(), llm.Request{
		Model:    "", // empty model
		Messages: llm.Messages{llm.User("hello")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")
}

func TestResolve_Aliases(t *testing.T) {
	t.Parallel()

	p := New()

	tests := []struct {
		name    string
		input   string
		wantID  string
		wantErr bool
	}{
		{"default alias", llm.ModelDefault, ModelM27, false},
		{"fast alias", llm.ModelFast, ModelM27, false},
		{"minimax alias", "minimax", ModelM27, false},
		{"exact model ID", ModelM27, ModelM27, false},
		{"unknown model", "MiniMax-Future-99", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := p.Resolve(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantID, resolved.ID)
			}
		})
	}
}

func TestCreateStream_RequestHeadersAndBody(t *testing.T) {
	t.Parallel()

	var (
		gotPath     string
		gotHeaders  http.Header
		gotBody     map[string]any
		requestBody map[string]any
		estimate    *llm.TokenEstimateEvent
	)

	sseBody := strings.Join([]string{
		"event: message_start",
		`data: {"message":{"id":"msg_1","model":"MiniMax-M2.7","usage":{"input_tokens":1}}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		gotPath = r.URL.Path
		gotHeaders = r.Header.Clone()
		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(bodyBytes, &gotBody))

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseBody)
	}))
	defer server.Close()

	p := New(WithLLMOpts(llm.WithBaseURL(server.URL), llm.WithAPIKey("test-key")))

	stream, err := p.CreateStream(context.Background(), llm.Request{
		Model:    llm.ModelDefault,
		Messages: llm.Messages{llm.User("hello")},
		Thinking: llm.ThinkingOff,
	})
	require.NoError(t, err)
	for ev := range stream {
		switch ev.Type {
		case llm.StreamEventRequest:
			reqEv := ev.Data.(*llm.RequestEvent)
			require.NoError(t, json.Unmarshal(reqEv.ProviderRequest.Body, &requestBody))
		case llm.StreamEventTokenEstimate:
			te := ev.Data.(*llm.TokenEstimateEvent)
			if len(te.Estimate.Dims.Labels) == 0 {
				estimate = te
			}
		}
	}

	assert.Equal(t, "/v1/messages", gotPath)
	assert.Equal(t, "Bearer test-key", gotHeaders.Get("Authorization"))
	assert.Equal(t, "test-key", gotHeaders.Get("x-api-key"))
	assert.Equal(t, anthropic.AnthropicVersion, gotHeaders.Get("Anthropic-Version"))
	assert.Equal(t, anthropic.BetaInterleavedThinking, gotHeaders.Get("Anthropic-Beta"))
	assert.Equal(t, "application/json", gotHeaders.Get("Content-Type"))
	assert.Equal(t, "application/json", gotHeaders.Get("Accept"))

	require.NotNil(t, gotBody)
	require.NotNil(t, requestBody)
	assert.Equal(t, ModelM27, gotBody["model"])
	assert.Equal(t, true, gotBody["stream"])
	_, hasThinking := gotBody["thinking"]
	assert.False(t, hasThinking, "thinking field must be omitted")
	_, requestHasThinking := requestBody["thinking"]
	assert.False(t, requestHasThinking, "request event body must also omit thinking")
	assert.Equal(t, gotBody, requestBody)
	if assert.NotNil(t, estimate) {
		assert.False(t, estimate.Estimate.Cost.IsZero())
	}
}

func TestCreateStream_APIErrorReturnsErrorAndInspectableStream(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		http.Error(w, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	p := New(WithLLMOpts(llm.WithBaseURL(server.URL), llm.WithAPIKey("test-key")))

	stream, err := p.CreateStream(context.Background(), llm.Request{
		Model:    ModelM27,
		Messages: llm.Messages{llm.User("hello")},
	})
	require.Error(t, err)
	require.NotNil(t, stream)
	assert.ErrorIs(t, err, llm.ErrAPIError)

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
		}
	}

	assert.True(t, sawRequest, "request event should still be inspectable")
	assert.False(t, sawError, "pre-stream API failures should return error, not emit stream error")
}
