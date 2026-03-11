package anthropic

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type claudeCredentialsFile struct {
	ClaudeAiOAuth struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

func defaultClaudeCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", ".credentials.json")
}

func loadOAuthConfigFromPath(path string) (*OAuthConfig, error) {
	if path == "" {
		return nil, fmt.Errorf("claude code credentials path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read claude code credentials %q: %w", path, err)
	}

	var creds claudeCredentialsFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse claude code credentials %q: %w", path, err)
	}

	oauth := &OAuthConfig{
		Access:  creds.ClaudeAiOAuth.AccessToken,
		Refresh: creds.ClaudeAiOAuth.RefreshToken,
		Expires: creds.ClaudeAiOAuth.ExpiresAt,
	}

	if oauth.Access == "" {
		return nil, fmt.Errorf("claude code credentials %q missing claudeAiOauth.accessToken", path)
	}
	if oauth.Expires == 0 {
		return nil, fmt.Errorf("claude code credentials %q missing claudeAiOauth.expiresAt", path)
	}

	return oauth, nil
}

func (o *OAuthConfig) IsExpired() bool {
	if o == nil {
		return true
	}
	return time.Now().UnixMilli() >= o.Expires
}
