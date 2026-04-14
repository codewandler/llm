package usage

import (
	"strings"

	"github.com/codewandler/llm/modeldb"
)

// Pricing holds per-token rates in USD per million tokens.
type Pricing struct {
	Input       float64 `json:"input"`
	Output      float64 `json:"output"`
	Reasoning   float64 `json:"reasoning,omitempty"` // 0 = same rate as Output
	CachedInput float64 `json:"cached_input,omitempty"`
	CacheWrite  float64 `json:"cache_write,omitempty"`
}

// CalcCost computes a Cost from token items and pricing.
// Each item contributes exactly one cost component.
// KindReasoning falls back to p.Output rate when p.Reasoning == 0.
// Sets Cost.Source = "calculated".
func CalcCost(items TokenItems, p Pricing) Cost {
	var c Cost
	for _, item := range items {
		switch item.Kind {
		case KindInput:
			c.Input = float64(item.Count) / 1_000_000 * p.Input
		case KindCacheRead:
			c.CacheRead = float64(item.Count) / 1_000_000 * p.CachedInput
		case KindCacheWrite:
			c.CacheWrite = float64(item.Count) / 1_000_000 * p.CacheWrite
		case KindOutput:
			c.Output = float64(item.Count) / 1_000_000 * p.Output
		case KindReasoning:
			rate := p.Reasoning
			if rate == 0 {
				rate = p.Output // current providers price reasoning at output rate
			}
			c.Reasoning = float64(item.Count) / 1_000_000 * rate
		}
	}
	c.Total = c.Input + c.CacheRead + c.CacheWrite + c.Output + c.Reasoning
	c.Source = "calculated"
	return c
}

// PricingEntry associates a provider+model pair with its pricing.
type PricingEntry struct {
	Provider string
	Model    string // exact ID or prefix (e.g. "claude-sonnet-4-6")
	Pricing  Pricing
}

// KnownPricing is the built-in registry of well-known model prices.
// Entries from all providers are kept here; providers no longer carry their own tables.
// Source: provider pricing pages (as of 2026-04-14).
var KnownPricing = []PricingEntry{
	// -------------------------------------------------------------------------
	// Anthropic — extracted from provider/anthropic/models.go
	// -------------------------------------------------------------------------
	// Claude 4.6 (current)
	{"anthropic", "claude-opus-4-6", Pricing{Input: 5.0, Output: 25.0, CachedInput: 0.50, CacheWrite: 6.25}},
	{"anthropic", "claude-sonnet-4-6", Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75}},
	// Claude 4.5
	{"anthropic", "claude-opus-4-5", Pricing{Input: 5.0, Output: 25.0, CachedInput: 0.50, CacheWrite: 6.25}},
	{"anthropic", "claude-opus-4-5-20251101", Pricing{Input: 5.0, Output: 25.0, CachedInput: 0.50, CacheWrite: 6.25}},
	{"anthropic", "claude-sonnet-4-5", Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75}},
	{"anthropic", "claude-sonnet-4-5-20250929", Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75}},
	{"anthropic", "claude-haiku-4-5", Pricing{Input: 1.0, Output: 5.0, CachedInput: 0.10, CacheWrite: 1.25}},
	{"anthropic", "claude-haiku-4-5-20251001", Pricing{Input: 1.0, Output: 5.0, CachedInput: 0.10, CacheWrite: 1.25}},
	// Claude 4.1
	{"anthropic", "claude-opus-4-1", Pricing{Input: 15.0, Output: 75.0, CachedInput: 1.50, CacheWrite: 18.75}},
	{"anthropic", "claude-opus-4-1-20250805", Pricing{Input: 15.0, Output: 75.0, CachedInput: 1.50, CacheWrite: 18.75}},
	// Claude 4.0
	{"anthropic", "claude-opus-4", Pricing{Input: 15.0, Output: 75.0, CachedInput: 1.50, CacheWrite: 18.75}},
	{"anthropic", "claude-opus-4-20250514", Pricing{Input: 15.0, Output: 75.0, CachedInput: 1.50, CacheWrite: 18.75}},
	{"anthropic", "claude-sonnet-4", Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75}},
	{"anthropic", "claude-sonnet-4-20250514", Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75}},
	// Claude 3.5
	{"anthropic", "claude-3-5-sonnet-20241022", Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75}},
	{"anthropic", "claude-3-5-sonnet-20240620", Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75}},
	{"anthropic", "claude-3-5-haiku-20241022", Pricing{Input: 1.0, Output: 5.0, CachedInput: 0.10, CacheWrite: 1.25}},
	// Claude 3
	{"anthropic", "claude-3-opus-20240229", Pricing{Input: 15.0, Output: 75.0, CachedInput: 1.50, CacheWrite: 18.75}},
	{"anthropic", "claude-3-sonnet-20240229", Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75}},
	{"anthropic", "claude-3-haiku-20240307", Pricing{Input: 0.25, Output: 1.25, CachedInput: 0.03, CacheWrite: 0.3125}},

	// -------------------------------------------------------------------------
	// OpenAI — extracted from provider/openai/models.go
	// -------------------------------------------------------------------------
	// GPT-5.4 series
	{"openai", "gpt-5.4", Pricing{Input: 2.50, Output: 15.00, CachedInput: 0.25}},
	{"openai", "gpt-5.4-mini", Pricing{Input: 0.75, Output: 4.50, CachedInput: 0.075}},
	{"openai", "gpt-5.4-nano", Pricing{Input: 0.20, Output: 1.25, CachedInput: 0.02}},
	{"openai", "gpt-5.4-pro", Pricing{Input: 30.00, Output: 180.00}},
	// GPT-5.3 series
	{"openai", "gpt-5.3-codex", Pricing{Input: 1.75, Output: 14.00, CachedInput: 0.175}},
	// GPT-5.2 series
	{"openai", "gpt-5.2", Pricing{Input: 1.75, Output: 14.00, CachedInput: 0.175}},
	{"openai", "gpt-5.2-pro", Pricing{Input: 30.00, Output: 180.00}},
	{"openai", "gpt-5.2-codex", Pricing{Input: 1.75, Output: 14.00, CachedInput: 0.175}},
	// GPT-5.1 series
	{"openai", "gpt-5.1", Pricing{Input: 1.25, Output: 10.00, CachedInput: 0.125}},
	{"openai", "gpt-5.1-codex", Pricing{Input: 1.25, Output: 10.00, CachedInput: 0.125}},
	{"openai", "gpt-5.1-codex-max", Pricing{Input: 1.25, Output: 10.00, CachedInput: 0.125}},
	{"openai", "gpt-5.1-codex-mini", Pricing{Input: 0.25, Output: 2.00, CachedInput: 0.025}},
	// GPT-5 series
	{"openai", "gpt-5", Pricing{Input: 1.25, Output: 10.00, CachedInput: 0.125}},
	{"openai", "gpt-5-mini", Pricing{Input: 0.25, Output: 2.00, CachedInput: 0.025}},
	{"openai", "gpt-5-nano", Pricing{Input: 0.05, Output: 0.40, CachedInput: 0.005}},
	{"openai", "gpt-5-pro", Pricing{Input: 15.00, Output: 120.00}},
	{"openai", "gpt-5-codex", Pricing{Input: 1.25, Output: 10.00, CachedInput: 0.125}},
	// GPT-4o series
	{"openai", "gpt-4o", Pricing{Input: 2.50, Output: 10.00, CachedInput: 1.25}},
	{"openai", "gpt-4o-mini", Pricing{Input: 0.15, Output: 0.60, CachedInput: 0.075}},
	// GPT-4.1 series
	{"openai", "gpt-4.1", Pricing{Input: 2.00, Output: 8.00, CachedInput: 0.50}},
	{"openai", "gpt-4.1-mini", Pricing{Input: 0.40, Output: 1.60, CachedInput: 0.10}},
	{"openai", "gpt-4.1-nano", Pricing{Input: 0.10, Output: 0.40, CachedInput: 0.025}},
	// GPT-4 series (legacy)
	{"openai", "gpt-4-turbo", Pricing{Input: 10.00, Output: 30.00}},
	{"openai", "gpt-4", Pricing{Input: 30.00, Output: 60.00}},
	// GPT-3.5 series (legacy)
	{"openai", "gpt-3.5-turbo", Pricing{Input: 0.50, Output: 1.50}},
	// o4 series
	{"openai", "o4-mini", Pricing{Input: 1.10, Output: 4.40, CachedInput: 0.275}},
	// o3 series
	{"openai", "o3", Pricing{Input: 2.00, Output: 8.00, CachedInput: 0.50}},
	{"openai", "o3-mini", Pricing{Input: 1.10, Output: 4.40, CachedInput: 0.55}},
	{"openai", "o3-pro", Pricing{Input: 20.00, Output: 80.00}},
	// o1 series (legacy reasoning)
	{"openai", "o1", Pricing{Input: 15.00, Output: 60.00, CachedInput: 7.50}},
	{"openai", "o1-mini", Pricing{Input: 1.10, Output: 4.40, CachedInput: 0.55}},
	{"openai", "o1-pro", Pricing{Input: 150.00, Output: 600.00}},

	// -------------------------------------------------------------------------
	// ChatGPT / Codex — accessed via chatgpt.com/backend-api (OAuth subscription)
	// These are the same underlying OpenAI Codex models but routed through the
	// ChatGPT backend using the Codex CLI OAuth token. Pricing mirrors OpenAI.
	// Separate provider key allows per-source usage attribution in the Tracker.
	// -------------------------------------------------------------------------
	// GPT-5.3 Codex
	{"chatgpt", "gpt-5.3-codex", Pricing{Input: 1.75, Output: 14.00, CachedInput: 0.175}},
	// GPT-5.2 Codex
	{"chatgpt", "gpt-5.2-codex", Pricing{Input: 1.75, Output: 14.00, CachedInput: 0.175}},
	// GPT-5.1 Codex series
	{"chatgpt", "gpt-5.1-codex", Pricing{Input: 1.25, Output: 10.00, CachedInput: 0.125}},
	{"chatgpt", "gpt-5.1-codex-max", Pricing{Input: 1.25, Output: 10.00, CachedInput: 0.125}},
	{"chatgpt", "gpt-5.1-codex-mini", Pricing{Input: 0.25, Output: 2.00, CachedInput: 0.025}},
	// GPT-5 Codex
	{"chatgpt", "gpt-5-codex", Pricing{Input: 1.25, Output: 10.00, CachedInput: 0.125}},

	// -------------------------------------------------------------------------
	// Bedrock — same models as Anthropic but with "anthropic." prefix
	// Extracted from provider/bedrock/models.go (allModels)
	// -------------------------------------------------------------------------
	// Claude 4.6
	{"bedrock", "anthropic.claude-opus-4-6-v1", Pricing{Input: 5.00, Output: 25.00, CachedInput: 0.50, CacheWrite: 6.25}},
	{"bedrock", "anthropic.claude-sonnet-4-6", Pricing{Input: 3.00, Output: 15.00, CachedInput: 0.30, CacheWrite: 3.75}},
	{"bedrock", "anthropic.claude-haiku-4-5-20251001-v1:0", Pricing{Input: 1.00, Output: 5.00, CachedInput: 0.10, CacheWrite: 1.25}},
	// Claude 4.5
	{"bedrock", "anthropic.claude-opus-4-5-20251101-v1:0", Pricing{Input: 5.00, Output: 25.00, CachedInput: 0.50, CacheWrite: 6.25}},
	{"bedrock", "anthropic.claude-sonnet-4-5-20250929-v1:0", Pricing{Input: 3.00, Output: 15.00, CachedInput: 0.30, CacheWrite: 3.75}},
	// Claude 3.x
	{"bedrock", "anthropic.claude-3-7-sonnet-20250219-v1:0", Pricing{Input: 3.00, Output: 15.00, CachedInput: 0.30, CacheWrite: 3.75}},
	{"bedrock", "anthropic.claude-3-5-sonnet-20240620-v1:0", Pricing{Input: 3.00, Output: 15.00, CachedInput: 0.30, CacheWrite: 3.75}},
	{"bedrock", "anthropic.claude-3-haiku-20240307-v1:0", Pricing{Input: 0.25, Output: 1.25, CachedInput: 0.025, CacheWrite: 0.3125}},
	// Amazon Nova
	{"bedrock", "amazon.nova-premier-v1:0", Pricing{Input: 2.50, Output: 10.00}},
	{"bedrock", "amazon.nova-pro-v1:0", Pricing{Input: 0.80, Output: 3.20}},
	{"bedrock", "amazon.nova-2-lite-v1:0", Pricing{Input: 0.06, Output: 0.24}},
	{"bedrock", "amazon.nova-lite-v1:0", Pricing{Input: 0.06, Output: 0.24}},
	{"bedrock", "amazon.nova-micro-v1:0", Pricing{Input: 0.035, Output: 0.14}},
	// Cohere
	{"bedrock", "cohere.command-r-plus-v1:0", Pricing{Input: 2.50, Output: 10.00}},
	{"bedrock", "cohere.command-r-v1:0", Pricing{Input: 0.15, Output: 0.60}},
	{"bedrock", "cohere.embed-v4:0", Pricing{Input: 0.10, Output: 0}},
	// DeepSeek
	{"bedrock", "deepseek.r1-v1:0", Pricing{Input: 1.35, Output: 5.40}},
	// Meta Llama
	{"bedrock", "meta.llama4-maverick-17b-instruct-v1:0", Pricing{Input: 0.22, Output: 0.88}},
	{"bedrock", "meta.llama4-scout-17b-instruct-v1:0", Pricing{Input: 0.22, Output: 0.88}},
	{"bedrock", "meta.llama3-3-70b-instruct-v1:0", Pricing{Input: 0.72, Output: 0.72}},
	{"bedrock", "meta.llama3-2-90b-instruct-v1:0", Pricing{Input: 0.72, Output: 0.72}},
	{"bedrock", "meta.llama3-2-11b-instruct-v1:0", Pricing{Input: 0.16, Output: 0.16}},
	{"bedrock", "meta.llama3-2-3b-instruct-v1:0", Pricing{Input: 0.15, Output: 0.15}},
	{"bedrock", "meta.llama3-2-1b-instruct-v1:0", Pricing{Input: 0.10, Output: 0.10}},
	{"bedrock", "meta.llama3-1-70b-instruct-v1:0", Pricing{Input: 0.72, Output: 0.72}},
	{"bedrock", "meta.llama3-1-8b-instruct-v1:0", Pricing{Input: 0.22, Output: 0.22}},
	{"bedrock", "meta.llama3-70b-instruct-v1:0", Pricing{Input: 2.65, Output: 3.50}},
	{"bedrock", "meta.llama3-8b-instruct-v1:0", Pricing{Input: 0.30, Output: 0.60}},
	// Mistral
	{"bedrock", "mistral.mistral-large-3-675b-instruct", Pricing{Input: 0.50, Output: 1.50}},
	{"bedrock", "mistral.pixtral-large-2502-v1:0", Pricing{Input: 0.50, Output: 1.50}},
	{"bedrock", "mistral.devstral-2-123b", Pricing{Input: 0.40, Output: 2.00}},
	{"bedrock", "mistral.magistral-small-2509", Pricing{Input: 0.50, Output: 1.50}},
	{"bedrock", "mistral.ministral-3-14b-instruct", Pricing{Input: 0.20, Output: 0.20}},
	{"bedrock", "mistral.ministral-3-8b-instruct", Pricing{Input: 0.15, Output: 0.15}},
	{"bedrock", "mistral.ministral-3-3b-instruct", Pricing{Input: 0.10, Output: 0.10}},
	{"bedrock", "mistral.voxtral-small-24b-2507", Pricing{Input: 0.10, Output: 0.30}},
	{"bedrock", "mistral.voxtral-mini-3b-2507", Pricing{Input: 0.04, Output: 0.04}},
	{"bedrock", "mistral.mistral-large-2402-v1:0", Pricing{Input: 4.00, Output: 12.00}},
	{"bedrock", "mistral.mistral-small-2402-v1:0", Pricing{Input: 0.10, Output: 0.30}},
	{"bedrock", "mistral.mixtral-8x7b-instruct-v0:1", Pricing{Input: 0.45, Output: 0.70}},
	{"bedrock", "mistral.mistral-7b-instruct-v0:2", Pricing{Input: 0.15, Output: 0.20}},
	// Writer
	{"bedrock", "writer.palmyra-x4-v1:0", Pricing{Input: 2.50, Output: 10.00}},
	{"bedrock", "writer.palmyra-x5-v1:0", Pricing{Input: 0.60, Output: 6.00}},

	// -------------------------------------------------------------------------
	// MiniMax — extracted from provider/minimax/models.go
	// -------------------------------------------------------------------------
	{"minimax", "MiniMax-M2.7", Pricing{Input: 0.3, Output: 1.2, CachedInput: 0.06, CacheWrite: 0.375}},
	{"minimax", "MiniMax-M2.7-highspeed", Pricing{Input: 0.6, Output: 2.4, CachedInput: 0.06, CacheWrite: 0.375}},
	{"minimax", "MiniMax-M2.5", Pricing{Input: 0.3, Output: 1.2, CachedInput: 0.03, CacheWrite: 0.375}},
	{"minimax", "MiniMax-M2.5-highspeed", Pricing{Input: 0.6, Output: 2.4, CachedInput: 0.03, CacheWrite: 0.375}},
	{"minimax", "MiniMax-M2.1", Pricing{Input: 0.3, Output: 1.2, CachedInput: 0.03, CacheWrite: 0.375}},
	{"minimax", "MiniMax-M2.1-highspeed", Pricing{Input: 0.6, Output: 2.4, CachedInput: 0.03, CacheWrite: 0.375}},
	{"minimax", "MiniMax-M2", Pricing{Input: 0.3, Output: 1.2, CachedInput: 0.03, CacheWrite: 0.375}},
}

// providerAliases maps provider names that share pricing tables to their
// canonical name in KnownPricing. Kept here (not only in modeldb) so Static()
// works correctly even for providers whose modeldb mapping differs.
// Note: modeldb.ProviderMapping also maps "claude" → "anthropic" for model
// DB lookups; keep the two in sync when adding aliases.
var providerAliases = map[string]string{
	"claude": "anthropic", // OAuth wrapper — same models, same Anthropic pricing
}

// Static returns a CostCalculator backed by the provided pricing entries.
// When called with no arguments it uses KnownPricing.
// Entries are matched: exact model ID first, then longest-prefix match after
// stripping a trailing 8-digit date suffix.
func Static(entries ...PricingEntry) CostCalculator {
	if len(entries) == 0 {
		entries = KnownPricing
	}
	return CostCalculatorFunc(func(provider, model string, tokens TokenItems) (Cost, bool) {
		// Normalise provider aliases so that e.g. "claude" finds "anthropic" entries.
		if canonical, ok := providerAliases[provider]; ok {
			provider = canonical
		}
		// Exact match first
		for _, e := range entries {
			if e.Provider == provider && e.Model == model {
				return CalcCost(tokens, e.Pricing), true
			}
		}

		// Prefix match: strip trailing 8-digit date suffix (e.g. "-20251001")
		modelBase := model
		if len(model) > 9 && model[len(model)-9] == '-' {
			// Check if last 8 chars are digits
			allDigits := true
			for i := len(model) - 8; i < len(model); i++ {
				if model[i] < '0' || model[i] > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				modelBase = model[:len(model)-9]
			}
		}

		// Find longest prefix match
		var bestMatch *PricingEntry
		var bestLen int
		for i := range entries {
			e := &entries[i]
			if e.Provider == provider && strings.HasPrefix(modelBase, e.Model) && len(e.Model) > bestLen {
				bestMatch = e
				bestLen = len(e.Model)
			}
		}

		if bestMatch != nil {
			return CalcCost(tokens, bestMatch.Pricing), true
		}

		return Cost{}, false
	})
}

// ModelDB returns a CostCalculator backed by the embedded models.dev database.
func ModelDB() CostCalculator {
	return CostCalculatorFunc(func(provider, model string, tokens TokenItems) (Cost, bool) {
		m, ok := modeldb.GetModel(provider, model)
		if !ok || (m.Cost.Input == 0 && m.Cost.Output == 0) {
			return Cost{}, false
		}
		p := Pricing{
			Input:       m.Cost.Input,
			Output:      m.Cost.Output,
			CachedInput: m.Cost.CacheRead,
			CacheWrite:  m.Cost.CacheWrite,
		}
		return CalcCost(tokens, p), true
	})
}

// Compose returns a CostCalculator that tries each given calculator in order,
// returning the first successful result.
func Compose(calculators ...CostCalculator) CostCalculator {
	return CostCalculatorFunc(func(provider, model string, tokens TokenItems) (Cost, bool) {
		for _, c := range calculators {
			if cost, ok := c.Calculate(provider, model, tokens); ok {
				return cost, true
			}
		}
		return Cost{}, false
	})
}

// defaultCalc is the package-level singleton returned by Default().
// Initialised once at package load; avoids closure allocations on every
// provider usage event.
var defaultCalc = Compose(Static(), ModelDB()) //nolint:gochecknoglobals

// Default returns the recommended default calculator:
//
//	Compose(Static(), ModelDB())
//
// Static() is checked first because KnownPricing is manually maintained and
// verified against provider docs. ModelDB() provides broader coverage.
func Default() CostCalculator {
	return defaultCalc
}
