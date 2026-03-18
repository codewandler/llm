package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// defaultClaudeDirName is the default directory name for Claude config.
	defaultClaudeDirName = ".claude"

	// credentialsFileName is the name of the credentials file.
	credentialsFileName = ".credentials.json"

	// localTokenKey is the key used for the single token in local storage.
	localTokenKey = "default"
)

// localCredentials matches the ~/.claude/.credentials.json format.
type localCredentials struct {
	ClaudeAiOauth *localOAuthData `json:"claudeAiOauth"`
}

type localOAuthData struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"` // Unix timestamp in milliseconds
}

// LocalTokenStore implements TokenStore for the local Claude Code credentials file.
// It reads and writes tokens to ~/.claude/.credentials.json.
type LocalTokenStore struct {
	path string
}

// DefaultClaudeDir returns the Claude config directory.
// Checks CLAUDE_CONFIG_DIR env var first, falls back to ~/.claude.
func DefaultClaudeDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, defaultClaudeDirName), nil
}

// NewLocalTokenStore creates a TokenStore that uses ~/.claude/.credentials.json.
func NewLocalTokenStore() (*LocalTokenStore, error) {
	path, err := defaultLocalCredentialsPath()
	if err != nil {
		return nil, err
	}
	return NewLocalTokenStoreWithPath(path)
}

// NewLocalTokenStoreWithPath creates a LocalTokenStore with a custom credentials file path.
func NewLocalTokenStoreWithPath(path string) (*LocalTokenStore, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("claude credentials not found at %s: %w", path, err)
	}
	return &LocalTokenStore{path: path}, nil
}

// NewLocalTokenStoreWithDir creates a LocalTokenStore using the specified directory.
// The directory should contain .credentials.json file.
func NewLocalTokenStoreWithDir(dir string) (*LocalTokenStore, error) {
	path := filepath.Join(dir, credentialsFileName)
	return NewLocalTokenStoreWithPath(path)
}

// LocalTokenProviderAvailable returns true if local Claude credentials exist.
// Uses CLAUDE_CONFIG_DIR env var if set, otherwise ~/.claude.
func LocalTokenProviderAvailable() bool {
	dir, err := DefaultClaudeDir()
	if err != nil {
		return false
	}
	return LocalTokenProviderAvailableInDir(dir)
}

// LocalTokenProviderAvailableInDir checks if credentials exist in the specified directory.
func LocalTokenProviderAvailableInDir(dir string) bool {
	path := filepath.Join(dir, credentialsFileName)
	_, err := os.Stat(path)
	return err == nil
}

// NewLocalTokenProvider creates a TokenProvider that reads from ~/.claude/.credentials.json.
// It returns a ManagedTokenProvider backed by LocalTokenStore.
// Uses CLAUDE_CONFIG_DIR env var if set, otherwise ~/.claude.
func NewLocalTokenProvider() (*ManagedTokenProvider, error) {
	store, err := NewLocalTokenStore()
	if err != nil {
		return nil, err
	}
	return NewManagedTokenProvider(localTokenKey, store, nil), nil
}

// NewLocalTokenProviderWithPath creates a LocalTokenProvider with a custom credentials file path.
func NewLocalTokenProviderWithPath(path string) (*ManagedTokenProvider, error) {
	store, err := NewLocalTokenStoreWithPath(path)
	if err != nil {
		return nil, err
	}
	return NewManagedTokenProvider(localTokenKey, store, nil), nil
}

// NewLocalTokenProviderWithDir creates a LocalTokenProvider using the specified directory.
// The directory should contain .credentials.json file.
func NewLocalTokenProviderWithDir(dir string) (*ManagedTokenProvider, error) {
	store, err := NewLocalTokenStoreWithDir(dir)
	if err != nil {
		return nil, err
	}
	return NewManagedTokenProvider(localTokenKey, store, nil), nil
}

// defaultLocalCredentialsPath returns the full path to the credentials file.
// Uses CLAUDE_CONFIG_DIR env var if set, otherwise ~/.claude/.credentials.json.
func defaultLocalCredentialsPath() (string, error) {
	dir, err := DefaultClaudeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialsFileName), nil
}

// --- TokenStore implementation ---

// Load retrieves the token from the credentials file.
// The key parameter is ignored since the file contains only one token.
func (s *LocalTokenStore) Load(ctx context.Context, key string) (*Token, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read credentials file: %w", err)
	}

	var creds localCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse credentials file: %w", err)
	}

	if creds.ClaudeAiOauth == nil {
		return nil, nil // No token found
	}

	oauth := creds.ClaudeAiOauth
	if oauth.AccessToken == "" {
		return nil, nil // No valid token
	}

	return &Token{
		AccessToken:  oauth.AccessToken,
		RefreshToken: oauth.RefreshToken,
		ExpiresAt:    time.UnixMilli(oauth.ExpiresAt),
	}, nil
}

// Save persists the token to the credentials file.
// The key parameter is ignored since the file contains only one token.
func (s *LocalTokenStore) Save(ctx context.Context, key string, token *Token) error {
	// Read existing file to preserve other fields
	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read credentials file: %w", err)
	}

	var creds localCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return fmt.Errorf("parse credentials file: %w", err)
	}

	if creds.ClaudeAiOauth == nil {
		creds.ClaudeAiOauth = &localOAuthData{}
	}

	// Update token fields
	creds.ClaudeAiOauth.AccessToken = token.AccessToken
	creds.ClaudeAiOauth.RefreshToken = token.RefreshToken
	creds.ClaudeAiOauth.ExpiresAt = token.ExpiresAt.UnixMilli()

	// Write back
	newData, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	if err := os.WriteFile(s.path, newData, 0600); err != nil {
		return fmt.Errorf("write credentials file: %w", err)
	}

	return nil
}

// Delete is a no-op for local storage (we don't delete the Claude credentials file).
func (s *LocalTokenStore) Delete(ctx context.Context, key string) error {
	return nil
}

// List returns the single key used for local storage.
func (s *LocalTokenStore) List(ctx context.Context) ([]string, error) {
	// Check if token exists
	token, err := s.Load(ctx, localTokenKey)
	if err != nil {
		return nil, err
	}
	if token == nil {
		return []string{}, nil
	}
	return []string{localTokenKey}, nil
}

// Ensure LocalTokenStore implements TokenStore
var _ TokenStore = (*LocalTokenStore)(nil)
