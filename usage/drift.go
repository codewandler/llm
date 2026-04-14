package usage

import "math"

// Drift holds the delta between the unlabeled pre-request estimate and the
// provider-reported actual for a single request.
type Drift struct {
	Dims Dims // from the actual Record (provider, model, requestID, ...)

	EstimatedInput int // TotalInput() of the unlabeled estimate (Input+CacheRead+CacheWrite)
	ActualInput    int // TotalInput() of the actual record (non-cache + cache-read + cache-write)

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

	// Use TotalInput() so cache hits don't skew drift: an estimate covers all
	// tokens sent regardless of what is cached; the actual TotalInput() is the
	// same total seen from the billing perspective.
	estInput := estimate.Tokens.TotalInput()
	actInput := actual.Tokens.TotalInput()
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
