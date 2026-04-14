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
