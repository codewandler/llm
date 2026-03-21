package anthropic

import (
	"strings"

	"github.com/codewandler/llm"
)

// Model ID constants for programmatic use.
const (
	// Claude 4.6 (current)
	ModelOpus   = "claude-opus-4-6"
	ModelSonnet = "claude-sonnet-4-6"

	// Claude 4.5 (Haiku latest)
	ModelHaiku = "claude-haiku-4-5-20251001"
)

// ModelAliases maps short alias names to full model IDs.
// These are used by the auto package for provider-prefixed resolution (e.g., "claude/sonnet").
var ModelAliases = map[string]string{
	"opus":   ModelOpus,
	"sonnet": ModelSonnet,
	"haiku":  ModelHaiku,
}

// Model pricing in USD per million tokens.
// Source: https://www.anthropic.com/pricing (as of 2025)
type modelPricing struct {
	InputPrice       float64 // Regular input tokens
	OutputPrice      float64 // Output tokens
	CachedInputPrice float64 // Cached input tokens (prompt caching read) — ~0.1× input
	CacheWritePrice  float64 // Cache write tokens (prompt caching write) — ~1.25× input
}

// modelPricingRegistry maps model IDs to their pricing.
// Includes both dated and undated model IDs.
var modelPricingRegistry = map[string]modelPricing{
	// Claude 4.6 (current)
	"claude-opus-4-6":   {InputPrice: 5.0, OutputPrice: 25.0, CachedInputPrice: 0.50, CacheWritePrice: 6.25},
	"claude-sonnet-4-6": {InputPrice: 3.0, OutputPrice: 15.0, CachedInputPrice: 0.30, CacheWritePrice: 3.75},

	// Claude 4.5
	"claude-opus-4-5":            {InputPrice: 5.0, OutputPrice: 25.0, CachedInputPrice: 0.50, CacheWritePrice: 6.25},
	"claude-opus-4-5-20251101":   {InputPrice: 5.0, OutputPrice: 25.0, CachedInputPrice: 0.50, CacheWritePrice: 6.25},
	"claude-sonnet-4-5":          {InputPrice: 3.0, OutputPrice: 15.0, CachedInputPrice: 0.30, CacheWritePrice: 3.75},
	"claude-sonnet-4-5-20250929": {InputPrice: 3.0, OutputPrice: 15.0, CachedInputPrice: 0.30, CacheWritePrice: 3.75},
	"claude-haiku-4-5":           {InputPrice: 1.0, OutputPrice: 5.0, CachedInputPrice: 0.10, CacheWritePrice: 1.25},
	"claude-haiku-4-5-20251001":  {InputPrice: 1.0, OutputPrice: 5.0, CachedInputPrice: 0.10, CacheWritePrice: 1.25},

	// Claude 4.1
	"claude-opus-4-1":          {InputPrice: 15.0, OutputPrice: 75.0, CachedInputPrice: 1.50, CacheWritePrice: 18.75},
	"claude-opus-4-1-20250805": {InputPrice: 15.0, OutputPrice: 75.0, CachedInputPrice: 1.50, CacheWritePrice: 18.75},

	// Claude 4.0
	"claude-opus-4":            {InputPrice: 15.0, OutputPrice: 75.0, CachedInputPrice: 1.50, CacheWritePrice: 18.75},
	"claude-opus-4-20250514":   {InputPrice: 15.0, OutputPrice: 75.0, CachedInputPrice: 1.50, CacheWritePrice: 18.75},
	"claude-sonnet-4":          {InputPrice: 3.0, OutputPrice: 15.0, CachedInputPrice: 0.30, CacheWritePrice: 3.75},
	"claude-sonnet-4-20250514": {InputPrice: 3.0, OutputPrice: 15.0, CachedInputPrice: 0.30, CacheWritePrice: 3.75},

	// Claude 3.5
	"claude-3-5-sonnet-20241022": {InputPrice: 3.0, OutputPrice: 15.0, CachedInputPrice: 0.30, CacheWritePrice: 3.75},
	"claude-3-5-sonnet-20240620": {InputPrice: 3.0, OutputPrice: 15.0, CachedInputPrice: 0.30, CacheWritePrice: 3.75},
	"claude-3-5-haiku-20241022":  {InputPrice: 1.0, OutputPrice: 5.0, CachedInputPrice: 0.10, CacheWritePrice: 1.25},

	// Claude 3
	"claude-3-opus-20240229":   {InputPrice: 15.0, OutputPrice: 75.0, CachedInputPrice: 1.50, CacheWritePrice: 18.75},
	"claude-3-sonnet-20240229": {InputPrice: 3.0, OutputPrice: 15.0, CachedInputPrice: 0.30, CacheWritePrice: 3.75},
	"claude-3-haiku-20240307":  {InputPrice: 0.25, OutputPrice: 1.25, CachedInputPrice: 0.03, CacheWritePrice: 0.3125},
}

// CalculateCost computes the cost in USD for the given usage and model.
// Returns 0 if the model is unknown.
func CalculateCost(model string, usage *llm.Usage) float64 {
	if usage == nil {
		return 0
	}

	pricing, ok := modelPricingRegistry[model]
	if !ok {
		// Try to match by prefix for unknown dated versions
		pricing, ok = matchPricingByPrefix(model)
		if !ok {
			return 0
		}
	}

	// Regular input tokens (non-cached, non-written)
	regularInput := usage.InputTokens - usage.CachedTokens - usage.CacheWriteTokens
	if regularInput < 0 {
		regularInput = 0
	}

	cost := (float64(regularInput) / 1_000_000) * pricing.InputPrice
	cost += (float64(usage.CachedTokens) / 1_000_000) * pricing.CachedInputPrice
	cost += (float64(usage.CacheWriteTokens) / 1_000_000) * pricing.CacheWritePrice
	cost += (float64(usage.OutputTokens) / 1_000_000) * pricing.OutputPrice

	return cost
}

// matchPricingByPrefix tries to find pricing for a model by matching prefixes.
// This handles future dated model versions (e.g., "claude-sonnet-4-6-20260101").
func matchPricingByPrefix(model string) (modelPricing, bool) {
	// Try progressively shorter prefixes
	prefixes := []struct {
		prefix  string
		pricing modelPricing
	}{
		// Opus models (most expensive first)
		{"claude-opus-4-6", modelPricing{5.0, 25.0, 0.50, 6.25}},
		{"claude-opus-4-5", modelPricing{5.0, 25.0, 0.50, 6.25}},
		{"claude-opus-4-1", modelPricing{15.0, 75.0, 1.50, 18.75}},
		{"claude-opus-4", modelPricing{15.0, 75.0, 1.50, 18.75}},
		{"claude-opus-3", modelPricing{15.0, 75.0, 1.50, 18.75}},

		// Sonnet models
		{"claude-sonnet-4-6", modelPricing{3.0, 15.0, 0.30, 3.75}},
		{"claude-sonnet-4-5", modelPricing{3.0, 15.0, 0.30, 3.75}},
		{"claude-sonnet-4", modelPricing{3.0, 15.0, 0.30, 3.75}},
		{"claude-3-5-sonnet", modelPricing{3.0, 15.0, 0.30, 3.75}},
		{"claude-3-sonnet", modelPricing{3.0, 15.0, 0.30, 3.75}},

		// Haiku models (cheapest)
		{"claude-haiku-4-5", modelPricing{1.0, 5.0, 0.10, 1.25}},
		{"claude-3-5-haiku", modelPricing{1.0, 5.0, 0.10, 1.25}},
		{"claude-3-haiku", modelPricing{0.25, 1.25, 0.03, 0.3125}},
	}

	for _, p := range prefixes {
		if strings.HasPrefix(model, p.prefix) {
			return p.pricing, true
		}
	}

	return modelPricing{}, false
}
