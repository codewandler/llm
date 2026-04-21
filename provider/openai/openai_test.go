package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

func TestProvider_CreateStream_ResponsesBodyIncludesPromptCacheRetention_FromRequestCache(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-5.4\",\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()

	p := New(llm.WithBaseURL(server.URL), llm.WithAPIKey("test-key"))
	stream, err := p.CreateStream(t.Context(), llm.Request{
		Model:     "gpt-5.4",
		CacheHint: &llm.CacheHint{Enabled: true, TTL: "1h"},
		Messages:  msg.BuildTranscript(msg.User("Hello")),
	})
	require.NoError(t, err)
	for range stream {
	}

	assert.Equal(t, "24h", gotBody["prompt_cache_retention"])
}

func TestProvider_CreateStream_ResponsesBodySynthesizesPromptCacheRetention_FromMessageCache(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-5.4\",\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()

	p := New(llm.WithBaseURL(server.URL), llm.WithAPIKey("test-key"))
	stream, err := p.CreateStream(t.Context(), llm.Request{
		Model: "gpt-5.4",
		Messages: msg.BuildTranscript(
			msg.System("cached").Cache(msg.CacheTTL1h),
			msg.User("Hello"),
		),
	})
	require.NoError(t, err)
	for range stream {
	}

	assert.Equal(t, "24h", gotBody["prompt_cache_retention"])
}

func TestProvider_CreateStream_ResponsesBody_UsesOnlyPromptCacheRetention_WhenBothRequestAndMessageCacheProvided(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-5.4\",\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()

	p := New(llm.WithBaseURL(server.URL), llm.WithAPIKey("test-key"))
	stream, err := p.CreateStream(t.Context(), llm.Request{
		Model:     "gpt-5.4",
		CacheHint: &llm.CacheHint{Enabled: true, TTL: "1h"},
		Messages: msg.BuildTranscript(
			msg.System("cached").Cache(msg.CacheTTL1h),
			msg.User("Hello"),
		),
	})
	require.NoError(t, err)
	for range stream {
	}

	assert.Equal(t, "24h", gotBody["prompt_cache_retention"])
	assert.Nil(t, gotBody["cache_control"])
}
