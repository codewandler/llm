package anthropic

import (
	"regexp"
	"sort"

	"github.com/codewandler/llm"
)

// Model ID constants for programmatic use.
const (
	// Claude 4.6 (current).
	ModelOpus   = "claude-opus-4-6"
	ModelSonnet = "claude-sonnet-4-6"

	// Claude 4.5 (Haiku latest).
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
// Source: https://www.anthropic.com/pricing (as of 2025).
type modelPricing struct {
	InputPrice       float64 // Regular input tokens
	OutputPrice      float64 // Output tokens
	CachedInputPrice float64 // Cached input tokens (prompt caching read) — ~0.1× input
	CacheWritePrice  float64 // Cache write tokens (prompt caching write) — ~1.25× input
}

// modelPricingRegistry maps model IDs to their pricing.
// Includes both dated and undated model IDs.
// This is the single source of truth — pricingPrefixes is derived from it at init.
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

// prefixEntry pairs a model-ID prefix with its pricing.
type prefixEntry struct {
	prefix  string
	pricing modelPricing
}

// pricingPrefixes is derived from modelPricingRegistry at init time.
// It is sorted longest-prefix-first so that more specific prefixes win.
var pricingPrefixes []prefixEntry

// dateSuffix matches a trailing 8-digit date segment (e.g. "-20250929").
var dateSuffix = regexp.MustCompile(`-\d{8}$`)

func init() {
	seen := make(map[string]modelPricing)
	for id, pricing := range modelPricingRegistry {
		prefix := dateSuffix.ReplaceAllString(id, "")
		seen[prefix] = pricing // last-write wins; all dated variants should be identical
	}
	pricingPrefixes = make([]prefixEntry, 0, len(seen))
	for prefix, pricing := range seen {
		pricingPrefixes = append(pricingPrefixes, prefixEntry{prefix, pricing})
	}
	// Sort longest-first so more-specific prefixes are tried first.
	sort.Slice(pricingPrefixes, func(i, j int) bool {
		return len(pricingPrefixes[i].prefix) > len(pricingPrefixes[j].prefix)
	})
}

// CalculateCost computes the total cost in USD for the given usage and model.
// Returns 0 if the model is unknown.
func CalculateCost(model string, usage *llm.Usage) float64 {
	if usage == nil {
		return 0
	}

	pricing, ok := modelPricingRegistry[model]
	if !ok {
		pricing, ok = matchPricingByPrefix(model)
		if !ok {
			return 0
		}
	}

	regularInput := usage.InputTokens - usage.CacheReadTokens - usage.CacheWriteTokens
	if regularInput < 0 {
		regularInput = 0
	}

	cost := (float64(regularInput) / 1_000_000) * pricing.InputPrice
	cost += (float64(usage.CacheReadTokens) / 1_000_000) * pricing.CachedInputPrice
	cost += (float64(usage.CacheWriteTokens) / 1_000_000) * pricing.CacheWritePrice
	cost += (float64(usage.OutputTokens) / 1_000_000) * pricing.OutputPrice

	return cost
}

// FillCost calculates the cost for the given usage and model and populates
// both the total Cost field and the granular breakdown fields on the usage struct.
// No-op if usage is nil or the model is unknown.
func FillCost(model string, usage *llm.Usage) {
	if usage == nil {
		return
	}

	pricing, ok := modelPricingRegistry[model]
	if !ok {
		pricing, ok = matchPricingByPrefix(model)
		if !ok {
			return
		}
	}

	regularInput := usage.InputTokens - usage.CacheReadTokens - usage.CacheWriteTokens
	if regularInput < 0 {
		regularInput = 0
	}

	usage.InputCost = (float64(regularInput) / 1_000_000) * pricing.InputPrice
	usage.CacheReadCost = (float64(usage.CacheReadTokens) / 1_000_000) * pricing.CachedInputPrice
	usage.CacheWriteCost = (float64(usage.CacheWriteTokens) / 1_000_000) * pricing.CacheWritePrice
	usage.OutputCost = (float64(usage.OutputTokens) / 1_000_000) * pricing.OutputPrice
	usage.Cost = usage.InputCost + usage.CacheReadCost + usage.CacheWriteCost + usage.OutputCost
}

// matchPricingByPrefix tries to find pricing for a model by matching prefixes
// derived from modelPricingRegistry. Handles future dated model versions
// (e.g., "claude-sonnet-4-6-20991231").
func matchPricingByPrefix(model string) (modelPricing, bool) {
	for _, p := range pricingPrefixes {
		if len(model) > len(p.prefix) && model[:len(p.prefix)] == p.prefix {
			return p.pricing, true
		}
	}
	return modelPricing{}, false
}
