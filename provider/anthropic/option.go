package anthropic

import (
	"github.com/codewandler/llm"
	providercore2 "github.com/codewandler/llm/internal/providercore"
)

// WithAnthropicAutoSystemCacheControl enables provider-level automatic
// cache_control on the first system block. Empty ttl defaults to 1h.
func WithAnthropicAutoSystemCacheControl(ttl string) llm.Option {
	if ttl == "" {
		ttl = "1h"
	}
	base := func(o *llm.Options) {}
	return registerAnthropicOption(base, func(cfg *anthropicExtraOptions) {
		cfg.autoSystemCacheControl = true
		cfg.autoSystemCacheTTL = ttl
	})
}

func AutoSystemCacheControlFromOptions(opts []llm.Option) *providercore2.MessagesCacheControl {
	cfg := &anthropicExtraOptions{}
	for _, opt := range opts {
		applyAnthropicExtraOption(cfg, opt)
	}
	if !cfg.autoSystemCacheControl {
		return nil
	}
	ttl := cfg.autoSystemCacheTTL
	if ttl == "" {
		ttl = "1h"
	}
	return &providercore2.MessagesCacheControl{Type: "ephemeral", TTL: ttl}
}

type anthropicExtraOptions struct {
	autoSystemCacheControl bool
	autoSystemCacheTTL     string
}

type anthropicExtraOption func(*anthropicExtraOptions)

func applyAnthropicExtraOption(dst *anthropicExtraOptions, opt llm.Option) {
	if opt == nil || dst == nil {
		return
	}
	if wrapped, ok := anthropicOptionRegistry[llm.FuncKey(opt)]; ok {
		wrapped(dst)
	}
}

var anthropicOptionRegistry = map[string]anthropicExtraOption{}

func registerAnthropicOption(opt llm.Option, extra anthropicExtraOption) llm.Option {
	anthropicOptionRegistry[llm.FuncKey(opt)] = extra
	return opt
}
