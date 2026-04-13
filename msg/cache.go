package msg

import "time"

type CacheTTL string

const (
	CacheTTLUnspecified CacheTTL = ""
	CacheTTL5m          CacheTTL = "5m"
	CacheTTL1h          CacheTTL = "1h"
	CacheTTLDefault              = CacheTTL5m
)

func (ttl CacheTTL) String() string { return string(ttl) }
func (ttl CacheTTL) Duration() time.Duration {
	switch ttl {
	case CacheTTL5m:
		return 5 * time.Minute
	case CacheTTL1h:
		return 1 * time.Hour
	default:
		return 0
	}
}

func (ttl CacheTTL) applyCacheOption(hint *CacheHint) { hint.TTL = ttl.String() }

// CacheHint requests provider-side prompt caching for a message or request.
// It is a provider-neutral instruction: Anthropic and Bedrock translate it to
// explicit cache breakpoints on content blocks; OpenAI caching is always
// automatic and ignores per-message hints, but honours TTL on
// Request.CacheHint.
type CacheHint struct {
	// Enabled marks this content as a cache breakpoint candidate.
	// For Anthropic/Bedrock: emits cache_control / cachePoint at this position.
	// For OpenAI: no-op (caching is automatic).
	Enabled bool `json:"enabled,omitempty"`

	// TTL requests a specific cache duration.
	// Valid values: "" (provider default, typically 5m), "5m", "1h".
	// The "1h" option requires a supporting model (Claude Haiku/Sonnet/Opus 4.5+).
	TTL string `json:"ttl,omitempty"`
}

func NewCacheHint(opts ...CacheOpt) *CacheHint {
	hint := &CacheHint{Enabled: true, TTL: string(CacheTTLDefault)}
	for _, opt := range opts {
		opt.applyCacheOption(hint)
	}
	return hint
}

type CacheOpt interface {
	applyCacheOption(*CacheHint)
}
