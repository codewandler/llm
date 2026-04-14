package usage

import (
	"math"
	"sort"
	"sync"
	"time"
)

// Tracker accumulates usage records, enriches costs, enforces budgets, and
// computes drift between pre-request estimates and actual usage.
type Tracker struct {
	mu         sync.Mutex
	records    []Record
	budget     Budget
	calculator CostCalculator
	sessionID  string
}

// TrackerOption configures a Tracker.
type TrackerOption func(*Tracker)

// WithBudget sets the spending/token ceiling for the Tracker.
func WithBudget(b Budget) TrackerOption {
	return func(t *Tracker) { t.budget = b }
}

// WithSessionID sets the session ID stamped on records that have no session ID.
func WithSessionID(id string) TrackerOption {
	return func(t *Tracker) { t.sessionID = id }
}

// WithCostCalculator sets the calculator used to enrich cost-less records.
func WithCostCalculator(c CostCalculator) TrackerOption {
	return func(t *Tracker) { t.calculator = c }
}

// NewTracker creates a new Tracker with the provided options.
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

	// Enrich cost if zero and a calculator is configured.
	if r.Cost.IsZero() && t.calculator != nil {
		if cost, ok := t.calculator.Calculate(r.Dims.Provider, r.Dims.Model, r.Tokens); ok {
			r.Cost = cost
		}
	}

	t.records = append(t.records, r)
}

// Records returns a copy of all stored records.
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

// Filter returns all records matching all provided filter functions.
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

// WithinBudget returns true when the aggregate does not exceed the configured budget.
func (t *Tracker) WithinBudget() bool {
	agg := t.Aggregate()
	return !t.budget.Exceeded(agg)
}

// Reset clears all stored records.
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

// --- Filter helpers ---

// ByProvider filters records matching the given provider name.
func ByProvider(name string) FilterFunc {
	return func(r Record) bool { return r.Dims.Provider == name }
}

// ByModel filters records matching the given model ID.
func ByModel(model string) FilterFunc {
	return func(r Record) bool { return r.Dims.Model == model }
}

// ByTurnID filters records matching the given turn ID.
func ByTurnID(id string) FilterFunc {
	return func(r Record) bool { return r.Dims.TurnID == id }
}

// BySessionID filters records matching the given session ID.
func BySessionID(id string) FilterFunc {
	return func(r Record) bool { return r.Dims.SessionID == id }
}

// EstimatesOnly filters to estimate records only.
func EstimatesOnly() FilterFunc {
	return func(r Record) bool { return r.IsEstimate }
}

// ExcludeEstimates filters to non-estimate records only.
func ExcludeEstimates() FilterFunc {
	return func(r Record) bool { return !r.IsEstimate }
}

// Since filters records recorded after the given time.
func Since(t time.Time) FilterFunc {
	return func(r Record) bool { return r.RecordedAt.After(t) }
}

// ByLabel filters records that have the given label key=value.
func ByLabel(key, value string) FilterFunc {
	return func(r Record) bool {
		if r.Dims.Labels == nil {
			return false
		}
		return r.Dims.Labels[key] == value
	}
}
