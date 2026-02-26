package llm

import (
	"context"
	"fmt"
	"os"
)

// Option configures provider options.
type Option func(*Options)

// Options holds configuration shared across providers.
type Options struct {
	// BaseURL is the base URL for the provider's API.
	BaseURL string

	// APIKeyFunc returns the API key for authentication.
	// It is called on each CreateStream() call, allowing for lazy/dynamic resolution.
	APIKeyFunc func(ctx context.Context) (string, error)
}

// Apply applies all options to a new Options struct and returns it.
func Apply(opts ...Option) *Options {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithBaseURL sets a custom base URL for the provider.
func WithBaseURL(url string) Option {
	return func(o *Options) {
		o.BaseURL = url
	}
}

// WithAPIKey sets a static API key.
func WithAPIKey(key string) Option {
	return func(o *Options) {
		o.APIKeyFunc = func(ctx context.Context) (string, error) {
			return key, nil
		}
	}
}

// WithAPIKeyFunc sets a dynamic API key resolver.
// The function is called on each CreateStream() call, enabling:
//   - Lazy key resolution (fetch from secret manager on first use)
//   - Key rotation (fetch fresh key each time)
//   - Context-aware resolution (respect timeouts/cancellation)
func WithAPIKeyFunc(f func(ctx context.Context) (string, error)) Option {
	return func(o *Options) {
		o.APIKeyFunc = f
	}
}

// APIKeyFromEnv returns an Option that reads the API key from environment variables.
// It tries each candidate in order, returning the first non-empty value.
// Returns an error at call time if none of the candidates are set.
func APIKeyFromEnv(candidates ...string) Option {
	return func(o *Options) {
		o.APIKeyFunc = func(ctx context.Context) (string, error) {
			for _, name := range candidates {
				if key := os.Getenv(name); key != "" {
					return key, nil
				}
			}
			return "", fmt.Errorf("API key not found in environment variables: %v", candidates)
		}
	}
}

// ResolveAPIKey calls the APIKeyFunc to get the API key.
// Returns an empty string (no error) if no APIKeyFunc was configured.
func (o *Options) ResolveAPIKey(ctx context.Context) (string, error) {
	if o.APIKeyFunc == nil {
		return "", nil
	}
	return o.APIKeyFunc(ctx)
}
