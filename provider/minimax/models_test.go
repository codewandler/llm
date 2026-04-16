package minimax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/usage"
)

func TestCalculateCost_UnknownModel(t *testing.T) {
	tokens := usage.TokenItems{{Kind: usage.KindInput, Count: 1000}, {Kind: usage.KindOutput, Count: 500}}
	_, ok := usage.Default().Calculate("minimax", "unknown-model", tokens)
	assert.False(t, ok)
}

func TestCalculateCost_M27_InputOutput(t *testing.T) {
	tokens := usage.TokenItems{
		{Kind: usage.KindInput, Count: 1_000_000},
		{Kind: usage.KindOutput, Count: 1_000_000},
	}
	cost, ok := usage.Default().Calculate("minimax", ModelM27, tokens)
	require.True(t, ok)

	assert.InDelta(t, 2.1, cost.Input, 1e-10)
	assert.InDelta(t, 8.4, cost.Output, 1e-10)
	assert.Equal(t, 0.0, cost.CacheRead)
	assert.Equal(t, 0.0, cost.CacheWrite)
	assert.InDelta(t, 10.5, cost.Total, 1e-10)
}

func TestCalculateCost_M27_WithCache(t *testing.T) {
	tokens := usage.TokenItems{
		{Kind: usage.KindInput, Count: 1_000_000},
		{Kind: usage.KindCacheRead, Count: 300_000},
		{Kind: usage.KindCacheWrite, Count: 200_000},
		{Kind: usage.KindOutput, Count: 500_000},
	}
	cost, ok := usage.Default().Calculate("minimax", ModelM27, tokens)
	require.True(t, ok)

	expectedInput := float64(1_000_000) / 1_000_000 * 2.1
	expectedCacheRead := float64(300_000) / 1_000_000 * 0.42
	expectedCacheWrite := float64(200_000) / 1_000_000 * 2.625
	expectedOutput := float64(500_000) / 1_000_000 * 8.4
	expectedTotal := expectedInput + expectedCacheRead + expectedCacheWrite + expectedOutput

	assert.InDelta(t, expectedInput, cost.Input, 1e-10, "Input")
	assert.InDelta(t, expectedCacheRead, cost.CacheRead, 1e-10, "CacheRead")
	assert.InDelta(t, expectedCacheWrite, cost.CacheWrite, 1e-10, "CacheWrite")
	assert.InDelta(t, expectedOutput, cost.Output, 1e-10, "Output")
	assert.InDelta(t, expectedTotal, cost.Total, 1e-10, "Total")
}

func TestCalculateCost_M27Highspeed(t *testing.T) {
	tokens := usage.TokenItems{
		{Kind: usage.KindInput, Count: 1_000_000},
		{Kind: usage.KindOutput, Count: 1_000_000},
	}
	cost, ok := usage.Default().Calculate("minimax", ModelM27Highspeed, tokens)
	require.True(t, ok)

	assert.InDelta(t, 4.2, cost.Input, 1e-10)
	assert.InDelta(t, 16.8, cost.Output, 1e-10)
	assert.InDelta(t, 21.0, cost.Total, 1e-10)
}

func TestCalculateCost_AllModelsPresent(t *testing.T) {
	models := []string{
		ModelM27, ModelM27Highspeed,
		ModelM25, ModelM25Highspeed,
		ModelM21, ModelM21Highspeed,
		ModelM2,
	}
	for _, m := range models {
		t.Run(m, func(t *testing.T) {
			tokens := usage.TokenItems{{Kind: usage.KindInput, Count: 1_000_000}, {Kind: usage.KindOutput, Count: 1_000_000}}
			cost, ok := usage.Default().Calculate("minimax", m, tokens)
			require.True(t, ok, "model %q must have a pricing entry", m)
			assert.Greater(t, cost.Total, 0.0, "model %q must have a non-zero total", m)
		})
	}
}
