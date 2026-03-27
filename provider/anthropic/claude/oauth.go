package claude

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// AnthropicClientID is the public OAuth client ToolCallID for Anthropic.
	AnthropicClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

	// AuthEndpoint is the Claude OAuth authorization endpoint.
	AuthEndpoint = "https://claude.ai/oauth/authorize"

	// DefaultRedirectURI is the default redirect URI for code-based flow.
	DefaultRedirectURI = "https://console.anthropic.com/oauth/code/callback"

	// DefaultScopes are the OAuth scopes requested.
	DefaultScopes = "org:create_api_key user:profile user:inference"
)

var (
	// tokenEndpoint is the Anthropic OAuth token endpoint.
	// This is a var to allow overriding in tests.
	tokenEndpoint = "https://console.anthropic.com/v1/oauth/token"

	// httpClient is the HTTP client used for token requests.
	// This is a var to allow overriding in tests.
	httpClient = http.DefaultClient
)

// PKCECodes holds the PKCE verifier and challenge pair.
type PKCECodes struct {
	Verifier  string // Random string, kept secret
	Challenge string // SHA256 hash of verifier, sent to auth server
}

// GeneratePKCE creates a new PKCE code pair.
func GeneratePKCE() (*PKCECodes, error) {
	verifier, err := generateRandomString(43)
	if err != nil {
		return nil, fmt.Errorf("generate PKCE verifier: %w", err)
	}

	hash := sha256.Sum256([]byte(verifier))
	challenge := base64URLEncode(hash[:])

	return &PKCECodes{
		Verifier:  verifier,
		Challenge: challenge,
	}, nil
}

// OAuthFlow manages the OAuth2 authorization code flow.
type OAuthFlow struct {
	ClientID    string
	RedirectURI string
	Scopes      string
	PKCE        *PKCECodes
	State       string // CSRF protection, also used to pass verifier in some flows
}

// NewOAuthFlow creates a new OAuth flow with auto-generated PKCE codes.
func NewOAuthFlow(redirectURI string) (*OAuthFlow, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	if redirectURI == "" {
		redirectURI = DefaultRedirectURI
	}

	return &OAuthFlow{
		ClientID:    AnthropicClientID,
		RedirectURI: redirectURI,
		Scopes:      DefaultScopes,
		PKCE:        pkce,
		State:       pkce.Verifier, // Anthropic uses state to pass verifier
	}, nil
}

// AuthorizeURL returns the URL the user should visit to authorize.
func (f *OAuthFlow) AuthorizeURL() string {
	params := url.Values{}
	params.Set("code", "true") // Indicates code-based flow
	params.Set("client_id", f.ClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", f.RedirectURI)
	params.Set("scope", f.Scopes)
	params.Set("code_challenge", f.PKCE.Challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", f.State)

	return AuthEndpoint + "?" + params.Encode()
}

// Exchange trades an authorization code for tokens.
// The code format from Anthropic is: "authorization_code#state".
func (f *OAuthFlow) Exchange(ctx context.Context, code string) (*Token, error) {
	// Split code and state if present
	parts := strings.SplitN(code, "#", 2)
	authCode := parts[0]

	body := map[string]string{
		"grant_type":    "authorization_code",
		"code":          authCode,
		"client_id":     f.ClientID,
		"redirect_uri":  f.RedirectURI,
		"code_verifier": f.PKCE.Verifier,
	}
	if len(parts) > 1 {
		body["state"] = parts[1]
	}

	return exchangeToken(ctx, body)
}

// RefreshToken exchanges a refresh token for new tokens.
func RefreshToken(ctx context.Context, refreshToken string) (*Token, error) {
	result, err := RefreshTokenVerbose(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	return result.Token, nil
}

// RefreshResult contains the result of a token refresh operation.
type RefreshResult struct {
	Token    *Token
	Duration time.Duration
	Endpoint string
}

// RefreshTokenVerbose exchanges a refresh token for new tokens and returns timing info.
func RefreshTokenVerbose(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	body := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     AnthropicClientID,
	}

	start := time.Now()
	token, err := exchangeToken(ctx, body)
	duration := time.Since(start)

	if err != nil {
		return nil, err
	}

	return &RefreshResult{
		Token:    token,
		Duration: duration,
		Endpoint: tokenEndpoint,
	}, nil
}

func exchangeToken(ctx context.Context, body map[string]string) (*Token, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenEndpoint, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("token exchange failed (HTTP %d): %v", resp.StatusCode, errBody)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	return &Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}, nil
}

// --- Helper functions ---

func generateRandomString(length int) (string, error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b), nil
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}
