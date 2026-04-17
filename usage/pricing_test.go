package usage

import (
	"testing"

	modeldb "github.com/codewandler/modeldb"
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
				Input:      1.5,
				CacheRead:  0.09,
				CacheWrite: 0.75,
				Output:     1.5,
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
			pricing: Pricing{Output: 15.0, Reasoning: 0},
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

func TestDefault_CatalogModelPricing(t *testing.T) {
	calc := Default()
	tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}

	cost, ok := calc.Calculate("anthropic", "claude-sonnet-4-6", tokens)
	require.True(t, ok, "claude-sonnet-4-6 should be resolved via catalog")
	assert.Greater(t, cost.Total, 0.0)

	cost, ok = calc.Calculate("minimax", "MiniMax-M2.7", tokens)
	require.True(t, ok, "MiniMax-M2.7 should be resolved via catalog")
	assert.Greater(t, cost.Total, 0.0)
}

func TestDefault_ProviderAliases(t *testing.T) {
	calc := Default()
	tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}

	costClaude, ok := calc.Calculate("claude", "claude-sonnet-4-6", tokens)
	require.True(t, ok, "claude provider should resolve via anthropic alias")

	costAnthropic, ok := calc.Calculate("anthropic", "claude-sonnet-4-6", tokens)
	require.True(t, ok, "anthropic direct lookup must succeed")

	assert.Equal(t, costAnthropic.Total, costClaude.Total,
		"claude and anthropic must produce identical cost for the same model")
}

func TestCalcCost_NegativeInputProducesNegativeCost(t *testing.T) {
	tokens := TokenItems{{Kind: KindInput, Count: -100}}
	pricing := Pricing{Input: 3.0}
	got := CalcCost(tokens, pricing)
	assert.True(t, got.Input < 0, "negative input tokens must produce negative cost")
}

func TestDefault_UnknownModel(t *testing.T) {
	calc := Default()
	tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}
	_, ok := calc.Calculate("anthropic", "unknown-model-xyz", tokens)
	assert.False(t, ok)
}

func TestDefault_LineKeyFallback(t *testing.T) {
	calc := Default()
	tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}

	cost, ok := calc.Calculate("anthropic", "claude-sonnet-4-6-20250929", tokens)
	require.True(t, ok, "future-dated variant should fall back to line-level pricing")
	assert.Greater(t, cost.Total, 0.0)

	exactCost, ok2 := calc.Calculate("anthropic", "claude-sonnet-4-6", tokens)
	require.True(t, ok2)
	assert.InDelta(t, exactCost.Total, cost.Total, 1e-9,
		"line fallback for %q should equal exact match", "claude-sonnet-4-6-20250929")
}

func TestDefault_BedrockOfferings(t *testing.T) {
	calc := Default()
	tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}

	cost, ok := calc.Calculate("bedrock", "anthropic.claude-sonnet-4-6", tokens)
	require.True(t, ok, "bedrock offering should resolve via catalog")
	assert.Greater(t, cost.Total, 0.0, "bedrock model should have non-zero cost")
}

func TestDefault_CodexProviderAliasResolvesOpenAIPricing(t *testing.T) {
	calc := Default()
	tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}

	costOpenAI, okOpenAI := calc.Calculate("openai", "gpt-5.4-mini", tokens)
	require.True(t, okOpenAI, "openai gpt-5.4-mini should resolve via built-in catalog fallback")
	require.Greater(t, costOpenAI.Total, 0.0)

	costCodex, okCodex := calc.Calculate("codex", "gpt-5.4-mini", tokens)
	require.True(t, okCodex, "codex should reuse openai pricing for identical wire model IDs")
	assert.InDelta(t, costOpenAI.Total, costCodex.Total, 1e-12)
	assert.InDelta(t, costOpenAI.Input, costCodex.Input, 1e-12)
}

func TestInferPricingModelKey_OpenAIDottedVersions(t *testing.T) {
	assert.Equal(t, pricingByModelKey{Creator: "openai", Family: "gpt", Version: "5.4", Variant: "mini"}, inferPricingModelKey("openai", "gpt-5.4-mini"))
	assert.Equal(t, pricingByModelKey{Creator: "openai", Family: "gpt", Version: "5.3", Variant: "codex"}, inferPricingModelKey("openai", "gpt-5.3-codex"))
	assert.Equal(t, pricingByModelKey{Creator: "openai", Family: "gpt", Version: "5.1", Variant: "codex-mini"}, inferPricingModelKey("openai", "gpt-5.1-codex-mini"))
}

func TestDefault_CodexOpenAIFallback(t *testing.T) {
	c, err := modeldb.LoadBuiltIn()
	require.NoError(t, err)

	anthropicOfferings := c.OfferingsByService("anthropic")
	require.NotEmpty(t, anthropicOfferings, "anthropic should have offerings")

	calc := Default()
	tokens := TokenItems{{Kind: KindInput, Count: 1_000_000}}

	found := false
	for _, offering := range anthropicOfferings {
		modelKey := modeldb.LineID(offering.ModelKey)
		cost, ok := calc.Calculate("anthropic", offering.WireModelID, tokens)
		if ok && cost.Total > 0 {
			found = true
			t.Logf("resolved %s via catalog", modelKey)
			return
		}
	}
	require.True(t, found, "at least one anthropic model should be resolvable via catalog")
}

func TestCompose(t *testing.T) {
	calc := Compose(Default())

	ok1, ok2 := false, false
	var c1, c2 Cost

	c1, ok1 = calc.Calculate("anthropic", "claude-sonnet-4-6", TokenItems{{Kind: KindInput, Count: 1_000_000}})
	if !ok1 {
		c1, ok1 = calc.Calculate("minimax", "MiniMax-M2.7", TokenItems{{Kind: KindInput, Count: 1_000_000}})
	}
	c2, ok2 = calc.Calculate("minimax", "MiniMax-M2.7", TokenItems{{Kind: KindInput, Count: 1_000_000}})

	assert.True(t, ok1 || ok2, "at least one calculator should succeed")
	_ = c1
	_ = c2
}
