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
