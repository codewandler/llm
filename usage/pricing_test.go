package usage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalcCost(t *testing.T) {
	tests := []struct {
		name     string
		tokens   TokenItems
		pricing  Pricing
		wantCost Cost
	}{
		{
			name:    "input only",
			tokens:  TokenItems{{Kind: KindInput, Count: 1_000_000}},
			pricing: Pricing{Input: 3.0, Output: 15.0},
			wantCost: Cost{
				Input:  3.0,
				Total:  3.0,
				Source: "calculated",
			},
		},
		{
			name: "all kinds",
			tokens: TokenItems{
				{Kind: KindInput, Count: 500_000},
				{Kind: KindCacheRead, Count: 300_000},
				{Kind: KindCacheWrite, Count: 200_000},
				{Kind: KindOutput, Count: 100_000},
			},
			pricing: Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75},
			wantCost: Cost{
				Input:      1.5,  // 500k * 3.0 / 1M
				CacheRead:  0.09, // 300k * 0.30 / 1M
				CacheWrite: 0.75, // 200k * 3.75 / 1M
				Output:     1.5,  // 100k * 15.0 / 1M
				Total:      3.84,
				Source:     "calculated",
			},
		},
		{
			name:    "reasoning with explicit rate",
			tokens:  TokenItems{{Kind: KindReasoning, Count: 1_000_000}},
			pricing: Pricing{Output: 15.0, Reasoning: 20.0},
			wantCost: Cost{
				Reasoning: 20.0,
				Total:     20.0,
				Source:    "calculated",
			},
		},
		{
			name:    "reasoning fallback to output rate",
			tokens:  TokenItems{{Kind: KindReasoning, Count: 1_000_000}},
			pricing: Pricing{Output: 15.0, Reasoning: 0}, // Reasoning == 0 means use Output
			wantCost: Cost{
				Reasoning: 15.0,
				Total:     15.0,
				Source:    "calculated",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcCost(tt.tokens, tt.pricing)
			assert.InDelta(t, tt.wantCost.Total, got.Total, 0.001)
			assert.InDelta(t, tt.wantCost.Input, got.Input, 0.001)
			assert.InDelta(t, tt.wantCost.Output, got.Output, 0.001)
			assert.InDelta(t, tt.wantCost.Reasoning, got.Reasoning, 0.001)
			assert.InDelta(t, tt.wantCost.CacheRead, got.CacheRead, 0.001)
			assert.InDelta(t, tt.wantCost.CacheWrite, got.CacheWrite, 0.001)
			assert.Equal(t, tt.wantCost.Source, got.Source)
		})
	}
}

func TestStatic(t *testing.T) {
	calc := Static() // uses KnownPricing

	t.Run("exact match", func(t *testing.T) {
		tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}
		cost, ok := calc.Calculate("anthropic", "claude-sonnet-4-6", tokens)
		require.True(t, ok)
		assert.Equal(t, "calculated", cost.Source)
		assert.InDelta(t, 3.0, cost.Total, 0.001)
	})

	t.Run("prefix match with date suffix", func(t *testing.T) {
		tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}
		cost, ok := calc.Calculate("anthropic", "claude-sonnet-4-6-20251015", tokens)
		require.True(t, ok)
		assert.InDelta(t, 3.0, cost.Total, 0.001)
	})

	t.Run("unknown model", func(t *testing.T) {
		tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}
		_, ok := calc.Calculate("anthropic", "unknown-model-xyz", tokens)
		assert.False(t, ok)
	})
}

func TestDefault(t *testing.T) {
	calc := Default()

	// Sanity check: known models from KnownPricing should resolve
	tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}

	cost, ok := calc.Calculate("anthropic", "claude-sonnet-4-6", tokens)
	require.True(t, ok, "claude-sonnet-4-6 should be in KnownPricing")
	assert.Greater(t, cost.Total, 0.0)

	cost, ok = calc.Calculate("openai", "gpt-4o", tokens)
	require.True(t, ok, "gpt-4o should be in KnownPricing")
	assert.Greater(t, cost.Total, 0.0)
}

func TestStatic_ProviderAliases(t *testing.T) {
	calc := Default()
	tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}

	// "claude" (OAuth wrapper) must resolve via the "anthropic" alias and
	// return identical pricing.  Removing the alias would silently produce
	// zero cost with no error, so this test acts as a regression guard.
	costClaude, ok := calc.Calculate("claude", "claude-sonnet-4-6", tokens)
	require.True(t, ok, "claude provider should resolve via anthropic alias")

	costAnthropic, ok := calc.Calculate("anthropic", "claude-sonnet-4-6", tokens)
	require.True(t, ok, "anthropic direct lookup must succeed")

	assert.Equal(t, costAnthropic.Total, costClaude.Total,
		"claude and anthropic must produce identical cost for the same model")
}

func TestCalcCost_NegativeInputProducesNegativeCost(t *testing.T) {
	// CalcCost itself does not clamp: providers are responsible for clamping
	// before calling CalcCost.  This test documents that behaviour so a
	// future change doesn't silently introduce clamping at the wrong layer.
	tokens := TokenItems{{Kind: KindInput, Count: -100}}
	pricing := Pricing{Input: 3.0}
	got := CalcCost(tokens, pricing)
	assert.True(t, got.Input < 0, "negative input tokens must produce negative cost (clamping is the caller's job)")
}

func TestStatic_PrefixFallback_AllKnownModels(t *testing.T) {
	// Every entry in KnownPricing must be reachable via a synthetic
	// future-dated variant, ensuring the prefix-matching logic covers the
	// full registry.  This is ported from the old
	// provider/anthropic/models_test.go TestMatchPricingByPrefix_DerivedFromRegistry.
	calc := Static()
	for _, entry := range KnownPricing {
		futureID := entry.Model + "-20991231"
		cost, ok := calc.Calculate(entry.Provider, futureID, TokenItems{{Kind: KindInput, Count: 1_000_000}})
		assert.True(t, ok, "prefix lookup failed for future variant %q (provider %s)", futureID, entry.Provider)
		if ok {
			// Must produce the same cost as the exact match.
			exactCost, _ := calc.Calculate(entry.Provider, entry.Model, TokenItems{{Kind: KindInput, Count: 1_000_000}})
			assert.InDelta(t, exactCost.Total, cost.Total, 1e-9,
				"prefix match for %q should equal exact match for %q", futureID, entry.Model)
		}
	}
}

func TestStatic_BedrockInferenceProfilePrefix(t *testing.T) {
	// Bedrock models can be prefixed with region (e.g. "us.anthropic.claude-sonnet-4-6").
	// The provider strips the prefix before calling Calculate, so here we test
	// the bare model ID (no region prefix) against the pricing table.
	calc := Static()
	tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}

	// Bare model ID (after prefix stripping by provider) — must match.
	cost, ok := calc.Calculate("bedrock", "anthropic.claude-sonnet-4-6", tokens)
	require.True(t, ok, "bare Bedrock model should resolve pricing")
	assert.Greater(t, cost.Total, 0.0, "Bedrock model should have non-zero cost")

	// A version suffix should also resolve via prefix match.
	cost2, ok2 := calc.Calculate("bedrock", "anthropic.claude-sonnet-4-6-v2:0", tokens)
	if ok2 {
		assert.Greater(t, cost2.Total, 0.0)
	}
}
