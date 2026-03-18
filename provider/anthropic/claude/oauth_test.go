package claude

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratePKCE(t *testing.T) {
	pkce, err := GeneratePKCE()
	require.NoError(t, err)
	require.NotNil(t, pkce)

	// Verifier should be 43 characters (per PKCE spec recommendation)
	assert.Len(t, pkce.Verifier, 43, "verifier should be 43 characters")

	// Verifier should only contain allowed characters
	const allowedChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	for _, c := range pkce.Verifier {
		assert.True(t, strings.ContainsRune(allowedChars, c),
			"verifier contains invalid character: %c", c)
	}

	// Challenge should be base64url-encoded SHA256 of verifier
	hash := sha256.Sum256([]byte(pkce.Verifier))
	expectedChallenge := strings.TrimRight(base64.URLEncoding.EncodeToString(hash[:]), "=")
	assert.Equal(t, expectedChallenge, pkce.Challenge)

	// Challenge should not contain padding
	assert.NotContains(t, pkce.Challenge, "=", "challenge should not have padding")
}

func TestGeneratePKCE_Uniqueness(t *testing.T) {
	// Generate multiple PKCE pairs and verify they're all different
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		pkce, err := GeneratePKCE()
		require.NoError(t, err)
		require.False(t, seen[pkce.Verifier], "duplicate verifier generated")
		seen[pkce.Verifier] = true
	}
}

func TestNewOAuthFlow(t *testing.T) {
	flow, err := NewOAuthFlow("")
	require.NoError(t, err)
	require.NotNil(t, flow)

	assert.Equal(t, AnthropicClientID, flow.ClientID)
	assert.Equal(t, DefaultRedirectURI, flow.RedirectURI)
	assert.Equal(t, DefaultScopes, flow.Scopes)
	assert.NotNil(t, flow.PKCE)
	assert.NotEmpty(t, flow.PKCE.Verifier)
	assert.NotEmpty(t, flow.PKCE.Challenge)
	assert.Equal(t, flow.PKCE.Verifier, flow.State, "state should equal verifier")
}

func TestNewOAuthFlow_CustomRedirectURI(t *testing.T) {
	customURI := "http://localhost:8080/callback"
	flow, err := NewOAuthFlow(customURI)
	require.NoError(t, err)

	assert.Equal(t, customURI, flow.RedirectURI)
}

func TestOAuthFlow_AuthorizeURL(t *testing.T) {
	flow, err := NewOAuthFlow("")
	require.NoError(t, err)

	authURL := flow.AuthorizeURL()

	// Parse the URL
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)

	// Check base URL
	assert.Equal(t, "claude.ai", parsed.Host)
	assert.Equal(t, "/oauth/authorize", parsed.Path)

	// Check query parameters
	params := parsed.Query()
	assert.Equal(t, "true", params.Get("code"))
	assert.Equal(t, AnthropicClientID, params.Get("client_id"))
	assert.Equal(t, "code", params.Get("response_type"))
	assert.Equal(t, DefaultRedirectURI, params.Get("redirect_uri"))
	assert.Equal(t, DefaultScopes, params.Get("scope"))
	assert.Equal(t, flow.PKCE.Challenge, params.Get("code_challenge"))
	assert.Equal(t, "S256", params.Get("code_challenge_method"))
	assert.Equal(t, flow.State, params.Get("state"))
}

func TestOAuthFlow_Exchange_Success(t *testing.T) {
	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]string
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)

		assert.Equal(t, "authorization_code", body["grant_type"])
		assert.Equal(t, "test-auth-code", body["code"])
		assert.Equal(t, AnthropicClientID, body["client_id"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-123",
			"refresh_token": "refresh-456",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	// Override token endpoint
	oldEndpoint := tokenEndpoint
	oldClient := httpClient
	tokenEndpoint = server.URL
	httpClient = server.Client()
	defer func() {
		tokenEndpoint = oldEndpoint
		httpClient = oldClient
	}()

	flow, err := NewOAuthFlow("")
	require.NoError(t, err)

	token, err := flow.Exchange(context.Background(), "test-auth-code")
	require.NoError(t, err)
	require.NotNil(t, token)

	assert.Equal(t, "access-123", token.AccessToken)
	assert.Equal(t, "refresh-456", token.RefreshToken)
	assert.WithinDuration(t, time.Now().Add(time.Hour), token.ExpiresAt, 5*time.Second)
}

func TestOAuthFlow_Exchange_WithState(t *testing.T) {
	// Setup mock server
	var receivedBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access",
			"refresh_token": "refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	oldEndpoint := tokenEndpoint
	oldClient := httpClient
	tokenEndpoint = server.URL
	httpClient = server.Client()
	defer func() {
		tokenEndpoint = oldEndpoint
		httpClient = oldClient
	}()

	flow, err := NewOAuthFlow("")
	require.NoError(t, err)

	// Code with state appended (as Anthropic returns it)
	_, err = flow.Exchange(context.Background(), "auth-code#state-value")
	require.NoError(t, err)

	assert.Equal(t, "auth-code", receivedBody["code"])
	assert.Equal(t, "state-value", receivedBody["state"])
}

func TestOAuthFlow_Exchange_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "The authorization code has expired",
		})
	}))
	defer server.Close()

	oldEndpoint := tokenEndpoint
	oldClient := httpClient
	tokenEndpoint = server.URL
	httpClient = server.Client()
	defer func() {
		tokenEndpoint = oldEndpoint
		httpClient = oldClient
	}()

	flow, err := NewOAuthFlow("")
	require.NoError(t, err)

	token, err := flow.Exchange(context.Background(), "expired-code")
	assert.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "HTTP 400")
}

func TestRefreshToken_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)

		assert.Equal(t, "refresh_token", body["grant_type"])
		assert.Equal(t, "old-refresh-token", body["refresh_token"])
		assert.Equal(t, AnthropicClientID, body["client_id"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"expires_in":    7200,
		})
	}))
	defer server.Close()

	oldEndpoint := tokenEndpoint
	oldClient := httpClient
	tokenEndpoint = server.URL
	httpClient = server.Client()
	defer func() {
		tokenEndpoint = oldEndpoint
		httpClient = oldClient
	}()

	token, err := RefreshToken(context.Background(), "old-refresh-token")
	require.NoError(t, err)
	require.NotNil(t, token)

	assert.Equal(t, "new-access-token", token.AccessToken)
	assert.Equal(t, "new-refresh-token", token.RefreshToken)
	assert.WithinDuration(t, time.Now().Add(2*time.Hour), token.ExpiresAt, 5*time.Second)
}

func TestRefreshToken_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "invalid_grant",
		})
	}))
	defer server.Close()

	oldEndpoint := tokenEndpoint
	oldClient := httpClient
	tokenEndpoint = server.URL
	httpClient = server.Client()
	defer func() {
		tokenEndpoint = oldEndpoint
		httpClient = oldClient
	}()

	token, err := RefreshToken(context.Background(), "invalid-refresh-token")
	assert.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "HTTP 401")
}

func TestRefreshToken_NetworkError(t *testing.T) {
	oldEndpoint := tokenEndpoint
	oldClient := httpClient
	tokenEndpoint = "http://localhost:1" // Invalid port
	httpClient = &http.Client{Timeout: 100 * time.Millisecond}
	defer func() {
		tokenEndpoint = oldEndpoint
		httpClient = oldClient
	}()

	token, err := RefreshToken(context.Background(), "refresh-token")
	assert.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "token exchange request")
}

func TestRefreshToken_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	oldEndpoint := tokenEndpoint
	oldClient := httpClient
	tokenEndpoint = server.URL
	httpClient = server.Client()
	defer func() {
		tokenEndpoint = oldEndpoint
		httpClient = oldClient
	}()

	token, err := RefreshToken(context.Background(), "refresh-token")
	assert.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "decode token response")
}
