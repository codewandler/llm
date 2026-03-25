package minimax

import "github.com/codewandler/llm"

// Model ToolCallID constants for programmatic use.
const (
	ModelM27          = "MiniMax-M2.7"
	ModelM27Highspeed = "MiniMax-M2.7-highspeed"
	ModelM25          = "MiniMax-M2.5"
	ModelM25Highspeed = "MiniMax-M2.5-highspeed"
	ModelM21          = "MiniMax-M2.1"
	ModelM21Highspeed = "MiniMax-M2.1-highspeed"
	ModelM2           = "MiniMax-M2"
)

// ModelAliases maps short alias names to full model IDs.
// Used by the auto package for provider-prefixed resolution (e.g., "minimax/fast").
var ModelAliases = map[string]string{
	"minimax":      ModelM27,
	"minimax:fast": ModelM27,
	"minimax:2.7":  ModelM27,
	"minimax:2.5":  ModelM25,
	"minimax:2.1":  ModelM21,
	"minimax:2":    ModelM2,
}

// modelPricing holds USD per million tokens for a model.
// Source: https://platform.minimax.io/docs/guides/pricing-paygo
type modelPricing struct {
	InputPrice      float64
	OutputPrice     float64
	CacheReadPrice  float64
	CacheWritePrice float64
}

// modelPricingRegistry maps model IDs to their pricing in USD per million tokens.
// Standard models: input $0.30/M, output $1.20/M.
// Highspeed variants: input $0.60/M, output $2.40/M (2× the standard rate).
// Source: https://platform.minimax.io/docs/guides/pricing-paygo
var modelPricingRegistry = map[string]modelPricing{
	ModelM27:          {InputPrice: 0.3, OutputPrice: 1.2, CacheReadPrice: 0.06, CacheWritePrice: 0.375},
	ModelM27Highspeed: {InputPrice: 0.6, OutputPrice: 2.4, CacheReadPrice: 0.06, CacheWritePrice: 0.375},
	ModelM25:          {InputPrice: 0.3, OutputPrice: 1.2, CacheReadPrice: 0.03, CacheWritePrice: 0.375},
	ModelM25Highspeed: {InputPrice: 0.6, OutputPrice: 2.4, CacheReadPrice: 0.03, CacheWritePrice: 0.375},
	ModelM21:          {InputPrice: 0.3, OutputPrice: 1.2, CacheReadPrice: 0.03, CacheWritePrice: 0.375},
	ModelM21Highspeed: {InputPrice: 0.6, OutputPrice: 2.4, CacheReadPrice: 0.03, CacheWritePrice: 0.375},
	ModelM2:           {InputPrice: 0.3, OutputPrice: 1.2, CacheReadPrice: 0.03, CacheWritePrice: 0.375},
}

// FillCost calculates cost for the given usage and model and populates usage cost fields.
// Handles input, output, cache read, and cache write token costs.
// InputCost is calculated only for non-cache tokens (total input minus cache reads/writes).
func FillCost(model string, usage *llm.Usage) {
	if usage == nil {
		return
	}
	pricing, ok := modelPricingRegistry[model]
	if !ok {
		return
	}
	// Calculate non-cache input tokens to avoid double-counting with cache costs.
	regularInput := usage.InputTokens - usage.CacheReadTokens - usage.CacheWriteTokens
	if regularInput < 0 {
		regularInput = 0
	}
	usage.InputCost = (float64(regularInput) / 1_000_000) * pricing.InputPrice
	usage.CacheReadCost = (float64(usage.CacheReadTokens) / 1_000_000) * pricing.CacheReadPrice
	usage.CacheWriteCost = (float64(usage.CacheWriteTokens) / 1_000_000) * pricing.CacheWritePrice
	usage.OutputCost = (float64(usage.OutputTokens) / 1_000_000) * pricing.OutputPrice
	usage.Cost = usage.InputCost + usage.CacheReadCost + usage.CacheWriteCost + usage.OutputCost
}
