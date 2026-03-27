package minimax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestFillCost_UnknownModel(t *testing.T) {
	u := &llm.Usage{InputTokens: 1000, OutputTokens: 500}
	FillCost("unknown-model", u)
	assert.Equal(t, 0.0, u.Cost)
	assert.Equal(t, 0.0, u.InputCost)
	assert.Equal(t, 0.0, u.OutputCost)
}

func TestFillCost_NilUsage(t *testing.T) {
	// Must not panic.
	FillCost(ModelM27, nil)
}

func TestFillCost_M27_InputOutput(t *testing.T) {
	// M2.7: input $0.30/M, output $1.20/M
	u := &llm.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	FillCost(ModelM27, u)

	assert.InDelta(t, 0.30, u.InputCost, 1e-10)
	assert.InDelta(t, 1.20, u.OutputCost, 1e-10)
	assert.Equal(t, 0.0, u.CacheReadCost)
	assert.Equal(t, 0.0, u.CacheWriteCost)
	assert.InDelta(t, 1.50, u.Cost, 1e-10)
}

func TestFillCost_M27_WithCache(t *testing.T) {
	// M2.7 cache: read $0.06/M, write $0.375/M
	// InputTokens includes all tokens (regular + cache read + cache write).
	// InputCost must only cover the non-cache portion.
	u := &llm.Usage{
		InputTokens:      1_500_000, // 1M regular + 300K cache-read + 200K cache-write
		CacheReadTokens:  300_000,
		CacheWriteTokens: 200_000,
		OutputTokens:     500_000,
	}
	FillCost(ModelM27, u)

	expectedInput := float64(1_000_000) / 1_000_000 * 0.30     // regular input only
	expectedCacheRead := float64(300_000) / 1_000_000 * 0.06   // $0.06/M
	expectedCacheWrite := float64(200_000) / 1_000_000 * 0.375 // $0.375/M
	expectedOutput := float64(500_000) / 1_000_000 * 1.20      // $1.20/M
	expectedTotal := expectedInput + expectedCacheRead + expectedCacheWrite + expectedOutput

	assert.InDelta(t, expectedInput, u.InputCost, 1e-10, "InputCost")
	assert.InDelta(t, expectedCacheRead, u.CacheReadCost, 1e-10, "CacheReadCost")
	assert.InDelta(t, expectedCacheWrite, u.CacheWriteCost, 1e-10, "CacheWriteCost")
	assert.InDelta(t, expectedOutput, u.OutputCost, 1e-10, "OutputCost")
	assert.InDelta(t, expectedTotal, u.Cost, 1e-10, "Cost total")
}

func TestFillCost_M27Highspeed(t *testing.T) {
	// Highspeed: input $0.60/M, output $2.40/M (2× standard)
	u := &llm.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	FillCost(ModelM27Highspeed, u)

	assert.InDelta(t, 0.60, u.InputCost, 1e-10)
	assert.InDelta(t, 2.40, u.OutputCost, 1e-10)
	assert.InDelta(t, 3.00, u.Cost, 1e-10)
}

func TestFillCost_NegativeRegularInput_Clamped(t *testing.T) {
	// CacheReadTokens + CacheWriteTokens exceed InputTokens → regularInput < 0 → clamped to 0.
	u := &llm.Usage{
		InputTokens:      100,
		CacheReadTokens:  80,
		CacheWriteTokens: 80,
		OutputTokens:     50,
	}
	FillCost(ModelM27, u)

	require.GreaterOrEqual(t, u.InputCost, 0.0, "InputCost must not be negative")
	require.GreaterOrEqual(t, u.Cost, 0.0, "Cost must not be negative")
}

func TestFillCost_AllModelsPresent(t *testing.T) {
	// Smoke-test: every exported model constant must have a pricing entry.
	models := []string{
		ModelM27, ModelM27Highspeed,
		ModelM25, ModelM25Highspeed,
		ModelM21, ModelM21Highspeed,
		ModelM2,
	}
	for _, m := range models {
		t.Run(m, func(t *testing.T) {
			u := &llm.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
			FillCost(m, u)
			assert.Greater(t, u.Cost, 0.0, "model %q must have a pricing entry", m)
		})
	}
}
