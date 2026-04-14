package usage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTracker_RecordAndRecords(t *testing.T) {
	tr := NewTracker()

	r1 := Record{Tokens: TokenItems{{Kind: KindInput, Count: 100}}, RecordedAt: time.Now()}
	r2 := Record{Tokens: TokenItems{{Kind: KindOutput, Count: 50}}, RecordedAt: time.Now()}

	tr.Record(r1)
	tr.Record(r2)

	records := tr.Records()
	require.Len(t, records, 2)
	assert.Equal(t, 100, records[0].Tokens.Count(KindInput))
	assert.Equal(t, 50, records[1].Tokens.Count(KindOutput))
}

func TestTracker_Aggregate(t *testing.T) {
	tr := NewTracker()

	tr.Record(Record{
		Tokens: TokenItems{{Kind: KindInput, Count: 100}, {Kind: KindOutput, Count: 50}},
		Cost:   Cost{Total: 0.5, Input: 0.3, Output: 0.2, Source: "calculated"},
	})
	tr.Record(Record{
		Tokens: TokenItems{{Kind: KindInput, Count: 200}, {Kind: KindOutput, Count: 100}},
		Cost:   Cost{Total: 1.0, Input: 0.6, Output: 0.4, Source: "calculated"},
	})

	agg := tr.Aggregate()
	assert.Equal(t, 300, agg.Tokens.Count(KindInput))
	assert.Equal(t, 150, agg.Tokens.Count(KindOutput))
	assert.InDelta(t, 1.5, agg.Cost.Total, 0.001)
	assert.InDelta(t, 0.9, agg.Cost.Input, 0.001)
	assert.InDelta(t, 0.6, agg.Cost.Output, 0.001)
}

func TestTracker_Filter(t *testing.T) {
	tr := NewTracker()

	tr.Record(Record{Dims: Dims{Provider: "anthropic"}, Tokens: TokenItems{{Kind: KindInput, Count: 100}}})
	tr.Record(Record{Dims: Dims{Provider: "openai"}, Tokens: TokenItems{{Kind: KindInput, Count: 200}}})
	tr.Record(Record{Dims: Dims{Provider: "anthropic"}, Tokens: TokenItems{{Kind: KindInput, Count: 300}}})

	anthropic := tr.Filter(ByProvider("anthropic"))
	require.Len(t, anthropic, 2)
	assert.Equal(t, "anthropic", anthropic[0].Dims.Provider)
	assert.Equal(t, "anthropic", anthropic[1].Dims.Provider)
}

func TestTracker_ByLabel(t *testing.T) {
	tr := NewTracker()

	tr.Record(Record{
		Dims:   Dims{Labels: map[string]string{"category": "system"}},
		Tokens: TokenItems{{Kind: KindInput, Count: 100}},
	})
	tr.Record(Record{
		Dims:   Dims{Labels: map[string]string{"category": "tools"}},
		Tokens: TokenItems{{Kind: KindInput, Count: 200}},
	})

	system := tr.Filter(ByLabel("category", "system"))
	require.Len(t, system, 1)
	assert.Equal(t, 100, system[0].Tokens.Count(KindInput))
}

func TestTracker_CostEnrichment(t *testing.T) {
	// Create calculator that returns fixed cost
	calc := CostCalculatorFunc(func(provider, model string, tokens TokenItems) (Cost, bool) {
		return Cost{Total: 1.0, Source: "calculated"}, true
	})

	tr := NewTracker(WithCostCalculator(calc))

	// Record with no cost
	tr.Record(Record{
		Dims:   Dims{Provider: "test", Model: "test-model"},
		Tokens: TokenItems{{Kind: KindInput, Count: 1000}},
	})

	records := tr.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "calculated", records[0].Cost.Source)
	assert.InDelta(t, 1.0, records[0].Cost.Total, 0.001)
}

func TestTracker_CostEnrichment_SkipsReported(t *testing.T) {
	calc := CostCalculatorFunc(func(provider, model string, tokens TokenItems) (Cost, bool) {
		return Cost{Total: 999.0, Source: "calculated"}, true
	})

	tr := NewTracker(WithCostCalculator(calc))

	// Record with reported cost
	tr.Record(Record{
		Dims:   Dims{Provider: "test", Model: "test-model"},
		Tokens: TokenItems{{Kind: KindInput, Count: 1000}},
		Cost:   Cost{Total: 5.0, Source: "reported"},
	})

	records := tr.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "reported", records[0].Cost.Source)
	assert.InDelta(t, 5.0, records[0].Cost.Total, 0.001) // NOT replaced
}

func TestTracker_Budget(t *testing.T) {
	tr := NewTracker(WithBudget(Budget{MaxCostUSD: 10.0}))

	assert.True(t, tr.WithinBudget())

	tr.Record(Record{Cost: Cost{Total: 5.0}})
	assert.True(t, tr.WithinBudget())

	tr.Record(Record{Cost: Cost{Total: 6.0}})
	assert.False(t, tr.WithinBudget()) // 5 + 6 = 11 > 10
}

func TestTracker_Drift(t *testing.T) {
	tr := NewTracker()

	// Record estimate
	tr.Record(Record{
		IsEstimate: true,
		Dims:       Dims{RequestID: "req1"},
		Tokens:     TokenItems{{Kind: KindInput, Count: 1000}},
		RecordedAt: time.Now(),
	})

	// Record actual
	tr.Record(Record{
		IsEstimate: false,
		Dims:       Dims{RequestID: "req1"},
		Tokens:     TokenItems{{Kind: KindInput, Count: 1100}},
		RecordedAt: time.Now(),
	})

	drift, ok := tr.Drift("req1")
	require.True(t, ok)
	assert.Equal(t, 1000, drift.EstimatedInput)
	assert.Equal(t, 1100, drift.ActualInput)
	assert.Equal(t, 100, drift.InputDelta)
	assert.InDelta(t, 10.0, drift.InputPct, 0.01) // 100/1000 * 100
}

func TestTracker_DriftStats(t *testing.T) {
	tr := NewTracker()

	// Add 3 requests with varying drift
	for i, delta := range []int{-50, 100, 200} {
		reqID := string(rune('a' + i))
		tr.Record(Record{
			IsEstimate: true,
			Dims:       Dims{RequestID: reqID},
			Tokens:     TokenItems{{Kind: KindInput, Count: 1000}},
			RecordedAt: time.Now(),
		})
		tr.Record(Record{
			IsEstimate: false,
			Dims:       Dims{RequestID: reqID},
			Tokens:     TokenItems{{Kind: KindInput, Count: 1000 + delta}},
			RecordedAt: time.Now(),
		})
	}

	stats := tr.DriftStats()
	assert.Equal(t, 3, stats.N)
	assert.InDelta(t, -5.0, stats.MinPct, 0.1) // -50/1000 * 100
	assert.InDelta(t, 20.0, stats.MaxPct, 0.1) // 200/1000 * 100
}
