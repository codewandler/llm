// Package store provides token storage implementations.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codewandler/llm/provider/anthropic/claude"
)

// FileTokenStore persists tokens to JSON files in a directory.
type FileTokenStore struct {
	dir string
}

// fileToken is the JSON format for stored tokens.
type fileToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// NewFileTokenStore creates a store that saves tokens to dir.
// Creates the directory if it doesn't exist.
func NewFileTokenStore(dir string) (*FileTokenStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create token store directory: %w", err)
	}
	return &FileTokenStore{dir: dir}, nil
}

// DefaultDir returns the default credentials directory (~/.llmcli/credentials).
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".llmcli", "credentials"), nil
}

// Load retrieves a stored token by key.
func (s *FileTokenStore) Load(ctx context.Context, key string) (*claude.Token, error) {
	path := s.pathFor(key)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read token file: %w", err)
	}

	var ft fileToken
	if err := json.Unmarshal(data, &ft); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}

	return &claude.Token{
		AccessToken:  ft.AccessToken,
		RefreshToken: ft.RefreshToken,
		ExpiresAt:    ft.ExpiresAt,
	}, nil
}

// Save persists a token with the given key.
func (s *FileTokenStore) Save(ctx context.Context, key string, token *claude.Token) error {
	ft := fileToken{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}

	data, err := json.MarshalIndent(ft, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	path := s.pathFor(key)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	return nil
}

// Delete removes a stored token.
func (s *FileTokenStore) Delete(ctx context.Context, key string) error {
	path := s.pathFor(key)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete token file: %w", err)
	}
	return nil
}

// List returns all stored token keys.
func (s *FileTokenStore) List(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list token directory: %w", err)
	}

	var keys []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".json") {
			keys = append(keys, strings.TrimSuffix(name, ".json"))
		}
	}
	return keys, nil
}

func (s *FileTokenStore) pathFor(key string) string {
	return filepath.Join(s.dir, key+".json")
}

// Verify FileTokenStore implements TokenStore.
var _ claude.TokenStore = (*FileTokenStore)(nil)
