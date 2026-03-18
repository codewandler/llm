// Package claude provides an Anthropic provider using Claude OAuth tokens.
package claude

import (
	"context"
	"time"
)

// Token represents OAuth tokens with expiration.
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// IsExpired returns true if the token is expired or will expire within 30 seconds.
func (t *Token) IsExpired() bool {
	return t.IsExpiredWithBuffer(30 * time.Second)
}

// IsExpiredWithBuffer returns true if token expires within the given buffer.
func (t *Token) IsExpiredWithBuffer(buffer time.Duration) bool {
	if t == nil || t.AccessToken == "" {
		return true
	}
	return time.Now().Add(buffer).After(t.ExpiresAt)
}

// TokenProvider supplies OAuth tokens to the provider.
// Implementations handle storage, refresh orchestration, and persistence.
type TokenProvider interface {
	// Token returns the current token, refreshing if necessary.
	// The provider calls this before each API request.
	Token(ctx context.Context) (*Token, error)
}

// TokenRefresher extends TokenProvider with explicit refresh capability.
type TokenRefresher interface {
	TokenProvider

	// Refresh forces a token refresh using the refresh token.
	// Returns the new token.
	Refresh(ctx context.Context) (*Token, error)
}

// TokenStore persists tokens.
// Implement this for your storage backend (file, database, keychain, etc.).
type TokenStore interface {
	// Load retrieves a stored token. Returns nil, nil if not found.
	Load(ctx context.Context, key string) (*Token, error)

	// Save persists a token.
	Save(ctx context.Context, key string, token *Token) error

	// Delete removes a stored token.
	Delete(ctx context.Context, key string) error

	// List returns all stored token keys.
	List(ctx context.Context) ([]string, error)
}

// OnTokenRefreshed is called when a token is successfully refreshed.
// Use this to trigger side effects like logging or notifications.
type OnTokenRefreshed func(ctx context.Context, key string, newToken *Token) error

// staticTokenProvider wraps a static token.
type staticTokenProvider struct {
	token *Token
}

// NewStaticTokenProvider creates a TokenProvider that always returns the same token.
// Useful for testing or when tokens are managed externally.
func NewStaticTokenProvider(token *Token) TokenProvider {
	return &staticTokenProvider{token: token}
}

func (p *staticTokenProvider) Token(ctx context.Context) (*Token, error) {
	return p.token, nil
}
