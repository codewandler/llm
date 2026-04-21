package codex

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

func testAuth() *Auth {
	return &Auth{
		auth:   authFile{Tokens: tokenStore{AccessToken: "test-token", AccountID: "acct_123"}},
		expiry: time.Now().Add(time.Hour),
	}
}

func TestProvider_CreateStream_ResponsesBodyIncludesPromptCacheRetention_FromRequestCache(t *testing.T) {
	var gotBody map[string]any
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		defer r.Body.Close()
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-5.4\",\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()

	p := New(testAuth(), llm.WithBaseURL(server.URL))
	stream, err := p.CreateStream(t.Context(), llm.Request{
		Model:     "gpt-5.4",
		CacheHint: &llm.CacheHint{Enabled: true, TTL: "1h"},
		Messages:  msg.BuildTranscript(msg.User("Hello")),
	})
	require.NoError(t, err)
	for range stream {
	}

	assert.Equal(t, "24h", gotBody["prompt_cache_retention"])
	assert.Equal(t, false, gotBody["store"])
	_, hasMaxTokens := gotBody["max_tokens"]
	assert.False(t, hasMaxTokens)
	_, hasMaxOutputTokens := gotBody["max_output_tokens"]
	assert.False(t, hasMaxOutputTokens)
	assert.Equal(t, "Bearer test-token", gotHeader.Get("Authorization"))
	assert.Equal(t, "acct_123", gotHeader.Get(accountIDHeader))
}
