package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	authRelPath       = ".codex/auth.json"
	tokenEndpoint     = "https://auth.openai.com/oauth/token"
	clientID          = "app_EMoamEEZ73f0CkXaXp7hrann"
	tokenExpiryBuffer = 5 * time.Minute
	accountIDHeader   = "ChatGPT-Account-ID"
	codexBetaHeader   = "OpenAI-Beta"
	codexBetaValue    = "responses=experimental"
	codexOriginator   = "codex_cli_rs"
	chatGPTAuthMode   = "chatgpt"
)

type tokenStore struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id"`
}

type authFile struct {
	AuthMode    string     `json:"auth_mode"`
	APIKey      *string    `json:"OPENAI_API_KEY"`
	Tokens      tokenStore `json:"tokens"`
	LastRefresh time.Time  `json:"last_refresh"`
}

type Auth struct {
	mu         sync.Mutex
	auth       authFile
	path       string
	expiry     time.Time
	httpClient *http.Client
}

func LoadAuth() (*Auth, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("codex: get home dir: %w", err)
	}
	return loadAuthFrom(filepath.Join(home, authRelPath))
}

func LocalAvailable() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	a, err := loadAuthFrom(filepath.Join(home, authRelPath))
	return err == nil && (a.auth.Tokens.AccessToken != "" || a.auth.Tokens.RefreshToken != "")
}

func loadAuthFrom(path string) (*Auth, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("codex: read %s: %w", path, err)
	}
	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("codex: parse auth file: %w", err)
	}
	if auth.AuthMode != "" && auth.AuthMode != chatGPTAuthMode {
		return nil, fmt.Errorf("codex: unsupported auth mode %q", auth.AuthMode)
	}
	if auth.Tokens.AccessToken == "" && auth.Tokens.RefreshToken == "" {
		return nil, fmt.Errorf("codex: no tokens in %s", path)
	}
	a := &Auth{auth: auth, path: path, httpClient: http.DefaultClient}
	if exp, err := jwtExpiry(auth.Tokens.AccessToken); err == nil {
		a.expiry = exp
	}
	return a, nil
}

func (a *Auth) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.expiry.IsZero() && time.Now().Add(tokenExpiryBuffer).Before(a.expiry) {
		return a.auth.Tokens.AccessToken, nil
	}
	if a.expiry.IsZero() && a.auth.Tokens.AccessToken != "" && a.auth.Tokens.RefreshToken == "" {
		return a.auth.Tokens.AccessToken, nil
	}
	if a.auth.Tokens.RefreshToken == "" {
		if a.auth.Tokens.AccessToken != "" {
			return a.auth.Tokens.AccessToken, nil
		}
		return "", fmt.Errorf("codex: no refresh token and access token is empty")
	}
	return a.refresh(ctx)
}

func (a *Auth) AccountID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.auth.Tokens.AccountID
}

func (a *Auth) refresh(ctx context.Context) (string, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {a.auth.Tokens.RefreshToken},
		"client_id":     {clientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("codex: build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("codex: token refresh: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
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
		return "", fmt.Errorf("codex: empty access token in refresh response (status %d)", resp.StatusCode)
	}

	a.auth.Tokens.AccessToken = result.AccessToken
	if result.RefreshToken != "" {
		a.auth.Tokens.RefreshToken = result.RefreshToken
	}
	if result.ExpiresIn > 0 {
		a.expiry = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	} else if exp, err := jwtExpiry(result.AccessToken); err == nil {
		a.expiry = exp
	} else {
		a.expiry = time.Time{}
	}

	a.saveLocked()
	return result.AccessToken, nil
}

func (a *Auth) saveLocked() {
	a.auth.LastRefresh = time.Now().UTC()
	data, err := json.MarshalIndent(a.auth, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(a.path, data, 0o600)
}

func jwtExpiry(token string) (time.Time, error) {
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
