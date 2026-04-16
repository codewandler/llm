package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

// ── unit tests ────────────────────────────────────────────────────────────────

func TestCodexInjectStore_SetsStoreFlag(t *testing.T) {
	input := `{"model":"gpt-5.3-codex","stream":true}`
	rc, _, err := codexInjectStore(io.NopCloser(strings.NewReader(input)))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.NewDecoder(rc).Decode(&m))
	assert.Equal(t, false, m["store"], "store must be set to false")
}

func TestCodexInjectStore_StripsUnsupportedFields(t *testing.T) {
	// max_tokens, max_output_tokens, and prompt_cache_retention are all
	// rejected by the Codex backend with HTTP 400.
	input := `{"model":"gpt-5.3-codex","max_tokens":1000,"max_output_tokens":1000,"prompt_cache_retention":"24h","stream":true}`
	rc, _, err := codexInjectStore(io.NopCloser(strings.NewReader(input)))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.NewDecoder(rc).Decode(&m))
	_, hasMaxTokens := m["max_tokens"]
	_, hasMaxOutputTokens := m["max_output_tokens"]
	_, hasCacheRetention := m["prompt_cache_retention"]
	assert.False(t, hasMaxTokens, "max_tokens must be stripped")
	assert.False(t, hasMaxOutputTokens, "max_output_tokens must be stripped")
	assert.False(t, hasCacheRetention, "prompt_cache_retention must be stripped")
}

func TestCodexInjectStore_PreservesOtherFields(t *testing.T) {
	input := `{"model":"gpt-5.3-codex","stream":true,"instructions":"be brief"}`
	rc, _, err := codexInjectStore(io.NopCloser(strings.NewReader(input)))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.NewDecoder(rc).Decode(&m))
	assert.Equal(t, "gpt-5.3-codex", m["model"])
	assert.Equal(t, "be brief", m["instructions"])
}

func TestCodexJWTExpiry_ValidToken(t *testing.T) {
	exp := time.Now().Add(10 * time.Minute).Unix()
	token := makeJWT(t, map[string]any{"exp": exp})

	got, err := codexJWTExpiry(token)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Unix(exp, 0), got, time.Second)
}

func TestCodexJWTExpiry_NotAJWT(t *testing.T) {
	_, err := codexJWTExpiry("notavalidtoken")
	require.Error(t, err)
}

func TestCodexJWTExpiry_MissingExp(t *testing.T) {
	token := makeJWT(t, map[string]any{"sub": "user-123"})
	_, err := codexJWTExpiry(token)
	require.Error(t, err)
}

func TestCodexJWTExpiry_ExpiredToken(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour).Unix()
	token := makeJWT(t, map[string]any{"exp": past})

	got, err := codexJWTExpiry(token)
	require.NoError(t, err)
	// Expiry parses fine even when already in the past; the caller decides what to do.
	assert.True(t, got.Before(time.Now()))
}

func TestLoadCodexAuthFrom_ParsesStructure(t *testing.T) {
	exp := time.Now().Add(24 * time.Hour).Unix()
	accessToken := makeJWT(t, map[string]any{
		"exp": exp,
		"aud": []string{"https://api.openai.com/v1"},
	})

	auth := codexAuthFile{
		AuthMode: "chatgpt",
		Tokens: codexTokenStore{
			AccessToken:  accessToken,
			RefreshToken: "rt_synthetic_refresh_token",
			AccountID:    "test-account-id",
		},
		LastRefresh: time.Now().UTC(),
	}
	path := writeAuthFile(t, auth)

	c, err := loadCodexAuthFrom(path)
	require.NoError(t, err)
	assert.Equal(t, "chatgpt", c.auth.AuthMode)
	assert.Equal(t, accessToken, c.auth.Tokens.AccessToken)
	assert.Equal(t, "rt_synthetic_refresh_token", c.auth.Tokens.RefreshToken)
	// Expiry should be parsed from the JWT.
	assert.WithinDuration(t, time.Unix(exp, 0), c.expiry, time.Second)
}

func TestLoadCodexAuthFrom_MissingFile(t *testing.T) {
	_, err := loadCodexAuthFrom("/nonexistent/path/auth.json")
	require.Error(t, err)
}

func TestLoadCodexAuthFrom_EmptyTokens(t *testing.T) {
	auth := codexAuthFile{AuthMode: "chatgpt"}
	path := writeAuthFile(t, auth)

	_, err := loadCodexAuthFrom(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tokens")
}

func TestCodexAuth_Token_ReturnsCachedWhenFresh(t *testing.T) {
	exp := time.Now().Add(1 * time.Hour).Unix()
	accessToken := makeJWT(t, map[string]any{"exp": exp})

	auth := codexAuthFile{
		AuthMode: "chatgpt",
		Tokens: codexTokenStore{
			AccessToken: accessToken,
		},
	}
	path := writeAuthFile(t, auth)

	c, err := loadCodexAuthFrom(path)
	require.NoError(t, err)

	tok, err := c.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, accessToken, tok)
}

// ── integration tests (skipped when ~/.codex/auth.json is unavailable) ────────

// TestCodexLocalAvailable verifies that CodexLocalAvailable returns true when
// ~/.codex/auth.json is present and contains tokens.
func TestCodexLocalAvailable(t *testing.T) {
	if !CodexLocalAvailable() {
		t.Skip("~/.codex/auth.json not available")
	}
	assert.True(t, CodexLocalAvailable())
}

// TestCodexAuth_LoadLocal verifies that the local auth file loads cleanly and
// that the access token is a non-empty JWT.
func TestCodexAuth_LoadLocal(t *testing.T) {
	if !CodexLocalAvailable() {
		t.Skip("~/.codex/auth.json not available")
	}

	c, err := LoadCodexAuth()
	require.NoError(t, err)
	require.NotEmpty(t, c.auth.Tokens.AccessToken, "access_token must be non-empty")
	assert.Equal(t, "chatgpt", c.auth.AuthMode)
}

// TestCodexAuth_Token_ReturnsNonEmpty verifies that Token() returns a
// non-empty string without hitting the refresh endpoint (token should be fresh).
func TestCodexAuth_Token_ReturnsNonEmpty(t *testing.T) {
	if !CodexLocalAvailable() {
		t.Skip("~/.codex/auth.json not available")
	}

	c, err := LoadCodexAuth()
	require.NoError(t, err)

	tok, err := c.Token(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, tok)
	// Should look like a JWT (three dot-separated parts).
	assert.Equal(t, 3, len(splitDots(tok)), "access_token should be a JWT")
}

// TestCodexAuth_TokenExpiry_ParsedFromJWT verifies that the expiry field is
// populated by parsing the access_token JWT.
func TestCodexAuth_TokenExpiry_ParsedFromJWT(t *testing.T) {
	if !CodexLocalAvailable() {
		t.Skip("~/.codex/auth.json not available")
	}

	c, err := LoadCodexAuth()
	require.NoError(t, err)
	assert.False(t, c.expiry.IsZero(), "expiry should be parsed from the JWT")
	assert.True(t, c.expiry.After(time.Now()), "access_token should not already be expired")
}

// TestCodexAuth_ListModels_WithLocalCredentials verifies that the standard
// /v1/models endpoint correctly rejects the ChatGPT OAuth token with a
// missing-scope error (not an invalid-token error). This confirms that the
// token authenticates successfully but is scoped for the ChatGPT backend
// rather than the OpenAI developer API — which is expected.
//
// To actually call inference models, use CodexAuth.NewProvider() which routes
// to https://chatgpt.com/backend-api/codex/responses (see TestCodexAuth_Stream_ResponsesAPI).
func TestCodexAuth_ListModels_WithLocalCredentials(t *testing.T) {
	if !CodexLocalAvailable() {
		t.Skip("~/.codex/auth.json not available")
	}

	c, err := LoadCodexAuth()
	require.NoError(t, err)

	// The ChatGPT Plus OAuth token has scopes api.connectors.* but NOT
	// api.model.read, so FetchModels against api.openai.com is expected to
	// return an API error (not a network or auth-format error).
	p := New(llm.WithAPIKeyFunc(c.Token))
	_, apiErr := p.FetchModels(context.Background())
	require.Error(t, apiErr, "expected an error from /v1/models with ChatGPT token")
	assert.Contains(t, apiErr.Error(), "403",
		"expected HTTP 403 (missing scope), got: %v", apiErr)
	t.Logf("confirmed expected scope rejection: %v", apiErr)
}

// TestCodexTransport_StripsPromptCacheRetention verifies that codexTransport
// removes prompt_cache_retention from the request body before it reaches the
// Codex backend. The Codex backend returns HTTP 400 for this field, so it
// must be stripped regardless of how the request was built.
func TestCodexTransport_StripsPromptCacheRetention(t *testing.T) {
	var capturedBody []byte

	// httptest server that records the raw body and returns a minimal
	// streaming response so the provider doesn't error out.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		capturedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Minimal valid response.completed event.
		_, _ = fmt.Fprintln(w, `event: response.completed`)
		_, _ = fmt.Fprintln(w, `data: {"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`)
		_, _ = fmt.Fprintln(w, ``)
	}))
	defer server.Close()

	// Build a CodexAuth with a fake long-lived JWT so Token() does not refresh.
	exp := time.Now().Add(1 * time.Hour).Unix()
	accessToken := makeJWT(t, map[string]any{"exp": exp, "sub": "test-user"})
	auth := codexAuthFile{
		AuthMode: "chatgpt",
		Tokens: codexTokenStore{
			AccessToken: accessToken,
			AccountID:   "test-account",
		},
	}
	path := writeAuthFile(t, auth)
	c, err := loadCodexAuthFrom(path)
	require.NoError(t, err)

	// Build the transport, pointing it at the test server.
	base := &http.Transport{} // plain transport, no TLS needed for httptest
	transport := &codexTransport{base: base, auth: c}

	// Build a body that contains prompt_cache_retention (as respBuildRequest
	// would for a gpt-5.4 model) and send it through the transport.
	reqBody := `{"model":"gpt-5.4","stream":true,"prompt_cache_retention":"24h","input":[{"role":"user","content":"hi"}]}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")

	//nolint:errcheck // response body not needed for this test
	resp, _ := transport.RoundTrip(httpReq)
	if resp != nil {
		_ = resp.Body.Close()
	}

	require.NotEmpty(t, capturedBody, "test server must have received a request body")
	var received map[string]any
	require.NoError(t, json.Unmarshal(capturedBody, &received))

	_, hasCacheRetention := received["prompt_cache_retention"]
	assert.False(t, hasCacheRetention,
		"prompt_cache_retention must be stripped by codexTransport before reaching the backend")
	assert.Equal(t, false, received["store"],
		"store:false must be injected by codexTransport")
}

func TestCodexTransport_RewritesPathAndInjectsHeaders(t *testing.T) {
	var (
		capturedPath    string
		capturedHeaders http.Header
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, `event: response.completed`)
		_, _ = fmt.Fprintln(w, `data: {"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
		_, _ = fmt.Fprintln(w, ``)
	}))
	defer server.Close()

	exp := time.Now().Add(1 * time.Hour).Unix()
	accessToken := makeJWT(t, map[string]any{"exp": exp, "sub": "test-user"})
	auth := codexAuthFile{
		AuthMode: "chatgpt",
		Tokens: codexTokenStore{
			AccessToken: accessToken,
			AccountID:   "test-account",
		},
	}
	path := writeAuthFile(t, auth)
	c, err := loadCodexAuthFrom(path)
	require.NoError(t, err)

	transport := &codexTransport{base: http.DefaultTransport, auth: c}
	reqBody := `{"model":"gpt-5.3-codex","stream":true,"input":[{"role":"user","content":"hi"}]}`
	httpReq, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", strings.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := transport.RoundTrip(httpReq)
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()

	assert.Equal(t, "/codex/responses", capturedPath)
	assert.Equal(t, "Bearer "+accessToken, capturedHeaders.Get("Authorization"))
	assert.Equal(t, "test-account", capturedHeaders.Get(codexAccountIDHeader))
	assert.Equal(t, codexBetaValue, capturedHeaders.Get(codexBetaHeader))
	assert.Equal(t, codexOriginator, capturedHeaders.Get("originator"))
}

// TestCodexAuth_Stream_ChatCompletions verifies that calling /v1/chat/completions
// on api.openai.com with the ChatGPT OAuth token is rejected by the developer
// API. The exact status may vary by account state (scope rejection, quota,
// billing), but the request must fail as an API error. Chat Completions are not
// available with this token; use the Responses API via NewProvider() instead
// (see TestCodexAuth_Stream_ResponsesAPI).
func TestCodexAuth_Stream_ChatCompletions(t *testing.T) {
	if !CodexLocalAvailable() {
		t.Skip("~/.codex/auth.json not available")
	}

	c, err := LoadCodexAuth()
	require.NoError(t, err)

	p := New(llm.WithAPIKeyFunc(c.Token))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Chat Completions is not a supported path for the ChatGPT OAuth token.
	_, apiErr := p.CreateStream(ctx, llm.Request{
		Model: ModelGPT4oMini,
		Messages: llm.Messages{
			llm.User("hi"),
		},
	})
	require.Error(t, apiErr, "expected developer API request to fail with ChatGPT OAuth token")
	assert.ErrorIs(t, apiErr, llm.ErrAPIError)
	assert.Contains(t, apiErr.Error(), "HTTP ", "expected an HTTP API error, got: %v", apiErr)
	t.Logf("confirmed expected developer API rejection: %v", apiErr)
}

// TestCodexAuth_Stream_ResponsesAPI makes a real streaming request to the
// ChatGPT Codex backend (https://chatgpt.com/backend-api/codex/responses)
// using local Codex credentials.
//
// This is the correct end-to-end path for Codex models:
//   - Use CodexAuth.NewProvider() to get a pre-configured *Provider.
//   - The underlying codexTransport rewrites the URL, injects auth headers,
//     and adds "store": false to the request body automatically.
func TestCodexAuth_Stream_ResponsesAPI(t *testing.T) {
	if !CodexLocalAvailable() {
		t.Skip("~/.codex/auth.json not available")
	}

	c, err := LoadCodexAuth()
	require.NoError(t, err)

	// NewProvider() routes to chatgpt.com/backend-api with the correct headers.
	p := c.NewProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model: ModelGPT53Codex,
		Messages: llm.Messages{
			llm.System("You are a helpful coding assistant."),
			llm.User("Reply with exactly one word: ok"),
		},
		MaxTokens: 20,
	})
	require.NoError(t, err)

	var text string
	var gotDone bool
	for event := range stream {
		switch event.Type {
		case llm.StreamEventError:
			t.Fatalf("stream error: %v", event.Data.(*llm.ErrorEvent).Error)
		case llm.StreamEventDelta:
			if de, ok := event.Data.(*llm.DeltaEvent); ok {
				text += de.Text
			}
		case llm.StreamEventCompleted:
			gotDone = true
		}
	}

	t.Logf("response: %q", text)
	assert.True(t, gotDone, "stream should complete")
	assert.NotEmpty(t, text, "should receive text")
}

// ── helpers ───────────────────────────────────────────────────────────────────

// makeJWT creates a minimal, unsigned JWT with the given claims, sufficient
// for testing JWT parsing logic without network calls.
func makeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	body := base64.RawURLEncoding.EncodeToString(payload)
	sig := base64.RawURLEncoding.EncodeToString([]byte("fakesig"))
	return fmt.Sprintf("%s.%s.%s", header, body, sig)
}

// writeAuthFile writes auth as JSON to a temp file and returns its path.
func writeAuthFile(t *testing.T, auth codexAuthFile) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	data, err := json.Marshal(auth)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	return path
}

// splitDots splits s by "." without importing strings in the test file.
func splitDots(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}
