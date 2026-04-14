package llm

import "github.com/codewandler/llm/usage"

// CostCalculatorProvider is an optional interface providers may implement to
// expose a calculator. Useful for Tracker injection and offline estimation.
type CostCalculatorProvider interface {
	CostCalculator() usage.CostCalculator
}
