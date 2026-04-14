# Implementation Plan: Usage & Cost Tracking Overhaul

**Date**: 2026-04-14  
**Design**: `.agents/plans/DESIGN-20260414-usage-tracking.md` (rev 8)  
**Scope**: Complete rewrite of usage/cost tracking with new `usage/` package  
**Total estimated time**: ~2.5 hours (20 tasks)

---

## Context

This plan replaces `llm.Usage` with a new `usage.Record` type that separates token counts (facts) from costs (derived), adds attribution (provider/model/requestID), tracks drift between estimates and actuals, and centralizes all pricing logic.

**Breaking change**: `llm.Usage` is deleted. No backwards compatibility.

**Import graph after completion**:
```
llm (root) ──imports──▶ usage
usage      ──imports──▶ modeldb
providers  ──imports──▶ llm, usage
```

---

## Phase 1: Create `usage/` package (standalone)

All tasks in this phase build the `usage/` package in isolation. No existing code is touched. After task 7, `go test ./usage/...` passes. The root package and providers still build against the old `llm.Usage`.

---

### Task 1: Create `usage/record.go` — core types

**Files created**: `usage/record.go`  
**Estimated time**: 10 minutes

**Code to write**:

```go
package usage

import "time"

// TokenKind identifies one independently-priced token category.
type TokenKind string

const (
	// KindInput are regular (non-cache) input tokens.
	// For providers with cache support this is:
	//   total_input_tokens - cache_read_tokens - cache_write_tokens
	// Priced at the model's standard input rate.
	KindInput TokenKind = "input"

	// KindOutput are generated/completion tokens, EXCLUDING reasoning tokens.
	// When the upstream API reports reasoning tokens separately, providers MUST
	// subtract them here so that KindOutput + KindReasoning == total completions
	// with no overlap. When the API cannot distinguish, KindOutput holds the
	// full completion count and KindReasoning is omitted.
	// Priced at the model's standard output rate.
	KindOutput TokenKind = "output"

	// KindReasoning are thinking/reasoning tokens reported by the provider.
	// Only emitted when the upstream API reports them as a distinct value.
	// They are NOT included in KindOutput (no double-counting).
	// Priced at the model's reasoning rate; falls back to output rate when
	// Pricing.Reasoning == 0 (true for all current providers).
	KindReasoning TokenKind = "reasoning"

	// KindCacheRead are input tokens served from an existing prompt cache entry.
	// Anthropic: cache_read_input_tokens.
	// OpenAI:    prompt_tokens_details.cached_tokens.
	// Priced at a reduced cache-read rate; NOT included in KindInput.
	KindCacheRead TokenKind = "cache_read"

	// KindCacheWrite are input tokens written to a new prompt cache entry.
	// Anthropic / Bedrock only (cache_creation_input_tokens).
	// Priced at a cache-write rate; NOT included in KindInput.
	KindCacheWrite TokenKind = "cache_write"
)

// TokenItem is one independently-priced token entry.
type TokenItem struct {
	Kind  TokenKind `json:"kind"`
	Count int       `json:"count"`
}

// TokenItems is the ordered list of token items for a Record.
// Each kind appears at most once per record.
type TokenItems []TokenItem

// Count returns the count for the item with the given kind.
// Returns 0 if no item with that kind exists.
func (t TokenItems) Count(kind TokenKind) int {
	for _, item := range t {
		if item.Kind == kind {
			return item.Count
		}
	}
	return 0
}

// TotalInput returns Input + CacheRead + CacheWrite.
func (t TokenItems) TotalInput() int {
	return t.Count(KindInput) + t.Count(KindCacheRead) + t.Count(KindCacheWrite)
}

// TotalOutput returns Output + Reasoning.
func (t TokenItems) TotalOutput() int {
	return t.Count(KindOutput) + t.Count(KindReasoning)
}

// Total returns TotalInput + TotalOutput.
func (t TokenItems) Total() int {
	return t.TotalInput() + t.TotalOutput()
}

// NonZero returns a new slice with zero-count items removed.
func (t TokenItems) NonZero() TokenItems {
	var result TokenItems
	for _, item := range t {
		if item.Count > 0 {
			result = append(result, item)
		}
	}
	return result
}

// Cost holds monetary amounts in USD.
// All fields are derived UNLESS Source == "reported" (provider sent the value directly).
type Cost struct {
	Total      float64 `json:"total"`
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	Reasoning  float64 `json:"reasoning,omitempty"`  // zero when KindReasoning not present
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`

	// Source describes how the cost was determined.
	//   "calculated" — via CalcCost from KnownPricing or ModelDB
	//   "reported"   — API-provided total (OpenRouter)
	//   "estimated"  — pre-request estimate from CountTokens
	//   ""           — no pricing available (Ollama local, unknown model)
	Source string `json:"source,omitempty"`
}

func (c Cost) IsZero() bool { return c.Source == "" && c.Total == 0 }

// Dims carries attribution context for a Record.
type Dims struct {
	Provider  string            `json:"provider,omitempty"`
	Model     string            `json:"model,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	TurnID    string            `json:"turn_id,omitempty"`    // caller-assigned turn identifier
	SessionID string            `json:"session_id,omitempty"` // caller-assigned session identifier

	// Labels are arbitrary string key-value annotations on the Record.
	// Used to distinguish sub-breakdowns within a request, e.g. in estimates:
	//   {"category": "system"}       — system prompt tokens
	//   {"category": "conversation"} — conversation history tokens
	//   {"category": "tools"}        — tool definition tokens
	// Provider-reported records carry no labels.
	Labels map[string]string `json:"labels,omitempty"`
}

// Record is a single, fully-attributed usage record.
type Record struct {
	Tokens     TokenItems    `json:"tokens"`
	Cost       Cost          `json:"cost"`
	Dims       Dims          `json:"dims"`
	IsEstimate bool          `json:"is_estimate,omitempty"`
	RecordedAt time.Time     `json:"recorded_at"`

	// Extras holds provider-specific metadata captured at request time.
	// Mirrors the StreamStartedEvent.Extra convention (same map[string]any type).
	// Keys and value types are defined per provider:
	//
	//   Anthropic / Claude-OAuth: "rate_limits" -> *llm.RateLimits
	//     Contains 5h/7d window utilisation, overage status, fallback percentage,
	//     and representative claim. Populated from HTTP response headers.
	//
	// nil for estimate records and for providers that return no extras.
	Extras map[string]any `json:"extras,omitempty"`
}
```

**Verification**:
```bash
go build ./usage/...
```

Expected: Clean build, no errors.

---

### Task 2: Create `usage/calculator.go` — CostCalculator interface

**Files created**: `usage/calculator.go`  
**Estimated time**: 3 minutes

**Code to write**:

```go
package usage

// CostCalculator computes a Cost for a given provider, model, and token items.
type CostCalculator interface {
	// Calculate returns (Cost, true) when pricing is known,
	// (Cost{}, false) when the provider+model has no entry.
	Calculate(provider, model string, tokens TokenItems) (Cost, bool)
}

// CostCalculatorFunc is a function that implements CostCalculator.
type CostCalculatorFunc func(provider, model string, tokens TokenItems) (Cost, bool)

func (f CostCalculatorFunc) Calculate(p, m string, t TokenItems) (Cost, bool) { return f(p, m, t) }
```

**Verification**:
```bash
go build ./usage/...
```

---

### Task 3: Create `usage/pricing.go` — centralized pricing + CalcCost

**Files created**: `usage/pricing.go`  
**Estimated time**: 20 minutes

**Mandatory preparation — do this before writing a single line of code:**

Open each file below and copy every model+price entry into `KnownPricing`. The stub entries in the code below are *illustrative examples only* — the real prices must come from the provider files.

```bash
# Read all four files first:
cat provider/anthropic/models.go   # look for allModels / modelPricingRegistry
cat provider/bedrock/models.go     # look for fillCost / pricing map
cat provider/openai/models.go      # look for calculateCost / pricing map
cat provider/minimax/models.go     # look for FillCost / pricing
```

Do not skip this step. The TODO comments in `KnownPricing` mark locations that MUST be filled before the file is complete. A test in Task 4 verifies the entries exist.

**Code to write**:

```go
package usage

import (
	"strings"

	"github.com/codewandler/llm/modeldb"
)

// Pricing holds per-token rates in USD per million tokens.
type Pricing struct {
	Input       float64 `json:"input"`
	Output      float64 `json:"output"`
	Reasoning   float64 `json:"reasoning,omitempty"`   // 0 = same rate as Output
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
	// Anthropic — extract ALL entries from provider/anthropic/models.go
	{"anthropic", "claude-opus-4-6", Pricing{Input: 15.0, Output: 75.0, CachedInput: 1.50, CacheWrite: 18.75}},
	{"anthropic", "claude-sonnet-4-6", Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75}},
	{"anthropic", "claude-haiku-4-5-20251001", Pricing{Input: 1.0, Output: 5.0, CachedInput: 0.10, CacheWrite: 1.25}},
	// TODO: Add ALL other Anthropic models from provider/anthropic/models.go

	// OpenAI — extract ALL entries from provider/openai/models.go
	{"openai", "gpt-4o", Pricing{Input: 2.50, Output: 10.0, CachedInput: 1.25}},
	{"openai", "gpt-4o-mini", Pricing{Input: 0.15, Output: 0.60, CachedInput: 0.075}},
	{"openai", "o1", Pricing{Input: 15.0, Output: 60.0}},
	{"openai", "o1-mini", Pricing{Input: 3.0, Output: 12.0}},
	// TODO: Add ALL other OpenAI models

	// Bedrock — same models as Anthropic but with "anthropic." prefix
	{"bedrock", "anthropic.claude-sonnet-4-6", Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75}},
	// TODO: Add ALL Bedrock model entries

	// MiniMax — extract from provider/minimax/models.go
	// TODO: Add MiniMax entries
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

// Default returns the recommended default calculator:
//   Compose(Static(), ModelDB())
// Static() is checked first because KnownPricing is manually maintained and
// verified against provider docs. ModelDB() provides broader coverage.
func Default() CostCalculator {
	return Compose(Static(), ModelDB())
}
```

**Action required during implementation**: Complete the `KnownPricing` array by extracting ALL entries from the four provider files. The TODO comments mark where entries must be added.

**Verification**:
```bash
go build ./usage/...
```

---

### Task 4: Create `usage/pricing_test.go` — verify CalcCost + pricing lookups

**Files created**: `usage/pricing_test.go`  
**Estimated time**: 10 minutes

**Code to write**:

```go
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
			name:   "input only",
			tokens: TokenItems{{Kind: KindInput, Count: 1_000_000}},
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
				Input:      1.5,   // 500k * 3.0 / 1M
				CacheRead:  0.09,  // 300k * 0.30 / 1M
				CacheWrite: 0.75,  // 200k * 3.75 / 1M
				Output:     1.5,   // 100k * 15.0 / 1M
				Total:      3.84,
				Source:     "calculated",
			},
		},
		{
			name:   "reasoning with explicit rate",
			tokens: TokenItems{{Kind: KindReasoning, Count: 1_000_000}},
			pricing: Pricing{Output: 15.0, Reasoning: 20.0},
			wantCost: Cost{
				Reasoning: 20.0,
				Total:     20.0,
				Source:    "calculated",
			},
		},
		{
			name:   "reasoning fallback to output rate",
			tokens: TokenItems{{Kind: KindReasoning, Count: 1_000_000}},
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
```

**Verification**:
```bash
go test -v ./usage/...
```

All tests must pass.

---

### Task 5: Create `usage/budget.go`

**Files created**: `usage/budget.go`  
**Estimated time**: 3 minutes

**Code to write**:

```go
package usage

// Budget defines spending and token ceilings. Zero value means "no limit".
type Budget struct {
	MaxCostUSD      float64
	MaxInputTokens  int
	MaxOutputTokens int
	MaxTotalTokens  int
}

// Exceeded returns true when agg violates any non-zero limit.
func (b Budget) Exceeded(agg Record) bool {
	if b.MaxCostUSD > 0 && agg.Cost.Total >= b.MaxCostUSD {
		return true
	}
	totalInput := agg.Tokens.TotalInput()
	if b.MaxInputTokens > 0 && totalInput >= b.MaxInputTokens {
		return true
	}
	totalOutput := agg.Tokens.TotalOutput()
	if b.MaxOutputTokens > 0 && totalOutput >= b.MaxOutputTokens {
		return true
	}
	if b.MaxTotalTokens > 0 && (totalInput+totalOutput) >= b.MaxTotalTokens {
		return true
	}
	return false
}
```

**Verification**:
```bash
go build ./usage/...
```

---

### Task 6: Create `usage/drift.go`

**Files created**: `usage/drift.go`  
**Estimated time**: 5 minutes

**Code to write**:

```go
package usage

import "math"

// Drift holds the delta between the unlabeled pre-request estimate and the
// provider-reported actual for a single request.
type Drift struct {
	Dims Dims // from the actual Record (provider, model, requestID, ...)

	EstimatedInput int // Count(KindInput) from the unlabeled estimate
	ActualInput    int // Count(KindInput) from the actual record

	// InputDelta = ActualInput - EstimatedInput.
	// Positive = underestimate (provider used more tokens than predicted).
	// Negative = overestimate (provider used fewer tokens than predicted).
	InputDelta int

	// InputPct = InputDelta / EstimatedInput * 100.
	// math.NaN() when EstimatedInput == 0.
	InputPct float64

	Estimate Record // the matched estimate (IsEstimate == true, Labels == nil)
	Actual   Record // the matched provider-reported actual
}

// DriftStats aggregates drift across multiple matched request pairs.
type DriftStats struct {
	N       int     // number of matched pairs
	MinPct  float64 // best-case (most negative = largest overestimate)
	MaxPct  float64 // worst-case (most positive = largest underestimate)
	MeanPct float64
	P50Pct  float64 // median
	P95Pct  float64 // 95th percentile — useful for worst-case budget planning
}

// ComputeDrift creates a Drift from an estimate and actual record.
// Returns nil if estimate or actual is nil, or if estimate is not an estimate.
func ComputeDrift(estimate, actual *Record) *Drift {
	if estimate == nil || actual == nil || !estimate.IsEstimate {
		return nil
	}

	estInput := estimate.Tokens.Count(KindInput)
	actInput := actual.Tokens.Count(KindInput)
	delta := actInput - estInput

	pct := math.NaN()
	if estInput > 0 {
		pct = float64(delta) / float64(estInput) * 100.0
	}

	return &Drift{
		Dims:           actual.Dims,
		EstimatedInput: estInput,
		ActualInput:    actInput,
		InputDelta:     delta,
		InputPct:       pct,
		Estimate:       *estimate,
		Actual:         *actual,
	}
}
```

**Verification**:
```bash
go build ./usage/...
```

---

### Task 7: Create `usage/tracker.go` + test

**Files created**: `usage/tracker.go`, `usage/tracker_test.go`  
**Estimated time**: 20 minutes

**Code to write** (`usage/tracker.go`):

```go
package usage

import (
	"math"
	"sort"
	"sync"
	"time"
)

type Tracker struct {
	mu         sync.Mutex
	records    []Record
	budget     Budget
	calculator CostCalculator
	sessionID  string
}

// TrackerOption configures a Tracker.
type TrackerOption func(*Tracker)

func WithBudget(b Budget) TrackerOption {
	return func(t *Tracker) { t.budget = b }
}

func WithSessionID(id string) TrackerOption {
	return func(t *Tracker) { t.sessionID = id }
}

func WithCostCalculator(c CostCalculator) TrackerOption {
	return func(t *Tracker) { t.calculator = c }
}

func NewTracker(opts ...TrackerOption) *Tracker {
	t := &Tracker{}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Record appends r to the history.
// If r.Cost.IsZero() and a CostCalculator is configured, the tracker attempts
// to fill cost before storing. Records with Source == "reported" are never
// recalculated.
func (t *Tracker) Record(r Record) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Enrich cost if zero and not reported
	if r.Cost.IsZero() && r.Cost.Source != "reported" && t.calculator != nil {
		if cost, ok := t.calculator.Calculate(r.Dims.Provider, r.Dims.Model, r.Tokens); ok {
			r.Cost = cost
		}
	}

	t.records = append(t.records, r)
}

func (t *Tracker) Records() []Record {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]Record, len(t.records))
	copy(result, t.records)
	return result
}

// Aggregate returns a Record whose Tokens and Cost fields are the sum of all
// non-estimate records. Dims is zero-valued (the aggregate has no single owner).
func (t *Tracker) Aggregate() Record {
	t.mu.Lock()
	defer t.mu.Unlock()

	var agg Record
	agg.RecordedAt = time.Now()

	// Build aggregated token items
	counts := make(map[TokenKind]int)
	var totalCost Cost

	for _, r := range t.records {
		if r.IsEstimate {
			continue
		}
		for _, item := range r.Tokens {
			counts[item.Kind] += item.Count
		}
		totalCost.Total += r.Cost.Total
		totalCost.Input += r.Cost.Input
		totalCost.Output += r.Cost.Output
		totalCost.Reasoning += r.Cost.Reasoning
		totalCost.CacheRead += r.Cost.CacheRead
		totalCost.CacheWrite += r.Cost.CacheWrite
		if totalCost.Source == "" {
			totalCost.Source = r.Cost.Source
		}
	}

	for kind, count := range counts {
		if count > 0 {
			agg.Tokens = append(agg.Tokens, TokenItem{Kind: kind, Count: count})
		}
	}
	agg.Cost = totalCost

	return agg
}

// FilterFunc is a predicate for filtering records.
type FilterFunc func(Record) bool

func (t *Tracker) Filter(fs ...FilterFunc) []Record {
	t.mu.Lock()
	defer t.mu.Unlock()

	var result []Record
outer:
	for _, r := range t.records {
		for _, f := range fs {
			if !f(r) {
				continue outer
			}
		}
		result = append(result, r)
	}
	return result
}

func (t *Tracker) WithinBudget() bool {
	agg := t.Aggregate()
	return !t.budget.Exceeded(agg)
}

func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = nil
}

// Drift computes the drift for a given requestID.
// Matches the first unlabeled estimate against the first actual record.
// Returns (nil, false) if no complete estimate+actual pair exists.
func (t *Tracker) Drift(requestID string) (*Drift, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var estimate, actual *Record
	for i := range t.records {
		r := &t.records[i]
		if r.Dims.RequestID != requestID {
			continue
		}
		if r.IsEstimate && r.Dims.Labels == nil && estimate == nil {
			estimate = r
		}
		if !r.IsEstimate && actual == nil {
			actual = r
		}
		if estimate != nil && actual != nil {
			break
		}
	}

	if estimate == nil || actual == nil {
		return nil, false
	}

	return ComputeDrift(estimate, actual), true
}

// Drifts returns drift for all requests with a complete pair,
// ordered by Actual.RecordedAt.
func (t *Tracker) Drifts() []Drift {
	t.mu.Lock()
	defer t.mu.Unlock()

	type pair struct {
		estimate Record
		actual   Record
	}

	pairsByID := make(map[string]*pair)

	for _, r := range t.records {
		if r.Dims.RequestID == "" {
			continue
		}

		p, ok := pairsByID[r.Dims.RequestID]
		if !ok {
			p = &pair{}
			pairsByID[r.Dims.RequestID] = p
		}

		if r.IsEstimate && r.Dims.Labels == nil {
			p.estimate = r
		}
		if !r.IsEstimate {
			p.actual = r
		}
	}

	var drifts []Drift
	for _, p := range pairsByID {
		if !p.estimate.RecordedAt.IsZero() && !p.actual.RecordedAt.IsZero() {
			if d := ComputeDrift(&p.estimate, &p.actual); d != nil {
				drifts = append(drifts, *d)
			}
		}
	}

	// Sort by actual RecordedAt
	sort.Slice(drifts, func(i, j int) bool {
		return drifts[i].Actual.RecordedAt.Before(drifts[j].Actual.RecordedAt)
	})

	return drifts
}

// DriftStats returns aggregate statistics across all matched pairs.
// Returns zero-value DriftStats when no pairs exist.
func (t *Tracker) DriftStats() DriftStats {
	drifts := t.Drifts()
	if len(drifts) == 0 {
		return DriftStats{}
	}

	pcts := make([]float64, 0, len(drifts))
	var sum float64
	var min, max float64 = math.MaxFloat64, -math.MaxFloat64

	for _, d := range drifts {
		if !math.IsNaN(d.InputPct) {
			pcts = append(pcts, d.InputPct)
			sum += d.InputPct
			if d.InputPct < min {
				min = d.InputPct
			}
			if d.InputPct > max {
				max = d.InputPct
			}
		}
	}

	if len(pcts) == 0 {
		return DriftStats{N: len(drifts)}
	}

	sort.Float64s(pcts)
	mean := sum / float64(len(pcts))
	p50 := pcts[len(pcts)/2]
	p95Idx := int(float64(len(pcts)) * 0.95)
	if p95Idx >= len(pcts) {
		p95Idx = len(pcts) - 1
	}
	p95 := pcts[p95Idx]

	return DriftStats{
		N:       len(drifts),
		MinPct:  min,
		MaxPct:  max,
		MeanPct: mean,
		P50Pct:  p50,
		P95Pct:  p95,
	}
}

// Filter helpers

func ByProvider(name string) FilterFunc {
	return func(r Record) bool { return r.Dims.Provider == name }
}

func ByModel(model string) FilterFunc {
	return func(r Record) bool { return r.Dims.Model == model }
}

func ByTurnID(id string) FilterFunc {
	return func(r Record) bool { return r.Dims.TurnID == id }
}

func BySessionID(id string) FilterFunc {
	return func(r Record) bool { return r.Dims.SessionID == id }
}

func EstimatesOnly() FilterFunc {
	return func(r Record) bool { return r.IsEstimate }
}

func ExcludeEstimates() FilterFunc {
	return func(r Record) bool { return !r.IsEstimate }
}

func Since(t time.Time) FilterFunc {
	return func(r Record) bool { return r.RecordedAt.After(t) }
}

func ByLabel(key, value string) FilterFunc {
	return func(r Record) bool {
		if r.Dims.Labels == nil {
			return false
		}
		return r.Dims.Labels[key] == value
	}
}
```

**Code to write** (`usage/tracker_test.go`):

```go
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
	assert.InDelta(t, -5.0, stats.MinPct, 0.1)  // -50/1000 * 100
	assert.InDelta(t, 20.0, stats.MaxPct, 0.1)  // 200/1000 * 100
}
```

**Verification**:
```bash
go test -v -race ./usage/...
```

All tests must pass.

---

## Phase 2: Root package interface changes

After this phase, `go build ./...` will FAIL. All providers reference deleted types. This is expected. Phase 3 fixes them.

---

### Task 8: Delete `usage.go`, update `event.go`, `event_publisher.go`, `response.go`

**Files deleted**: `usage.go`  
**Files modified**: `event.go`, `event_publisher.go`, `response.go`  
**Estimated time**: 10 minutes

**Step 1**: Delete `usage.go`:
```bash
rm usage.go
```

**Step 2**: Edit `event.go`:

At top of file, add import:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
)
```

Add new event type constant (after existing EventType constants):
```go
StreamEventTokenEstimate EventType = "token_estimate"
```

Replace the entire `UsageUpdatedEvent` struct definition with:
```go
type UsageUpdatedEvent struct {
	Record usage.Record `json:"record"`
}
```

**Important**: the `Type()` method on `UsageUpdatedEvent` must be kept. After replacing the struct, verify that this method still exists in `event.go`:
```go
func (e *UsageUpdatedEvent) Type() EventType { return StreamEventUsageUpdated }
```
If it was part of the struct definition block you just replaced, re-add it immediately after the struct.

Add new event type (after `UsageUpdatedEvent`):
```go
// TokenEstimateEvent is dispatched before the first response delta.
// It carries the pre-request token estimate so consumers can display
// estimates and drift without calling CountTokens themselves.
type TokenEstimateEvent struct {
	// Estimate is one pre-request estimate record.
	// The event is emitted once per record; multiple events may be emitted per request
	// when a labeled breakdown is provided (each with distinct Dims.Labels).
	Estimate usage.Record `json:"estimate"` // IsEstimate == true
}

func (e TokenEstimateEvent) Type() EventType { return StreamEventTokenEstimate }
```

In the `Publisher` interface, **remove** this method:
```go
Usage(usage Usage)
```

In the `Publisher` interface, **add** these two methods:
```go
UsageRecord(r usage.Record)
TokenEstimate(r usage.Record)
```

**Step 3**: Edit `event_publisher.go`:

Find and **delete** this method (line 75):
```go
func (s *eventPub) Usage(usage Usage) { s.Publish(&UsageUpdatedEvent{Usage: usage}) }
```

**Add** these two methods at the end of the file:
```go
func (s *eventPub) UsageRecord(r usage.Record) { s.Publish(&UsageUpdatedEvent{Record: r}) }
func (s *eventPub) TokenEstimate(r usage.Record) { s.Publish(&TokenEstimateEvent{Estimate: r}) }
```

**Step 4**: Edit `response.go`:

In the `Response` interface, **remove** this method (line 35):
```go
Usage() *Usage
```

**Verification**:
```bash
go build ./usage/...  # still passes
go build .            # root llm package now compiles; providers do not yet
# go build ./... will FAIL — expected, providers not migrated yet
```

---

### Task 9: Update `event_processor.go` + `event_processor_test.go`

**Files modified**: `event_processor.go`, `event_processor_test.go`  
**Estimated time**: 15 minutes

**Edit `event_processor.go`**:

Add import at top:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
)
```

**Update the `Result` interface** (lines 15-18) — add the three new methods:
```go
type Result interface {
	Response
	Next() msg.Messages
	UsageRecords()   []usage.Record  // provider-reported, in arrival order
	TokenEstimates() []usage.Record  // pre-request estimates, in order
	Drift()          *usage.Drift    // nil if no estimate received
}
```

The compile-time assertion `var _ Result = (*result)(nil)` at line 132 will immediately flag any missing method implementations.

In the `result` struct, **replace**:
```go
usage *Usage
```
**with**:
```go
usageRecords []usage.Record
estimateRecs []usage.Record
```

**Remove** this method:
```go
func (r *result) applyUsage(u *Usage) {
	if r.usage == nil {
		r.usage = u
	}
}
```

**Add** these methods:
```go
func (r *result) applyUsage(rec usage.Record) {
	r.usageRecords = append(r.usageRecords, rec)
}

func (r *result) applyEstimate(rec usage.Record) {
	r.estimateRecs = append(r.estimateRecs, rec)
}
```

In `processEvent` function, find the `*UsageUpdatedEvent` case and **replace**:
```go
case *UsageUpdatedEvent:
	r.result.applyUsage(&actual.Usage)
```
**with**:
```go
case *UsageUpdatedEvent:
	r.result.applyUsage(actual.Record)
case *TokenEstimateEvent:
	r.result.applyEstimate(actual.Estimate)
```

**Remove** this method from `result`:
```go
func (r *result) Usage() *Usage {
	return r.usage
}
```

**Add** these methods to `result`:
```go
func (r *result) UsageRecords() []usage.Record {
	return r.usageRecords
}

func (r *result) TokenEstimates() []usage.Record {
	return r.estimateRecs
}

func (r *result) Drift() *usage.Drift {
	if len(r.estimateRecs) == 0 || len(r.usageRecords) == 0 {
		return nil
	}
	// Find first unlabeled estimate
	var estimate *usage.Record
	for i := range r.estimateRecs {
		if r.estimateRecs[i].Dims.Labels == nil {
			estimate = &r.estimateRecs[i]
			break
		}
	}
	if estimate == nil {
		return nil
	}
	// Match with first actual
	return usage.ComputeDrift(estimate, &r.usageRecords[0])
}
```

Find the `ResponseJSON` struct (used in `MarshalJSON`, around line 48). **Replace**:
```go
Usage *Usage `json:"usage,omitempty"`
```
**with**:
```go
UsageRecords []usage.Record `json:"usage_records,omitempty"`
```

In the `MarshalJSON` method, **replace**:
```go
Usage: r.Usage(),
```
**with**:
```go
UsageRecords: r.UsageRecords(),
```

**Edit `event_processor_test.go`**:

Add import:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
)
```

Find `TestEventProcessor_Empty` test. **Replace**:
```go
assert.Nil(t, result.Usage())
```
**with**:
```go
assert.Empty(t, result.UsageRecords())
assert.Empty(t, result.TokenEstimates())
assert.Nil(t, result.Drift())
```

Find `TestEventProcessor_Usage` test. **Replace all old assertions** with:
```go
func TestEventProcessor_Usage(t *testing.T) {
	proc := NewEventProcessor(context.Background())

	ch := make(chan Envelope, 10)
	ch <- Envelope{Data: &StreamCreatedEvent{}}
	ch <- Envelope{Data: &UsageUpdatedEvent{
		Record: usage.Record{
			Tokens: usage.TokenItems{{Kind: usage.KindInput, Count: 10}, {Kind: usage.KindOutput, Count: 5}},
			Cost:   usage.Cost{Total: 0.001, Source: "calculated"},
		},
	}}
	close(ch)

	result := proc.Process(ch)

	require.Len(t, result.UsageRecords(), 1)
	assert.Equal(t, 10, result.UsageRecords()[0].Tokens.Count(usage.KindInput))
	assert.Equal(t, 5, result.UsageRecords()[0].Tokens.Count(usage.KindOutput))
	assert.InDelta(t, 0.001, result.UsageRecords()[0].Cost.Total, 0.0001)
}
```

**Add new test**:
```go
func TestEventProcessor_Drift(t *testing.T) {
	proc := NewEventProcessor(context.Background())

	ch := make(chan Envelope, 10)
	ch <- Envelope{Data: &StreamCreatedEvent{}}
	ch <- Envelope{Data: &TokenEstimateEvent{
		Estimate: usage.Record{
			IsEstimate: true,
			Tokens:     usage.TokenItems{{Kind: usage.KindInput, Count: 1000}},
			Dims:       usage.Dims{RequestID: "req1"},
		},
	}}
	ch <- Envelope{Data: &UsageUpdatedEvent{
		Record: usage.Record{
			Tokens: usage.TokenItems{{Kind: usage.KindInput, Count: 1100}},
			Dims:   usage.Dims{RequestID: "req1"},
		},
	}}
	close(ch)

	result := proc.Process(ch)

	drift := result.Drift()
	require.NotNil(t, drift)
	assert.Equal(t, 1000, drift.EstimatedInput)
	assert.Equal(t, 1100, drift.ActualInput)
	assert.Equal(t, 100, drift.InputDelta)
	assert.InDelta(t, 10.0, drift.InputPct, 0.01)
}
```

**Verification**:
```bash
go test ./usage/...  # still passes
go build .           # root llm package (event_processor.go) still compiles
# provider tests will still fail until llmtest is updated
```

---

### Task 10: Update `model.go`, add `CostCalculatorProvider` interface

**Files modified**: `model.go`  
**Files created**: `cost.go` (or modify `provider.go`)  
**Estimated time**: 5 minutes

**Edit `model.go`**:

Add import at top:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
)
```

In the `Model` struct, **add** this field (after `Aliases`):
```go
Pricing *usage.Pricing `json:"pricing,omitempty"` // nil unless fetched dynamically
```

**Add** this function at end of file:
```go
// ModelListCalculator returns a CostCalculator backed by a dynamically-fetched model list.
// Intended to be composed after Static() for providers that populate Model.Pricing:
//
//   usage.Compose(usage.Static(), llm.ModelListCalculator(p.Models()), usage.ModelDB())
func ModelListCalculator(models Models) usage.CostCalculator {
	return usage.CostCalculatorFunc(func(_, model string, tokens usage.TokenItems) (usage.Cost, bool) {
		for _, m := range models {
			if m.ID == model && m.Pricing != nil {
				return usage.CalcCost(tokens, *m.Pricing), true
			}
		}
		return usage.Cost{}, false
	})
}
```

**Create new file `cost.go`** (or append to `provider.go`):

```go
package llm

import "github.com/codewandler/llm/usage"

// CostCalculatorProvider is an optional interface providers may implement to
// expose a calculator. Useful for Tracker injection and offline estimation.
type CostCalculatorProvider interface {
	CostCalculator() usage.CostCalculator
}
```

**Verification**:
```bash
go build ./usage/...
go build .           # root llm package: model.go + cost.go should compile cleanly
# Providers still won't build
```

---

## Phase 3: Provider migrations

After ALL tasks in this phase complete, `go build ./...` will pass.

---

### Task 11: Migrate `provider/anthropic/`

**Files modified**: `provider/anthropic/stream.go`, `provider/anthropic/stream_processor.go`, `provider/anthropic/models.go`, `provider/anthropic/anthropic.go`, `provider/anthropic/claude/provider.go`, test files  
**Estimated time**: 20 minutes

**Step 1**: Edit `provider/anthropic/stream.go`:

`CostFn` was used to let MiniMax override cost calculation via `ParseOpts`. After this migration cost is calculated inside `onMessageStop` using `usage.Default()` with `p.meta.ProviderName` as the provider key, so `CostFn` is no longer needed.

**Delete** the entire `CostFn` type definition (line 14):
```go
// DELETE this line:
type CostFn func(model string, usage *llm.Usage)
```

**Remove** the `CostFn` field from `ParseOpts` struct (delete these two lines):
```go
// DELETE:
// CostFn overrides the default Anthropic cost calculation.
// When nil, FillCost (Anthropic pricing) is used.
CostFn CostFn
```

Keep all other fields in `ParseOpts` unchanged.

**Step 2**: Edit `provider/anthropic/models.go`:

**Delete** these items entirely:
- `modelPricingRegistry` map or array
- `pricingPrefixes` array (if it exists)
- `modelPricing` struct definition
- `FillCost` function
- `CalculateCost` function

Keep the `Models()` function and model-list code — only remove pricing-related code.

**Step 3**: Edit `provider/anthropic/models_test.go`:

**Delete** all tests named `TestCalculateCost*` and `TestFillCost*`.

**Step 4**: Edit `provider/anthropic/stream_processor.go`:

Add imports:
```go
import (
    // ... existing imports ...
    "time"

    "github.com/codewandler/llm/usage"
)
```

In the `streamProcessor` struct, **replace** the `usage llm.Usage` field with four raw ints and a request ID:
```go
// REMOVE:
usage       llm.Usage

// ADD:
regularInput    int    // evt.Message.Usage.InputTokens (non-cache portion)
cacheReadTokens int
cacheWriteTokens int
outputTokens    int
requestID       string // stored from StreamStartedEvent for Record.Dims
```

In `onMessageStart`, **replace**:
```go
p.usage.CacheWriteTokens = evt.Message.Usage.CacheCreationInputTokens
p.usage.CacheReadTokens = evt.Message.Usage.CacheReadInputTokens
p.usage.InputTokens = evt.Message.Usage.InputTokens +
	p.usage.CacheWriteTokens + p.usage.CacheReadTokens
```
**with**:
```go
p.cacheWriteTokens = evt.Message.Usage.CacheCreationInputTokens
p.cacheReadTokens  = evt.Message.Usage.CacheReadInputTokens
p.regularInput     = evt.Message.Usage.InputTokens
// Note: evt.Message.Usage.InputTokens is already the non-cache portion.
p.requestID = evt.Message.ID
```

In `onMessageDelta`, **replace**:
```go
p.usage.OutputTokens = evt.Usage.OutputTokens
p.usage.TotalTokens = p.usage.InputTokens + p.usage.OutputTokens
```
**with**:
```go
p.outputTokens = evt.Usage.OutputTokens
```

In `onMessageStop`, **replace the entire method body**:
```go
func (p *streamProcessor) onMessageStop() {
	tokens := usage.TokenItems{
		{Kind: usage.KindInput,      Count: p.regularInput},
		{Kind: usage.KindCacheRead,  Count: p.cacheReadTokens},
		{Kind: usage.KindCacheWrite, Count: p.cacheWriteTokens},
		{Kind: usage.KindOutput,     Count: p.outputTokens},
	}.NonZero()

	var extras map[string]any
	if p.rateLimits != nil {
		extras = map[string]any{"rate_limits": p.rateLimits}
	}

	rec := usage.Record{
		Dims: usage.Dims{
			// Use p.meta.ProviderName so MiniMax (which reuses this parser)
			// gets its own provider name in the record, not "anthropic".
			Provider:  p.meta.ProviderName,
			Model:     p.meta.Model,
			RequestID: p.requestID,
		},
		Tokens:     tokens,
		Extras:     extras,
		RecordedAt: time.Now(),
	}

	if cost, ok := usage.Default().Calculate(p.meta.ProviderName, p.meta.Model, tokens); ok {
		rec.Cost = cost
	}

	p.pub.UsageRecord(rec)
	p.pub.Completed(llm.CompletedEvent{StopReason: p.stopReason})
}
```

**Step 5**: Edit `provider/anthropic/anthropic.go`:

Add import:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
    "github.com/codewandler/llm/tokencount"
)
```

**Add** this method to the `Provider` type:
```go
func (*Provider) CostCalculator() usage.CostCalculator {
	return usage.Default()
}
```

In the `CreateStream` method, find where `PublishRequestParams` is called. **After that call**, add:

```go
// Emit token estimate
if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
	Model:    opts.Model,
	Messages: opts.Messages,
	Tools:    opts.Tools,
}); err == nil {
	tokens := usage.TokenItems{{Kind: usage.KindInput, Count: est.InputTokens}}
	rec := usage.Record{
		IsEstimate: true,
		RecordedAt: time.Now(),
		Dims:       usage.Dims{Provider: llm.ProviderNameAnthropic, Model: opts.Model},
		Tokens:     tokens,
	}
	if cost, ok := usage.Default().Calculate(llm.ProviderNameAnthropic, opts.Model, tokens); ok {
		rec.Cost = cost
		rec.Cost.Source = "estimated"
	}
	pub.TokenEstimate(rec)
}
```

**Step 6**: Edit `provider/anthropic/claude/provider.go`:

Same two changes as `anthropic.go`: add `CostCalculator()` method and emit `TokenEstimateEvent` in `CreateStream` using `llm.ProviderNameClaude` (or whatever the Claude provider name constant is) instead of `llm.ProviderNameAnthropic`.

**Step 7**: Edit test files:

In `provider/anthropic/cache_stream_test.go` and `provider/anthropic/stream_processor_test.go`, find any assertions on `llm.Usage` fields. Replace:
```go
var doneUsage *llm.Usage
// ...
ue := env.Data.(*llm.UsageUpdatedEvent)
doneUsage = &ue.Usage
```
**with**:
```go
var doneRec *usage.Record
// ...
ue := env.Data.(*llm.UsageUpdatedEvent)
doneRec = &ue.Record
```

Update field accesses:
- `doneUsage.InputTokens` → `doneRec.Tokens.Count(usage.KindInput) + doneRec.Tokens.Count(usage.KindCacheRead) + doneRec.Tokens.Count(usage.KindCacheWrite)`
- `doneUsage.OutputTokens` → `doneRec.Tokens.Count(usage.KindOutput)`
- `doneUsage.CacheReadTokens` → `doneRec.Tokens.Count(usage.KindCacheRead)`
- `doneUsage.CacheWriteTokens` → `doneRec.Tokens.Count(usage.KindCacheWrite)`
- `doneUsage.Cost` → `doneRec.Cost.Total`

**Verification**:
```bash
go build ./provider/anthropic/...
go test ./provider/anthropic/...
```

---

### Task 12: Migrate `provider/bedrock/`

**Files modified**: `provider/bedrock/models.go`, `provider/bedrock/bedrock.go`  
**Estimated time**: 10 minutes

**Step 1**: Edit `provider/bedrock/models.go`:

**Delete** the `fillCost` function and any pricing struct definitions. Keep the model-list functions.

**Step 2**: Edit `provider/bedrock/bedrock.go`:

Add imports:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
    "github.com/codewandler/llm/tokencount"
)
```

Find `var usage llm.Usage` at around line 700. **Replace** it with four raw ints:
```go
// REMOVE:
var usage llm.Usage

// ADD:
var inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int
```

In the `*types.ConverseStreamOutputMemberMetadata` case (this is where token counts arrive from Bedrock), **replace** the `usage.XXX = ...` block:
```go
// REMOVE the entire if e.Value.Usage != nil { ... } block:
if e.Value.Usage != nil {
    if e.Value.Usage.InputTokens != nil {
        usage.InputTokens = int(*e.Value.Usage.InputTokens)
    }
    if e.Value.Usage.OutputTokens != nil {
        usage.OutputTokens = int(*e.Value.Usage.OutputTokens)
    }
    if e.Value.Usage.TotalTokens != nil {
        usage.TotalTokens = int(*e.Value.Usage.TotalTokens)
    }
    if e.Value.Usage.CacheReadInputTokens != nil {
        usage.CacheReadTokens = int(*e.Value.Usage.CacheReadInputTokens)
    }
    if e.Value.Usage.CacheWriteInputTokens != nil {
        usage.CacheWriteTokens = int(*e.Value.Usage.CacheWriteInputTokens)
    }
    usage.InputTokens += usage.CacheReadTokens + usage.CacheWriteTokens
    fillCost(meta.ResolvedModel, &usage)
}
pub.Usage(usage)

// ADD this replacement:
if e.Value.Usage != nil {
    if e.Value.Usage.InputTokens != nil {
        inputTokens = int(*e.Value.Usage.InputTokens)
    }
    if e.Value.Usage.OutputTokens != nil {
        outputTokens = int(*e.Value.Usage.OutputTokens)
    }
    if e.Value.Usage.CacheReadInputTokens != nil {
        cacheReadTokens = int(*e.Value.Usage.CacheReadInputTokens)
    }
    if e.Value.Usage.CacheWriteInputTokens != nil {
        cacheWriteTokens = int(*e.Value.Usage.CacheWriteInputTokens)
    }
    // inputTokens from Bedrock is the non-cache portion (same as Anthropic).
    // Do NOT add cacheRead/cacheWrite to it — KindInput must be non-cache only.
}

tokens := usage.TokenItems{
    {Kind: usage.KindInput,      Count: inputTokens},
    {Kind: usage.KindCacheRead,  Count: cacheReadTokens},
    {Kind: usage.KindCacheWrite, Count: cacheWriteTokens},
    {Kind: usage.KindOutput,     Count: outputTokens},
}.NonZero()

rec := usage.Record{
    Dims:       usage.Dims{Provider: llm.ProviderNameBedrock, Model: meta.ResolvedModel},
    Tokens:     tokens,
    RecordedAt: time.Now(),
}
if cost, ok := usage.Default().Calculate(llm.ProviderNameBedrock, meta.ResolvedModel, tokens); ok {
    rec.Cost = cost
}
pub.UsageRecord(rec)
```

**Note**: `meta.ResolvedModel` is the `streamMeta.ResolvedModel` field already confirmed in the existing code at line 714.

**Add** `CostCalculator()` method to `Provider`:
```go
func (*Provider) CostCalculator() usage.CostCalculator {
	return usage.Default()
}
```

In `CreateStream`, after the HTTP request is built but before it is sent, emit a `TokenEstimateEvent` (same pattern as Anthropic Task 11 step 5):
```go
if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
	Model: opts.Model, Messages: opts.Messages, Tools: opts.Tools,
}); err == nil {
	tokens := usage.TokenItems{{Kind: usage.KindInput, Count: est.InputTokens}}
	rec := usage.Record{
		IsEstimate: true, RecordedAt: time.Now(),
		Dims:    usage.Dims{Provider: llm.ProviderNameBedrock, Model: opts.Model},
		Tokens:  tokens,
	}
	if cost, ok := usage.Default().Calculate(llm.ProviderNameBedrock, opts.Model, tokens); ok {
		rec.Cost = cost; rec.Cost.Source = "estimated"
	}
	pub.TokenEstimate(rec)
}
```

**Verification**:
```bash
go build ./provider/bedrock/...
```

---

### Task 13: Migrate `provider/openai/`

**Files modified**: `provider/openai/models.go`, `provider/openai/api_completions.go`, `provider/openai/api_responses.go`, `provider/openai/openai.go`, `provider/openai/openai_test.go`  
**Estimated time**: 20 minutes

**Step 1**: Edit `provider/openai/models.go`:

**Delete** the `calculateCost` function and any pricing struct/map.

**Step 2**: Edit `provider/openai/api_completions.go`:

Add import:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
)
```

Find `var finalUsage *llm.Usage` (around line 287). Replace the usage accumulation and emission with:

```go
var inputTokens, outputTokens, cachedTokens, reasoningTokens int
// (these are declared alongside the existing activeTools / stopReason vars)
```

In the chunk-parsing block (where `chunk.Usage != nil` is checked, around lines 321-333), **replace** the `finalUsage = &llm.Usage{...}` block:
```go
// REMOVE:
finalUsage = &llm.Usage{
    InputTokens:  chunk.Usage.PromptTokens,
    OutputTokens: chunk.Usage.CompletionTokens,
    ...
}

// ADD:
inputTokens  = chunk.Usage.PromptTokens
outputTokens = chunk.Usage.CompletionTokens
if chunk.Usage.PromptTokensDetails != nil {
    cachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
}
if chunk.Usage.CompletionTokensDetails != nil {
    reasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
}
```

In the `[DONE]` handling block, **replace** the `calculateCost` + `pub.Usage` calls with:
```go
// REMOVE:
if finalUsage != nil {
    calculateCost(meta.requestedModel, finalUsage)
}
if finalUsage != nil {
    pub.Usage(*finalUsage)
}

// ADD:
regularInput := inputTokens - cachedTokens

// Reasoning separation: use data from API, not model-name detection.
// If the API reported reasoning tokens, KindOutput = completion - reasoning (no overlap).
var outputItem, reasoningItem usage.TokenItem
if reasoningTokens > 0 {
    outputItem    = usage.TokenItem{Kind: usage.KindOutput,    Count: outputTokens - reasoningTokens}
    reasoningItem = usage.TokenItem{Kind: usage.KindReasoning, Count: reasoningTokens}
} else {
    outputItem = usage.TokenItem{Kind: usage.KindOutput, Count: outputTokens}
}

items := usage.TokenItems{
    {Kind: usage.KindInput, Count: regularInput},
    outputItem,
}.NonZero()
if cachedTokens > 0 {
    items = append(items, usage.TokenItem{Kind: usage.KindCacheRead, Count: cachedTokens})
}
if reasoningTokens > 0 {
    items = append(items, reasoningItem)
}

rec := usage.Record{
    Dims:       usage.Dims{Provider: llm.ProviderNameOpenAI, Model: meta.requestedModel, RequestID: meta.responseID},
    Tokens:     items,
    RecordedAt: time.Now(),
}
if cost, ok := usage.Default().Calculate(llm.ProviderNameOpenAI, meta.requestedModel, items); ok {
    rec.Cost = cost
}
pub.UsageRecord(rec)
```

**Note**: `meta.requestedModel` and `meta.responseID` are fields of `ccStreamMeta` (the struct passed to `ccParseStream`). Confirm their exact names in the struct definition around line 240.

**Step 3**: Edit `provider/openai/api_responses.go`:

Same pattern as `api_completions.go` — find `var usage *llm.Usage` around line 490, replace with `TokenItems` construction and `pub.UsageRecord(rec)`.

**Step 4**: Edit `provider/openai/openai.go`:

Add:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
    "github.com/codewandler/llm/tokencount"
)

func (*Provider) CostCalculator() usage.CostCalculator {
	return usage.Default()
}
```

In `CreateStream`, emit `TokenEstimateEvent` after request setup (same pattern as Anthropic).

**Step 5**: Edit `provider/openai/openai_test.go`:

Find all assertions like:
```go
var usage *llm.Usage
// ...
if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
	usage = &ue.Usage
}
```

Replace with:
```go
var usageRec *usage.Record
// ...
if ue, ok := event.Data.(*llm.UsageUpdatedEvent); ok {
	usageRec = &ue.Record
}
```

Update field accesses from `usage.InputTokens` to `usageRec.Tokens.Count(usage.KindInput)`, etc.

**Verification**:
```bash
go build ./provider/openai/...
go test ./provider/openai/...
```

---

### Task 14: Migrate `provider/minimax/`

**Files modified**: `provider/minimax/models.go`, `provider/minimax/minimax.go`, `provider/minimax/models_test.go`  
**Estimated time**: 8 minutes

**Important context**: MiniMax does NOT have its own stream parser. It calls `anthropic.ParseStreamWith` with `ParseOpts{CostFn: FillCost, ...}`. After Task 11 removed `CostFn` from `ParseOpts` and rewired `onMessageStop` to use `usage.Default()` with `p.meta.ProviderName`, MiniMax's token records will be emitted automatically by Anthropic's parser with `Dims.Provider = "minimax"` (because `ParseOpts.ProviderName` is set to `providerName` which is the MiniMax provider name). No separate stream-building code is needed here.

**Step 1**: Edit `provider/minimax/models.go`:

**Delete** `FillCost` function and pricing struct.

**Step 2**: Edit `provider/minimax/models_test.go`:

**Delete** all cost tests.

**Step 3**: Edit `provider/minimax/minimax.go`:

In `CreateStream`, find the `parseOpts` construction block:
```go
parseOpts := anthropic.ParseOpts{
    Model:         opts.Model,
    ProviderName:  providerName,
    CostFn:        FillCost,   // DELETE this line
    LLMRequest:    opts,
    RequestParams: llm.ProviderRequestFromHTTP(req, body),
}
```

**Remove** the `CostFn: FillCost` line. `CostFn` no longer exists on `ParseOpts` after Task 11.

Add imports:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
    "github.com/codewandler/llm/tokencount"
)
```

**Add** `CostCalculator()` method:
```go
func (*Provider) CostCalculator() usage.CostCalculator {
    return usage.Default()
}
```

In `CreateStream`, after `anthropic.PublishRequestParams(pub, parseOpts)` is called (or after `pub` is created), emit a `TokenEstimateEvent`:
```go
if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
    Model: opts.Model, Messages: opts.Messages, Tools: opts.Tools,
}); err == nil {
    tokens := usage.TokenItems{{Kind: usage.KindInput, Count: est.InputTokens}}
    rec := usage.Record{
        IsEstimate: true, RecordedAt: time.Now(),
        Dims:   usage.Dims{Provider: providerName, Model: opts.Model},
        Tokens: tokens,
    }
    if cost, ok := usage.Default().Calculate(providerName, opts.Model, tokens); ok {
        rec.Cost = cost; rec.Cost.Source = "estimated"
    }
    pub.TokenEstimate(rec)
}
```

**Verification**:
```bash
go build ./provider/minimax/...
```

---

### Task 15: Migrate `provider/openrouter/`

**Files modified**: `provider/openrouter/openrouter.go`, `provider/openrouter/openrouter_test.go`  
**Estimated time**: 10 minutes

**Confirmed variable names** (read from actual source before editing):
- `providerName` — package-level `const providerName = "openrouter"` (line 21)
- `requestedModel` — function parameter of `parseStream`
- `chunk.Model` — the resolved model (available on each chunk)
- `chunk.Usage.PromptTokens`, `chunk.Usage.CompletionTokens`, `chunk.Usage.Cost` — confirmed wire fields
- `chunk.Usage.PromptTokensDetails.CachedTokens` — confirmed wire field
- `chunk.Usage.CompletionTokensDetails.ReasoningTokens` — confirmed wire field

**Edit `provider/openrouter/openrouter.go`**:

Add imports:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
    "github.com/codewandler/llm/tokencount"
)
```

In `parseStream`, add a variable to track the resolved model (it arrives in the first chunk):
```go
// Add alongside the existing startEmitted / stopReason vars:
var resolvedModel string
```

In the `!startEmitted` block (lines ~486-497), after `pub.Started(...)` is called, store the resolved model:
```go
resolvedModel = chunk.Model
```

Find the `var usage *llm.Usage` declaration (line 458). **Remove** it and the `if chunk.Usage != nil { usage = &llm.Usage{...} }` block (lines 499-512). Replace with:
```go
// Declare alongside other vars at top of parseStream:
var usageRec *usage.Record

// In the chunk-handling loop, replace if chunk.Usage != nil { ... } with:
if chunk.Usage != nil {
    cached := 0
    if chunk.Usage.PromptTokensDetails != nil {
        cached = chunk.Usage.PromptTokensDetails.CachedTokens
    }
    tokens := usage.TokenItems{
        {Kind: usage.KindInput,  Count: chunk.Usage.PromptTokens - cached},
        {Kind: usage.KindOutput, Count: chunk.Usage.CompletionTokens},
    }
    if cached > 0 {
        tokens = append(tokens, usage.TokenItem{Kind: usage.KindCacheRead, Count: cached})
    }
    r := usage.Record{
        RecordedAt: time.Now(),
        Dims:       usage.Dims{Provider: providerName, Model: resolvedModel},
        Tokens:     tokens.NonZero(),
        Cost: usage.Cost{
            Total:  chunk.Usage.Cost,
            Source: "reported", // OpenRouter reports cost directly; do not recalculate
        },
    }
    usageRec = &r
}
```

In the `[DONE]` block, **replace** `if usage != nil { pub.Usage(*usage) }` with:
```go
if usageRec != nil {
    pub.UsageRecord(*usageRec)
}
```

In `CreateStream` (not `parseStream`), after the request is prepared but before calling `parseStream`, emit a `TokenEstimateEvent`:
```go
if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
    Model: opts.Model, Messages: opts.Messages, Tools: opts.Tools,
}); err == nil {
    tokens := usage.TokenItems{{Kind: usage.KindInput, Count: est.InputTokens}}
    pub.TokenEstimate(usage.Record{
        IsEstimate: true,
        RecordedAt: time.Now(),
        Dims:       usage.Dims{Provider: providerName, Model: opts.Model},
        Tokens:     tokens,
    })
}
```

**Edit `provider/openrouter/openrouter_test.go`**:

Update assertions from `llm.Usage` to `usage.Record`.

**Verification**:
```bash
go build ./provider/openrouter/...
```

---

### Task 16: Migrate `provider/ollama/`

**Files modified**: `provider/ollama/ollama.go`  
**Estimated time**: 8 minutes

**Edit `provider/ollama/ollama.go`**:

**Confirmed variable names** (read from actual source):
- `meta streamMeta` — function parameter with fields `RequestedModel`, `ResolvedModel`, `StartTime`
- `meta.ResolvedModel` — the model to use in `Dims`
- `chunk.PromptEvalCount` and `chunk.EvalCount` — NEW fields to add (currently absent)

Add imports:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
    "github.com/codewandler/llm/tokencount"
)
```

Find the `streamChunk` struct definition (around line 419). **Add** the two missing fields:
```go
type streamChunk struct {
	Message struct {
		// ... existing Message fields ...
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason,omitempty"`
	PromptEvalCount int    `json:"prompt_eval_count"` // NEW: input token count
	EvalCount       int    `json:"eval_count"`         // NEW: output token count
}
```

Find `var usage llm.Usage` at line 448. **Remove** it entirely.

Find `pub.Usage(usage)` at line 504 (inside `if chunk.Done {`). **Replace** just that line with:
```go
tokens := usage.TokenItems{
	{Kind: usage.KindInput,  Count: chunk.PromptEvalCount},
	{Kind: usage.KindOutput, Count: chunk.EvalCount},
}.NonZero()

pub.UsageRecord(usage.Record{
	Dims:       usage.Dims{Provider: llm.ProviderNameOllama, Model: meta.ResolvedModel},
	Tokens:     tokens,
	RecordedAt: time.Now(),
})
```

In `CreateStream`, after `pub` is created and the request is built but before the HTTP call, emit a `TokenEstimateEvent`:
```go
if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
	Model: opts.Model, Messages: opts.Messages, Tools: opts.Tools,
}); err == nil {
	tokens := usage.TokenItems{{Kind: usage.KindInput, Count: est.InputTokens}}
	pub.TokenEstimate(usage.Record{
		IsEstimate: true,
		RecordedAt: time.Now(),
		Dims:   usage.Dims{Provider: llm.ProviderNameOllama, Model: opts.Model},
		Tokens: tokens,
	})
}
```

**Verification**:
```bash
go build ./provider/ollama/...
```

---

### Task 17: Migrate `provider/fake/` and `provider/router/`

**Files modified**: `provider/fake/fake.go`, `provider/router/router.go`  
**Estimated time**: 5 minutes

**Edit `provider/fake/fake.go`**:

Add import:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
)
```

Find `pub.Usage(llm.Usage{...})` calls. Replace with:
```go
pub.UsageRecord(usage.Record{
	Tokens:     usage.TokenItems{{Kind: usage.KindInput, Count: 1}, {Kind: usage.KindOutput, Count: 1}},
	Cost:       usage.Cost{Total: 0.01, Source: "calculated"},
	RecordedAt: time.Now(),
})
```

**Edit `provider/router/router.go`**:

If there are any `llm.Usage` references, replace with `usage.Record`. Add import if needed:
```go
import (
    // ...
    "github.com/codewandler/llm/usage"
)
```

The router manages multiple providers via a `providers map[string]llm.Provider` field — there is no single "current provider". Add a `CostCalculatorProvider` implementation that returns `usage.Default()`, which already includes the full `KnownPricing` + `ModelDB` chain and works for all providers:

```go
func (*Provider) CostCalculator() usage.CostCalculator {
	return usage.Default()
}
```

If you want the router to forward to an underlying provider's calculator (for future use), it can be done lazily later. For this migration `usage.Default()` is correct and safe.

**Verification**:
```bash
go build ./provider/...
```

At this point, `go build ./...` should succeed.

---

## Phase 4: Test helpers and consumer updates

---

### Task 18: Update `llmtest/events.go`

**Files modified**: `llmtest/events.go`  
**Estimated time**: 3 minutes

Add import:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
)
```

**Replace**:
```go
func UsageEvent(u llm.Usage) llm.Event { return &llm.UsageUpdatedEvent{Usage: u} }
```
**with**:
```go
func UsageEvent(r usage.Record) llm.Event { return &llm.UsageUpdatedEvent{Record: r} }
func EstimateEvent(r usage.Record) llm.Event { return &llm.TokenEstimateEvent{Estimate: r} }
```

**Verification**:
```bash
go build ./llmtest/...
go test ./...  # all tests should pass now
```

---

### Task 19: Update `cmd/llmcli/cmds/infer.go`

**Files modified**: `cmd/llmcli/cmds/infer.go`  
**Estimated time**: 15 minutes

Add import:
```go
import (
    // ... existing imports ...
    "github.com/codewandler/llm/usage"
)
```

**Step 1**: Find the manual `CountTokens` block (around ~15 lines that cast the provider to `tokencount.TokenCounter`, call `CountTokens`, and store the result). **Delete that entire block.**

**Step 2**: Add an `OnEvent` handler for `TokenEstimateEvent`. Do **not** declare a `tokenEstimate` variable — just call `printTokenEstimate` directly from the handler. The drift is obtained from `result.Drift()` after the stream ends, not from the stored estimate:

```go
proc = proc.OnEvent(llm.TypedEventHandler[*llm.TokenEstimateEvent](func(ev *llm.TokenEstimateEvent) {
	if verbose {
		printTokenEstimate(&ev.Estimate)
	}
}))
```

**Step 3**: Find `printVerboseInfo` function. Replace the manual drift calculation with:

```go
func printVerboseInfo(result llm.Result, verbose bool) {
	if !verbose {
		return
	}

	records := result.UsageRecords()
	if len(records) == 0 {
		return
	}

	actual := records[0]
	fmt.Fprintf(os.Stderr, "\nTokens: %d input, %d output (total: %d)\n",
		actual.Tokens.TotalInput(),
		actual.Tokens.TotalOutput(),
		actual.Tokens.Total(),
	)

	if !actual.Cost.IsZero() {
		fmt.Fprintf(os.Stderr, "Cost: $%.4f (%s)\n", actual.Cost.Total, actual.Cost.Source)
	}

	if drift := result.Drift(); drift != nil {
		sign := "+"
		if drift.InputDelta < 0 {
			sign = ""
		}
		fmt.Fprintf(os.Stderr, "Drift: %s%d tokens (%+.1f%%)\n",
			sign, drift.InputDelta, drift.InputPct,
		)
	}
}
```

**Step 4**: Update `printTokenEstimate` signature to receive `*usage.Record`:

```go
func printTokenEstimate(est *usage.Record) {
	if est == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "Estimated input tokens: %d\n", est.Tokens.Count(usage.KindInput))
}
```

**Verification**:
```bash
go build ./cmd/llmcli/...
go run ./cmd/llmcli infer -v "Hello"
```

Should show estimate, actual, and drift.

---

## Phase 5: Full verification

---

### Task 20: Build, vet, fmt, test, final checks

**Estimated time**: 10 minutes

Run full verification suite:

```bash
# Format
go fmt ./...

# Build all packages
go build ./...

# Vet
go vet ./...

# Test all packages
go test ./...

# Test with race detector
go test -race ./...
```

**Verify no old types remain**:
```bash
grep -r "llm\.Usage" --include="*.go" . | grep -v "UsageRecord\|UsageUpdated"
```

Must return zero results (except in comments).

**Verify acceptance criteria** (spot-check):
```bash
# AC 1-6: TokenKind constants exist
grep -r "KindInput\|KindOutput\|KindReasoning\|KindCacheRead\|KindCacheWrite" usage/record.go

# AC 10-11: No provider has pricing code
grep -r "FillCost\|CalculateCost\|fillCost\|calculateCost" provider/*/models.go
# Must return zero results

# AC 18: All providers emit estimates
grep -r "TokenEstimateEvent" provider/*/
# Must show calls in all 7 providers

# AC 24: llm.Usage deleted
ls usage.go
# Must fail (file not found)

# AC 30: Drift types exist
grep -r "type Drift\|type DriftStats" usage/drift.go
```

If all checks pass, the implementation is complete.

---

## Summary

| Phase | Tasks | Time | Verification |
|---|---|---|---|
| 1: `usage/` package | 1-7 | 60 min | `go test -race ./usage/...` |
| 2: Root interfaces | 8-10 | 30 min | `go build ./usage/...` |
| 3: Providers | 11-17 | 80 min | `go build ./...` (passes after task 17) |
| 4: Helpers + CLI | 18-19 | 20 min | `go test ./...` |
| 5: Verification | 20 | 10 min | All checks pass |
| **Total** | **20 tasks** | **~200 min** | Clean build, all tests pass |
