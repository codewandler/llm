package llm_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	llm "github.com/codewandler/llm"
)

func TestSmartCache_BelowThreshold_DoesNotMark(t *testing.T) {
	sc := llm.NewSmartCache(2048)
	sc.UpdateTokenCount(1000)
	require.False(t, sc.ShouldMarkForCache())
}

func TestSmartCache_ExactlyAtThreshold_DoesNotMark(t *testing.T) {
	sc := llm.NewSmartCache(2048)
	sc.UpdateTokenCount(2048)
	require.False(t, sc.ShouldMarkForCache())
}

func TestSmartCache_AboveThreshold_Marks(t *testing.T) {
	sc := llm.NewSmartCache(2048)
	sc.UpdateTokenCount(2049)
	require.True(t, sc.ShouldMarkForCache())
}

func TestSmartCache_MarkCachePoint_FreezesPosition(t *testing.T) {
	sc := llm.NewSmartCache(500)
	sc.UpdateTokenCount(600) // above threshold
	require.True(t, sc.ShouldMarkForCache())

	sc.MarkCachePoint()
	// Now distance resets to 0; adding less than threshold should not trigger
	require.False(t, sc.ShouldMarkForCache())

	sc.UpdateTokenCount(400)
	require.False(t, sc.ShouldMarkForCache())

	sc.UpdateTokenCount(101) // 400+101 = 501 > 500
	require.True(t, sc.ShouldMarkForCache())
}

func TestSmartCache_UpdateTokenCount_Accumulates(t *testing.T) {
	sc := llm.NewSmartCache(1000)
	sc.UpdateTokenCount(400)
	sc.UpdateTokenCount(300)
	sc.UpdateTokenCount(301) // 400+300+301 = 1001 > 1000
	require.True(t, sc.ShouldMarkForCache())
	require.Equal(t, 1001, sc.TotalTokensSeen())
}

func TestSmartCache_Reset_ClearsState(t *testing.T) {
	sc := llm.NewSmartCache(100)
	sc.UpdateTokenCount(500)
	require.True(t, sc.ShouldMarkForCache())

	sc.Reset()
	require.False(t, sc.ShouldMarkForCache())
	require.Equal(t, 0, sc.TotalTokensSeen())
	require.Equal(t, 0, sc.LastCachePointAt())
}

func TestSmartCache_DisabledWhenThresholdZero(t *testing.T) {
	sc := llm.NewSmartCache(0)
	sc.UpdateTokenCount(99999)
	require.False(t, sc.ShouldMarkForCache())
}

func TestSmartCache_DisabledWhenNil(t *testing.T) {
	var sc *llm.SmartCache
	sc.UpdateTokenCount(99999)
	require.False(t, sc.ShouldMarkForCache())
	sc.MarkCachePoint()
	sc.Reset()
	// No panic
}

func TestSmartCache_MultiTurn_DistanceAccumulates(t *testing.T) {
	// Simulates a real agent loop: each turn the LLM reports input tokens,
	// then we check if the next message should be cached.
	sc := llm.NewSmartCache(2048)

	// Turn 1: 600 input tokens → distance 600, no mark
	sc.UpdateTokenCount(600)
	require.False(t, sc.ShouldMarkForCache())

	// Turn 2: 700 more → 1300 total, no mark
	sc.UpdateTokenCount(700)
	require.False(t, sc.ShouldMarkForCache())

	// Turn 3: 800 more → 2100 total, still no mark (2100-0 > 2048 → true... wait)
	sc.UpdateTokenCount(800)
	// 600+700+800 = 2100, lastCachePointAt = 0, distance = 2100 > 2048 → MARK
	require.True(t, sc.ShouldMarkForCache())
	require.Equal(t, 2100, sc.TotalTokensSeen())
	require.Equal(t, 0, sc.LastCachePointAt())

	// Mark and verify frozen position
	sc.MarkCachePoint()
	require.Equal(t, 2100, sc.LastCachePointAt())
	require.False(t, sc.ShouldMarkForCache()) // distance = 0 now

	// Turn 4: 500 more → distance 500, no mark
	sc.UpdateTokenCount(500)
	require.False(t, sc.ShouldMarkForCache())

	// Turn 5: 1600 more → total 4200, distance from 2100 = 2100 > 2048 → MARK
	sc.UpdateTokenCount(1600)
	require.True(t, sc.ShouldMarkForCache())

	sc.MarkCachePoint()
	require.Equal(t, 4200, sc.LastCachePointAt())
}
