package tokencount

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/usage"
)

func TestEstimateRecords_NilTokenCount(t *testing.T) {
	recs := EstimateRecords(nil, "anthropic", "claude-sonnet-4-6", "heuristic", nil)
	assert.Nil(t, recs)
}

func TestEstimateRecords_PrimaryOnly(t *testing.T) {
	// When all breakdown fields are zero, only the primary record is emitted.
	est := &TokenCount{InputTokens: 1000}
	recs := EstimateRecords(est, "anthropic", "claude-sonnet-4-6", "heuristic", nil)

	require.Len(t, recs, 1)
	assert.True(t, recs[0].IsEstimate)
	assert.Equal(t, "heuristic", recs[0].Source)
	assert.Nil(t, recs[0].Dims.Labels)
	assert.Equal(t, 1000, recs[0].Tokens.Count(usage.KindInput))
	assert.Equal(t, "anthropic", recs[0].Dims.Provider)
	assert.Equal(t, "claude-sonnet-4-6", recs[0].Dims.Model)
}

func TestEstimateRecords_WithBreakdowns(t *testing.T) {
	est := &TokenCount{
		InputTokens:     1500,
		SystemTokens:    500,
		UserTokens:      300,
		AssistantTokens: 100,
		ToolsTokens:     200,
		OverheadTokens:  400,
	}
	recs := EstimateRecords(est, "openai", "gpt-4o", "heuristic", nil)

	// Primary + 5 segments (system, user, assistant, tools, overhead)
	// ToolResultTokens == 0 so it is skipped.
	require.Len(t, recs, 6)

	// First record is always the unlabeled primary.
	assert.Nil(t, recs[0].Dims.Labels, "primary must have no labels")
	assert.Equal(t, 1500, recs[0].Tokens.Count(usage.KindInput))

	// Verify each labeled breakdown.
	wantSegments := map[string]int{
		"system":    500,
		"user":      300,
		"assistant": 100,
		"tools":     200,
		"overhead":  400,
	}
	for _, rec := range recs[1:] {
		require.NotNil(t, rec.Dims.Labels)
		cat := rec.Dims.Labels["category"]
		expected, ok := wantSegments[cat]
		require.True(t, ok, "unexpected category: %s", cat)
		assert.Equal(t, expected, rec.Tokens.Count(usage.KindInput),
			"wrong count for category %s", cat)
		delete(wantSegments, cat)
	}
	assert.Empty(t, wantSegments, "some expected segments were not emitted")
}

func TestEstimateRecords_WithCostCalculator(t *testing.T) {
	est := &TokenCount{InputTokens: 1_000_000}

	calc := usage.CostCalculatorFunc(func(provider, model string, tokens usage.TokenItems) (usage.Cost, bool) {
		return usage.Cost{Total: 3.0, Input: 3.0, Source: "calculated"}, true
	})
	recs := EstimateRecords(est, "anthropic", "claude-sonnet-4-6", "heuristic", calc)

	require.Len(t, recs, 1) // only primary, no breakdown fields
	assert.Equal(t, "estimated", recs[0].Cost.Source)
	assert.InDelta(t, 3.0, recs[0].Cost.Total, 0.001)
}

func TestEstimateRecords_BreakdownsHaveNoCost(t *testing.T) {
	est := &TokenCount{
		InputTokens:  1000,
		SystemTokens: 500,
		UserTokens:   500,
	}
	calc := usage.CostCalculatorFunc(func(provider, model string, tokens usage.TokenItems) (usage.Cost, bool) {
		return usage.Cost{Total: 1.0, Source: "calculated"}, true
	})
	recs := EstimateRecords(est, "anthropic", "claude-sonnet-4-6", "api", calc)

	require.Len(t, recs, 3) // primary + system + user

	// Only primary should have cost enriched.
	assert.Equal(t, "estimated", recs[0].Cost.Source)
	assert.Equal(t, "api", recs[0].Source)
	assert.True(t, recs[1].Cost.IsZero(), "breakdown should not have cost")
	assert.True(t, recs[2].Cost.IsZero(), "breakdown should not have cost")
}
