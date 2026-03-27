package anthropic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestCalculateCost_NilUsage(t *testing.T) {
	assert.Equal(t, 0.0, CalculateCost("claude-sonnet-4-6", nil))
}

func TestCalculateCost_UnknownModel(t *testing.T) {
	assert.Equal(t, 0.0, CalculateCost("gpt-4o", &llm.Usage{InputTokens: 1000, OutputTokens: 500}))
}

func TestCalculateCost_KnownModel(t *testing.T) {
	// claude-sonnet-4-6: input $3.00/M, output $15.00/M
	cost := CalculateCost("claude-sonnet-4-6", &llm.Usage{
		InputTokens:  1_000_000,
		OutputTokens: 1_000_000,
	})
	assert.InDelta(t, 18.0, cost, 1e-9)
}

func TestCalculateCost_WithCacheTokens(t *testing.T) {
	// claude-sonnet-4-6: input $3/M, cache-read $0.30/M, cache-write $3.75/M, output $15/M
	cost := CalculateCost("claude-sonnet-4-6", &llm.Usage{
		InputTokens:      1_500_000,
		CacheReadTokens:  300_000,
		CacheWriteTokens: 200_000,
		OutputTokens:     500_000,
	})
	expected := (1_000_000.0/1e6)*3.0 +
		(300_000.0/1e6)*0.30 +
		(200_000.0/1e6)*3.75 +
		(500_000.0/1e6)*15.0
	assert.InDelta(t, expected, cost, 1e-9)
}

func TestCalculateCost_NegativeRegularInput_Clamped(t *testing.T) {
	// CacheReadTokens + CacheWriteTokens > InputTokens → regularInput clamped to 0
	cost := CalculateCost("claude-sonnet-4-6", &llm.Usage{
		InputTokens:      100,
		CacheReadTokens:  80,
		CacheWriteTokens: 80,
		OutputTokens:     50,
	})
	require.GreaterOrEqual(t, cost, 0.0, "cost must not be negative")
}

func TestCalculateCost_PrefixFallback(t *testing.T) {
	// A future dated sonnet that isn't in the registry — should match the prefix
	cost := CalculateCost("claude-sonnet-4-6-20991231", &llm.Usage{
		InputTokens:  1_000_000,
		OutputTokens: 1_000_000,
	})
	// Should match claude-sonnet-4-6 pricing: $3+$15 = $18
	assert.InDelta(t, 18.0, cost, 1e-9)
}

func TestMatchPricingByPrefix_NoMatch(t *testing.T) {
	_, ok := matchPricingByPrefix("totally-unknown-model-xyz")
	assert.False(t, ok)
}

func TestMatchPricingByPrefix_DerivedFromRegistry(t *testing.T) {
	// Every key in modelPricingRegistry must produce a working prefix fallback
	// when a synthetic future-dated variant is looked up.
	for id := range modelPricingRegistry {
		base := dateSuffix.ReplaceAllString(id, "")
		futureID := base + "-20991231"
		pricing, ok := matchPricingByPrefix(futureID)
		assert.True(t, ok, "prefix lookup failed for future variant of %q (%q)", id, futureID)
		if ok {
			expected := modelPricingRegistry[base]
			if expected == (modelPricing{}) {
				// base itself is a dated-only entry; just check non-zero output price
				assert.Greater(t, pricing.OutputPrice, 0.0,
					"pricing for %q has zero output price", futureID)
			} else {
				assert.Equal(t, expected, pricing,
					"pricing mismatch for future variant of %q", id)
			}
		}
	}
}

func TestFillCost_NilUsage(t *testing.T) {
	// Must not panic.
	FillCost("claude-sonnet-4-6", nil)
}

func TestFillCost_UnknownModel(t *testing.T) {
	u := &llm.Usage{InputTokens: 1000, OutputTokens: 500}
	FillCost("gpt-4o", u)
	assert.Equal(t, 0.0, u.Cost)
	assert.Equal(t, 0.0, u.InputCost)
	assert.Equal(t, 0.0, u.OutputCost)
}

func TestFillCost_KnownModel(t *testing.T) {
	u := &llm.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	FillCost("claude-sonnet-4-6", u)
	assert.InDelta(t, 3.0, u.InputCost, 1e-9)
	assert.InDelta(t, 15.0, u.OutputCost, 1e-9)
	assert.InDelta(t, 18.0, u.Cost, 1e-9)
	assert.Equal(t, 0.0, u.CacheReadCost)
	assert.Equal(t, 0.0, u.CacheWriteCost)
}
