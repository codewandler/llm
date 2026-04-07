package openai

// Codex CLI local credentials support.
//
// The Codex CLI (https://github.com/openai/codex) stores OAuth tokens for a
// ChatGPT subscription in ~/.codex/auth.json. The access_token in that file
// is a standard JWT accepted as a Bearer token by https://api.openai.com/v1,
// so it works as a drop-in replacement for a regular OPENAI_API_KEY.
//
// Public surface:
//   - [CodexAuth]            — loads and refreshes ~/.codex/auth.json tokens
//   - [LoadCodexAuth]        — loads from the default path
//   - [CodexLocalAvailable]  — availability check (no network call)

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codewandler/llm"
)

const (
	// codexAuthRelPath is the path to auth.json relative to $HOME.
	codexAuthRelPath = ".codex/auth.json"

	// codexTokenEndpoint is the OpenAI Auth0-based token refresh endpoint.
	// Issuer: https://auth.openai.com (observed from id_token.iss).
	codexTokenEndpoint = "https://auth.openai.com/oauth/token"

	// codexClientID is the OAuth client_id used by the Codex CLI.
	// Extracted from the id_token's aud claim.
	codexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// codexTokenExpiryBuffer is how early we attempt a refresh before expiry.
	codexTokenExpiryBuffer = 5 * time.Minute

	// codexBackendBaseURL is the ChatGPT backend that serves Codex models.
	// The ChatGPT Plus OAuth token lacks the api.responses.write scope needed
	// for api.openai.com, but it works here with additional auth headers.
	codexBackendBaseURL = "https://chatgpt.com/backend-api"

	// codexOriginator is the originator header value expected by the backend.
	codexOriginator = "codex_cli_rs"

	// codexBetaHeader / codexBetaValue enable the experimental Responses API.
	codexBetaHeader = "OpenAI-Beta"
	codexBetaValue  = "responses=experimental"

	// codexAccountIDHeader is the header carrying the ChatGPT account UUID.
	codexAccountIDHeader = "chatgpt-account-id"
)

// codexTokenStore mirrors the tokens sub-object in auth.json.
type codexTokenStore struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id"`
}

// codexAuthFile mirrors the structure of ~/.codex/auth.json.
type codexAuthFile struct {
	AuthMode    string          `json:"auth_mode"`
	APIKey      *string         `json:"OPENAI_API_KEY"`
	Tokens      codexTokenStore `json:"tokens"`
	LastRefresh time.Time       `json:"last_refresh"`
}

// CodexAuth wraps Codex CLI OAuth credentials and handles transparent token
// refresh. It is safe for concurrent use.
//
// Obtain one via [LoadCodexAuth], then pass [CodexAuth.Token] to
// [llm.WithAPIKeyFunc] when constructing a Provider:
//
//	auth, err := openai.LoadCodexAuth()
//	if err != nil { ... }
//	p := openai.New(llm.WithAPIKeyFunc(auth.Token))
type CodexAuth struct {
	mu         sync.Mutex
	auth       codexAuthFile
	path       string
	expiry     time.Time
	httpClient *http.Client
}

// LoadCodexAuth reads ~/.codex/auth.json and returns a CodexAuth ready for
// use. Returns an error if the file does not exist or contains no tokens.
func LoadCodexAuth() (*CodexAuth, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("codex: get home dir: %w", err)
	}
	return loadCodexAuthFrom(filepath.Join(home, codexAuthRelPath))
}

// loadCodexAuthFrom reads the auth file at path.
func loadCodexAuthFrom(path string) (*CodexAuth, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("codex: read %s: %w", path, err)
	}
	var auth codexAuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("codex: parse auth file: %w", err)
	}
	if auth.Tokens.AccessToken == "" && auth.Tokens.RefreshToken == "" {
		return nil, fmt.Errorf("codex: no tokens in %s", path)
	}
	c := &CodexAuth{
		auth:       auth,
		path:       path,
		httpClient: http.DefaultClient,
	}
	if exp, err := codexJWTExpiry(auth.Tokens.AccessToken); err == nil {
		c.expiry = exp
	}
	return c, nil
}

// CodexLocalAvailable reports whether ~/.codex/auth.json exists and contains
// usable credentials. Does not validate the token against the server.
func CodexLocalAvailable() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	c, err := loadCodexAuthFrom(filepath.Join(home, codexAuthRelPath))
	return err == nil && (c.auth.Tokens.AccessToken != "" || c.auth.Tokens.RefreshToken != "")
}

// Token returns a valid OAuth access token, refreshing it transparently when
// it is within [codexTokenExpiryBuffer] of expiry. It satisfies the signature
// expected by [llm.WithAPIKeyFunc].
func (c *CodexAuth) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Token is still fresh — return it directly.
	if !c.expiry.IsZero() && time.Now().Add(codexTokenExpiryBuffer).Before(c.expiry) {
		return c.auth.Tokens.AccessToken, nil
	}
	// Expiry unknown but we have a token and no refresh token — return as-is.
	if c.expiry.IsZero() && c.auth.Tokens.AccessToken != "" && c.auth.Tokens.RefreshToken == "" {
		return c.auth.Tokens.AccessToken, nil
	}
	// Refresh.
	if c.auth.Tokens.RefreshToken == "" {
		if c.auth.Tokens.AccessToken != "" {
			return c.auth.Tokens.AccessToken, nil
		}
		return "", fmt.Errorf("codex: no refresh token and access_token is empty")
	}
	return c.refresh(ctx)
}

// refresh fetches a new access token using the stored refresh token.
// Caller must hold c.mu.
func (c *CodexAuth) refresh(ctx context.Context) (string, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.auth.Tokens.RefreshToken},
		"client_id":     {codexClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexTokenEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("codex: build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("codex: token refresh: %w", err)
	}
	//nolint:errcheck // intentional: defer Close is only for cleanup
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"` // server may rotate the refresh token
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("codex: decode refresh response (status %d): %w", resp.StatusCode, err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("codex: token refresh failed: %s: %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("codex: empty access_token in refresh response (status %d)", resp.StatusCode)
	}

	c.auth.Tokens.AccessToken = result.AccessToken
	if result.RefreshToken != "" {
		c.auth.Tokens.RefreshToken = result.RefreshToken
	}
	if result.ExpiresIn > 0 {
		c.expiry = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	} else if exp, err := codexJWTExpiry(result.AccessToken); err == nil {
		c.expiry = exp
	} else {
		c.expiry = time.Time{}
	}

	// Best-effort write-back so other processes (e.g. the Codex TUI) pick up
	// the refreshed token. Failure is intentionally ignored.
	c.saveAuthLocked()

	return result.AccessToken, nil
}

// saveAuthLocked persists auth to disk. Caller must hold c.mu.
func (c *CodexAuth) saveAuthLocked() {
	c.auth.LastRefresh = time.Now().UTC()
	data, err := json.MarshalIndent(c.auth, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(c.path, data, 0600)
}

// AccountID returns the ChatGPT account UUID from the stored tokens.
// It is required as the chatgpt-account-id HTTP header on every Codex request.
func (c *CodexAuth) AccountID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.auth.Tokens.AccountID
}

// NewProvider creates an OpenAI *Provider pre-configured to route requests to
// the ChatGPT Codex backend (https://chatgpt.com/backend-api).
//
// The standard api.openai.com endpoints require an api.responses.write scope
// that the ChatGPT Plus OAuth token does not carry. The ChatGPT backend accepts
// the same token but requires three additional headers and store:false in the
// request body. codexTransport handles all of this transparently.
//
// baseTransport is the underlying HTTP transport that codexTransport wraps.
// Pass nil (or omit) to use http.DefaultTransport. Callers that need custom
// proxy settings or timeouts should pass their own http.RoundTripper here.
//
// The returned provider supports Codex models (gpt-5.3-codex, etc.) via the
// Responses API SSE format, which is identical between the two backends.
func (c *CodexAuth) NewProvider(baseTransport ...http.RoundTripper) *Provider {
	base := http.RoundTripper(http.DefaultTransport)
	if len(baseTransport) > 0 && baseTransport[0] != nil {
		base = baseTransport[0]
	}
	transport := &codexTransport{base: base, auth: c}
	return New(
		llm.WithBaseURL(codexBackendBaseURL),
		llm.WithAPIKey("chatgpt-oauth"), // placeholder; overridden per-request by transport
		llm.WithHTTPClient(&http.Client{Transport: transport}),
	)
}

// codexTransport is an http.RoundTripper that adapts standard OpenAI SDK
// requests for the ChatGPT Codex backend.
//
// Per-request transformations:
//  1. Injects Authorization, chatgpt-account-id, OpenAI-Beta, originator headers.
//  2. Rewrites the URL path:  /v1/responses → /codex/responses
//     (Chat Completions paths are left unchanged for future use.)
//  3. Injects "store": false into JSON request bodies (required by backend).
type codexTransport struct {
	base http.RoundTripper
	auth *CodexAuth
}

func (t *codexTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())

	// Rewrite /v1/responses → /codex/responses.
	if strings.HasSuffix(req.URL.Path, "/v1/responses") {
		req.URL.Path = strings.TrimSuffix(req.URL.Path, "/v1/responses") + "/codex/responses"
	}

	// Inject auth and Codex-specific headers.
	tok, err := t.auth.Token(req.Context())
	if err != nil {
		return nil, fmt.Errorf("codex transport: get token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set(codexAccountIDHeader, t.auth.AccountID())
	req.Header.Set(codexBetaHeader, codexBetaValue)
	req.Header.Set("originator", codexOriginator)

	// Inject "store": false into the JSON body (required by the Codex backend).
	if req.Body != nil && req.Header.Get("Content-Type") == "application/json" {
		newBody, length, injectErr := codexInjectStore(req.Body)
		if injectErr == nil {
			req.Body = newBody
			req.ContentLength = length
		}
		// On decode error leave the body unchanged; the server will return an
		// informative error rather than silently corrupting the request.
	}

	return t.base.RoundTrip(req)
}

// codexInjectStore reads b, applies the following transformations, and returns
// a replacement ReadCloser. The original body is always closed.
//
// Transformations:
//  1. Sets "store": false (required by the Codex backend).
//  2. Deletes "max_tokens" and "max_output_tokens" — neither field is
//     supported by chatgpt.com/backend-api/codex/responses. The backend
//     controls output length itself; supplying these fields returns HTTP 400.
func codexInjectStore(b io.ReadCloser) (io.ReadCloser, int64, error) {
	//nolint:errcheck // intentional: caller replaces body only on success
	defer b.Close()
	var m map[string]any
	if err := json.NewDecoder(b).Decode(&m); err != nil {
		return nil, 0, fmt.Errorf("decode body: %w", err)
	}
	m["store"] = false
	// The Codex backend does not accept token-limit parameters.
	delete(m, "max_tokens")
	delete(m, "max_output_tokens")
	out, err := json.Marshal(m)
	if err != nil {
		return nil, 0, fmt.Errorf("re-encode body: %w", err)
	}
	return io.NopCloser(bytes.NewReader(out)), int64(len(out)), nil
}

// codexJWTExpiry parses the exp claim from a JWT without verifying the
// signature. Returns an error if token is not a valid JWT or carries no exp.
func codexJWTExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decode JWT payload: %w", err)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("unmarshal JWT claims: %w", err)
	}
	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("JWT has no exp claim")
	}
	return time.Unix(claims.Exp, 0), nil
}
