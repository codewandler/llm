package tokencount

import (
	"time"

	"github.com/codewandler/llm/usage"
)

// EstimateRecords converts a TokenCount into a primary usage.Record (unlabeled,
// containing the total input count) followed by per-segment labeled breakdown
// records.  The primary is always first so that consumers can rely on [0] being
// the summary when iterating.
//
// provider and model are stamped into every record's Dims.
// source identifies how the count was obtained ("api" or "heuristic").
// calculator is used to enrich cost; pass nil to skip cost enrichment.
func EstimateRecords(
	est *TokenCount,
	provider, model string,
	source string,
	calculator usage.CostCalculator,
) []usage.Record {
	if est == nil {
		return nil
	}

	now := time.Now()
	dims := usage.Dims{Provider: provider, Model: model}

	// Primary summary record — total input tokens, no labels.
	primaryTokens := usage.TokenItems{{Kind: usage.KindInput, Count: est.InputTokens}}
	primary := usage.Record{
		IsEstimate: true,
		Source:     source,
		Encoder:    est.Encoder,
		RecordedAt: now,
		Dims:       dims,
		Tokens:     primaryTokens,
	}
	if calculator != nil {
		if cost, ok := calculator.Calculate(provider, model, primaryTokens); ok {
			cost.Source = "estimated"
			primary.Cost = cost
		}
	}

	records := []usage.Record{primary}

	// Per-segment labeled breakdown records.
	type segment struct {
		label string
		count int
	}
	segments := []segment{
		{"system", est.SystemTokens},
		{"user", est.UserTokens},
		{"assistant", est.AssistantTokens},
		{"tool_results", est.ToolResultTokens},
		{"tools", est.ToolsTokens},
		{"overhead", est.OverheadTokens},
	}
	for _, seg := range segments {
		if seg.count <= 0 {
			continue
		}
		rec := usage.Record{
			IsEstimate: true,
			Source:     source,
			RecordedAt: now,
			Dims: usage.Dims{
				Provider: provider,
				Model:    model,
				Labels:   map[string]string{"category": seg.label},
			},
			Tokens: usage.TokenItems{{Kind: usage.KindInput, Count: seg.count}},
		}
		records = append(records, rec)
	}

	return records
}
