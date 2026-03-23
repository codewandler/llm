package minimax

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
)

func TestFillCost_WithCacheTokens(t *testing.T) {
	// Test that FillCost correctly calculates costs when cache tokens are present.
	// Scenario: 1000 total input tokens, 600 from cache read, 200 written to new cache.
	// Therefore: 200 regular input tokens (1000 - 600 - 200).
	usage := &llm.Usage{
		InputTokens:      1000,
		OutputTokens:     500,
		CacheReadTokens:  600,
		CacheWriteTokens: 200,
	}

	FillCost(ModelM27, usage)

	// M2.7 pricing: input $0.3/M, output $1.2/M, cache-read $0.06/M, cache-write $0.375/M
	// Regular input: 200 tokens -> $0.3/M * 200/1M = $0.00006
	assert.InDelta(t, 0.00006, usage.InputCost, 0.0000001)
	// Cache read: 600 tokens -> $0.06/M * 600/1M = $0.000036
	assert.InDelta(t, 0.000036, usage.CacheReadCost, 0.0000001)
	// Cache write: 200 tokens -> $0.375/M * 200/1M = $0.000075
	assert.InDelta(t, 0.000075, usage.CacheWriteCost, 0.0000001)
	// Output: 500 tokens -> $1.2/M * 500/1M = $0.0006
	assert.InDelta(t, 0.0006, usage.OutputCost, 0.0000001)
	// Total: $0.00006 + $0.000036 + $0.000075 + $0.0006 = $0.000771
	assert.InDelta(t, 0.000771, usage.Cost, 0.000001)
}

func TestFillCost_WithoutCacheTokens(t *testing.T) {
	// Test that FillCost works correctly when there are no cache tokens.
	usage := &llm.Usage{
		InputTokens:      1000,
		OutputTokens:     500,
		CacheReadTokens:  0,
		CacheWriteTokens: 0,
	}

	FillCost(ModelM27, usage)

	// All 1000 input tokens are regular input
	assert.Equal(t, 0.0003, usage.InputCost)   // $0.3/M * 1000/1M
	assert.Equal(t, 0.0, usage.CacheReadCost)
	assert.Equal(t, 0.0, usage.CacheWriteCost)
	assert.Equal(t, 0.0006, usage.OutputCost) // $1.2/M * 500/1M
	assert.Equal(t, 0.0009, usage.Cost)
}

func TestFillCost_OnlyCacheReadTokens(t *testing.T) {
	// Test when all input tokens come from cache (no regular input, no cache write).
	usage := &llm.Usage{
		InputTokens:      500,
		OutputTokens:     100,
		CacheReadTokens:  500,
		CacheWriteTokens: 0,
	}

	FillCost(ModelM27, usage)

	// No regular input cost (regularInput = 500 - 500 - 0 = 0)
	assert.Equal(t, 0.0, usage.InputCost)
	// All input charged at cache-read rate: $0.06/M * 500/1M = $0.00003
	assert.Equal(t, 0.00003, usage.CacheReadCost)
	assert.Equal(t, 0.0, usage.CacheWriteCost)
	// Output: $1.2/M * 100/1M = $0.00012
	assert.Equal(t, 0.00012, usage.OutputCost)
	assert.InDelta(t, 0.00015, usage.Cost, 0.000001)
}

func TestFillCost_OnlyCacheWriteTokens(t *testing.T) {
	// Test when all input tokens are written to cache (no regular input, no cache read).
	// This is a theoretical scenario - in practice you'd still have some regular input.
	usage := &llm.Usage{
		InputTokens:      300,
		OutputTokens:     50,
		CacheReadTokens:  0,
		CacheWriteTokens: 300,
	}

	FillCost(ModelM27, usage)

	// No regular input cost
	assert.Equal(t, 0.0, usage.InputCost)
	assert.Equal(t, 0.0, usage.CacheReadCost)
	// Cache write: $0.375/M * 300/1M = $0.0001125
	assert.InDelta(t, 0.0001125, usage.CacheWriteCost, 0.0000001)
	assert.Equal(t, 0.00006, usage.OutputCost) // $1.2/M * 50/1M
}

func TestFillCost_DifferentModels(t *testing.T) {
	tests := []struct {
		model           string
		wantInputCost   float64
		wantCacheRead   float64
		wantCacheWrite  float64
		wantOutputCost  float64
	}{
		{
			model:           ModelM27,
			wantInputCost:   0.00003,   // 100 tokens * $0.3/M
			wantCacheRead:   0.000003,  // 50 tokens * $0.06/M
			wantCacheWrite:  0.00001875, // 50 tokens * $0.375/M
			wantOutputCost:  0.00006,   // 50 tokens * $1.2/M
		},
		{
			model:          ModelM25,
			wantInputCost:  0.00003,   // 100 tokens * $0.3/M
			wantCacheRead:  0.0000015, // 50 tokens * $0.03/M
			wantCacheWrite: 0.00001875, // 50 tokens * $0.375/M
			wantOutputCost: 0.00006,   // 50 tokens * $1.2/M
		},
		{
			model:          ModelM21,
			wantInputCost:  0.00003,   // 100 tokens * $0.3/M
			wantCacheRead:  0.0000015, // 50 tokens * $0.03/M
			wantCacheWrite: 0.00001875, // 50 tokens * $0.375/M
			wantOutputCost: 0.00006,   // 50 tokens * $1.2/M
		},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			usage := &llm.Usage{
				InputTokens:      200, // 100 regular + 50 cache-read + 50 cache-write
				OutputTokens:     50,
				CacheReadTokens:  50,
				CacheWriteTokens: 50,
			}

			FillCost(tt.model, usage)

			assert.InDelta(t, tt.wantInputCost, usage.InputCost, 0.0000001)
			assert.InDelta(t, tt.wantCacheRead, usage.CacheReadCost, 0.0000001)
			assert.InDelta(t, tt.wantCacheWrite, usage.CacheWriteCost, 0.0000001)
			assert.InDelta(t, tt.wantOutputCost, usage.OutputCost, 0.0000001)
		})
	}
}

func TestFillCost_NilUsage(t *testing.T) {
	// Should not panic when usage is nil
	FillCost(ModelM27, nil)
}

func TestFillCost_UnknownModel(t *testing.T) {
	// Should not modify usage when model is unknown
	usage := &llm.Usage{
		InputTokens:      1000,
		OutputTokens:     500,
		CacheReadTokens:  100,
		CacheWriteTokens: 100,
	}

	FillCost("unknown-model", usage)

	// Costs should remain at zero (not modified)
	assert.Equal(t, 0.0, usage.InputCost)
	assert.Equal(t, 0.0, usage.CacheReadCost)
	assert.Equal(t, 0.0, usage.CacheWriteCost)
	assert.Equal(t, 0.0, usage.OutputCost)
	assert.Equal(t, 0.0, usage.Cost)
}

func TestFillCost_HighspeedModels(t *testing.T) {
	// Test that highspeed models have pricing (even if placeholder)
	usage := &llm.Usage{
		InputTokens:      1000,
		OutputTokens:     500,
		CacheReadTokens:  0,
		CacheWriteTokens: 0,
	}

	// These will use placeholder pricing - test that they don't panic
	FillCost(ModelM27Highspeed, usage)
	assert.Greater(t, usage.Cost, 0.0)

	FillCost(ModelM25Highspeed, usage)
	assert.Greater(t, usage.Cost, 0.0)

	FillCost(ModelM21Highspeed, usage)
	assert.Greater(t, usage.Cost, 0.0)
}

func TestFillCost_NegativeRegularInput(t *testing.T) {
	// Edge case: if cache tokens exceed input tokens (shouldn't happen in practice)
	usage := &llm.Usage{
		InputTokens:      100,
		OutputTokens:     50,
		CacheReadTokens:  150, // More than input
		CacheWriteTokens: 100,
	}

	FillCost(ModelM27, usage)

	// regularInput = 100 - 150 - 100 = -150, should be clamped to 0
	assert.Equal(t, 0.0, usage.InputCost)
	// But cache costs should still be calculated
	assert.Greater(t, usage.CacheReadCost, 0.0)
	assert.Greater(t, usage.CacheWriteCost, 0.0)
}
