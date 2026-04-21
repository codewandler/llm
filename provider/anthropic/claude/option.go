package claude

import (
	"fmt"

	"github.com/codewandler/llm"
	providercore2 "github.com/codewandler/llm/internal/providercore"
	"github.com/codewandler/llm/provider/anthropic"
)

// Option configures the Claude provider.
type Option func(*Provider)

// WithLLMOptions applies one or more llm.Option values to the Claude provider.
// This allows using shared llm options (e.g. llm.WithHTTPClient) with this provider.
//
// Example:
//
//	claude.New(claude.WithLLMOptions(llm.WithHTTPClient(myClient)))
func WithLLMOptions(opts ...llm.Option) Option {
	return func(p *Provider) {
		cfg := llm.Apply(opts...)
		if cfg.HTTPClient != nil {
			p.client = cfg.HTTPClient
		}
		if cfg.Logger != nil {
			p.log = cfg.Logger
		}
		p.autoSystemCacheControl = anthropic.AutoSystemCacheControlFromOptions(opts)
	}
}

// WithTokenProvider sets a custom token provider.
func WithTokenProvider(tp TokenProvider) Option {
	return func(p *Provider) {
		p.tokenProvider = tp
	}
}

// WithManagedTokenProvider creates and sets a managed token provider.
func WithManagedTokenProvider(key string, store TokenStore, onRefreshed OnTokenRefreshed) Option {
	return func(p *Provider) {
		p.tokenProvider = NewManagedTokenProvider(key, store, onRefreshed)
	}
}

// WithLocalTokenProvider sets the local Claude Code token provider.
// This reads tokens from ~/.claude/.credentials.json (or CLAUDE_CONFIG_DIR)
// and automatically refreshes expired tokens, writing them back to the file.
func WithLocalTokenProvider() Option {
	return func(p *Provider) {
		tp, err := NewLocalTokenProvider()
		if err != nil {
			p.initErr = fmt.Errorf("WithLocalTokenProvider: %w", err)
			return
		}
		p.tokenProvider = tp
	}
}

// WithClaudeDir sets a custom Claude config directory for local credentials.
// The directory should contain .credentials.json file.
func WithClaudeDir(dir string) Option {
	return func(p *Provider) {
		tp, err := NewLocalTokenProviderWithDir(dir)
		if err != nil {
			p.initErr = fmt.Errorf("WithClaudeDir(%q): %w", dir, err)
			return
		}
		p.tokenProvider = tp
	}
}

// WithBaseURL sets a custom base URL for the API.
func WithBaseURL(url string) Option {
	return func(p *Provider) {
		p.baseURL = url
	}
}

// WithAutoSystemCacheControl enables provider-level automatic cache_control on
// the injected Claude SDK system block. Empty ttl defaults to 1h.
func WithAutoSystemCacheControl(ttl string) Option {
	return func(p *Provider) {
		if ttl == "" {
			ttl = "1h"
		}
		p.autoSystemCacheControl = &providercore2.MessagesCacheControl{Type: "ephemeral", TTL: ttl}
	}
}
