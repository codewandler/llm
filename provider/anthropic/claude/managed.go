package claude

import (
	"context"
	"fmt"
	"sync"
)

// ManagedTokenProvider wraps a TokenStore with automatic refresh logic.
type ManagedTokenProvider struct {
	key         string
	store       TokenStore
	onRefreshed OnTokenRefreshed
	mu          sync.Mutex
	cached      *Token
}

// NewManagedTokenProvider creates a token provider with automatic refresh.
// The key identifies which token to load from the store.
// onRefreshed is called after successful token refresh (can be nil).
func NewManagedTokenProvider(key string, store TokenStore, onRefreshed OnTokenRefreshed) *ManagedTokenProvider {
	return &ManagedTokenProvider{
		key:         key,
		store:       store,
		onRefreshed: onRefreshed,
	}
}

// Token returns a valid token, refreshing if necessary.
func (p *ManagedTokenProvider) Token(ctx context.Context) (*Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Try cached token first
	if p.cached != nil && !p.cached.IsExpired() {
		return p.cached, nil
	}

	// Load from store
	token, err := p.store.Load(ctx, p.key)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	if token == nil {
		return nil, fmt.Errorf("no token found for key %q; run 'llmcli auth login claude --key=%s' to authenticate", p.key, p.key)
	}

	// Refresh if expired
	if token.IsExpired() {
		newToken, err := RefreshToken(ctx, token.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("refresh token: %w", err)
		}

		// Persist refreshed token
		if err := p.store.Save(ctx, p.key, newToken); err != nil {
			return nil, fmt.Errorf("save refreshed token: %w", err)
		}

		// Notify callback
		if p.onRefreshed != nil {
			if err := p.onRefreshed(ctx, p.key, newToken); err != nil {
				// Log but don't fail - token refresh succeeded
			}
		}

		token = newToken
	}

	p.cached = token
	return token, nil
}

// Refresh forces a token refresh regardless of expiration.
func (p *ManagedTokenProvider) Refresh(ctx context.Context) (*Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	token, err := p.store.Load(ctx, p.key)
	if err != nil || token == nil {
		return nil, fmt.Errorf("load token for refresh: %w", err)
	}

	newToken, err := RefreshToken(ctx, token.RefreshToken)
	if err != nil {
		return nil, err
	}

	if err := p.store.Save(ctx, p.key, newToken); err != nil {
		return nil, fmt.Errorf("save refreshed token: %w", err)
	}

	if p.onRefreshed != nil {
		p.onRefreshed(ctx, p.key, newToken)
	}

	p.cached = newToken
	return newToken, nil
}

// Key returns the token key this provider manages.
func (p *ManagedTokenProvider) Key() string {
	return p.key
}

// Invalidate clears the cached token, forcing a reload on next Token() call.
func (p *ManagedTokenProvider) Invalidate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cached = nil
}
