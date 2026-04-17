package usage

import (
	"strings"
	"sync"

	modeldb "github.com/codewandler/modeldb"
)

var providerAliases = map[string]string{
	"claude": "anthropic",
	"codex":  "openai",
}

func canonicalProvider(provider string) string {
	if p, ok := providerAliases[provider]; ok {
		return p
	}
	return provider
}

type pricingByModelKey struct {
	Creator string
	Family  string
	Series  string
	Version string
	Variant string
}

func (k pricingByModelKey) String() string {
	parts := make([]string, 0, 5)
	for _, v := range []string{k.Creator, k.Family, k.Series, k.Version, k.Variant} {
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, "/")
}

func (k pricingByModelKey) isZero() bool {
	return k.Creator == "" && k.Family == "" && k.Series == "" && k.Version == "" && k.Variant == ""
}

type catalogCalc struct {
	byServiceModel map[string]map[string]Pricing
	byModelKey     map[pricingByModelKey]Pricing
}

var (
	defaultCalc     CostCalculator
	defaultCalcOnce sync.Once
)

func Default() CostCalculator {
	defaultCalcOnce.Do(func() {
		defaultCalc = newCatalogCalculator()
	})
	return defaultCalc
}

func newCatalogCalculator() CostCalculator {
	c, err := modeldb.LoadBuiltIn()
	if err != nil {
		return CostCalculatorFunc(func(string, string, TokenItems) (Cost, bool) {
			return Cost{}, false
		})
	}

	calc := &catalogCalc{
		byServiceModel: make(map[string]map[string]Pricing),
		byModelKey:     make(map[pricingByModelKey]Pricing),
	}

	for ref, offering := range c.Offerings {
		pricing := offering.Pricing
		if pricing == nil {
			model, ok := c.Models[offering.ModelKey]
			if !ok {
				continue
			}
			pricing = model.ReferencePricing
		}
		if pricing == nil {
			continue
		}
		if calc.byServiceModel[ref.ServiceID] == nil {
			calc.byServiceModel[ref.ServiceID] = make(map[string]Pricing)
		}
		calc.byServiceModel[ref.ServiceID][ref.WireModelID] = toUsagePricing(pricing)

		lineKey := pricingByModelKey{
			Creator: offering.ModelKey.Creator,
			Family:  offering.ModelKey.Family,
			Series:  offering.ModelKey.Series,
			Version: offering.ModelKey.Version,
			Variant: offering.ModelKey.Variant,
		}
		if !lineKey.isZero() {
			if _, exists := calc.byModelKey[lineKey]; !exists {
				calc.byModelKey[lineKey] = toUsagePricing(pricing)
			}
		}
	}

	return CostCalculatorFunc(func(provider, model string, tokens TokenItems) (Cost, bool) {
		provider = canonicalProvider(provider)

		if byModel, ok := calc.byServiceModel[provider]; ok {
			if p, ok := byModel[model]; ok {
				return CalcCost(tokens, p), true
			}
		}

		lineKey := inferPricingModelKey(provider, model)
		if lineKey != (pricingByModelKey{}) {
			if p, ok := calc.byModelKey[lineKey]; ok {
				return CalcCost(tokens, p), true
			}
		}

		return Cost{}, false
	})
}

func inferPricingModelKey(provider, model string) pricingByModelKey {
	id := strings.TrimSpace(model)

	providerPrefix := provider + "/"
	if strings.HasPrefix(id, providerPrefix) {
		id = strings.TrimPrefix(id, providerPrefix)
	}

	for _, suffix := range []string{"-v1:0", "-v1"} {
		id = strings.TrimSuffix(id, suffix)
	}

	parts := strings.Split(id, "-")
	if len(parts) < 2 {
		return pricingByModelKey{}
	}

	family := parts[0]

	if len(parts) >= 3 && len(parts[len(parts)-1]) == 8 && allDigits(parts[len(parts)-1]) {
		version := parts[len(parts)-3] + "." + parts[len(parts)-2]
		if len(parts) == 4 {
			return pricingByModelKey{
				Creator: provider,
				Family:  family,
				Version: version,
			}
		}
		series := parts[1]
		variant := strings.Join(parts[2:len(parts)-3], "-")
		return pricingByModelKey{
			Creator: provider,
			Family:  family,
			Series:  series,
			Version: version,
			Variant: variant,
		}
	}

	var series, version, variant string

	if len(parts) >= 2 && strings.Contains(parts[1], ".") && allDigits(strings.ReplaceAll(parts[1], ".", "")) {
		version = parts[1]
		if len(parts) > 2 {
			variant = strings.Join(parts[2:], "-")
		}
	} else if len(parts) >= 4 && allDigits(parts[len(parts)-1]) && allDigits(parts[len(parts)-2]) {
		series = parts[1]
		version = parts[len(parts)-2] + "." + parts[len(parts)-1]
		variant = strings.Join(parts[2:len(parts)-2], "-")
	} else if len(parts) >= 3 && allDigits(parts[len(parts)-1]) {
		if allDigits(parts[len(parts)-2]) {
			series = parts[1]
			version = parts[len(parts)-2] + "." + parts[len(parts)-1]
			variant = strings.Join(parts[2:len(parts)-2], "-")
		} else {
			version = parts[len(parts)-1]
			variant = strings.Join(parts[1:len(parts)-1], "-")
		}
	} else {
		version = parts[len(parts)-1]
		variant = strings.Join(parts[1:len(parts)-1], "-")
	}

	if version == "" {
		return pricingByModelKey{}
	}

	return pricingByModelKey{
		Creator: provider,
		Family:  family,
		Series:  series,
		Version: version,
		Variant: variant,
	}
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func toUsagePricing(p *modeldb.Pricing) Pricing {
	if p == nil {
		return Pricing{}
	}
	return Pricing{
		Input:       p.Input,
		Output:      p.Output,
		CachedInput: p.CachedInput,
		CacheWrite:  p.CacheWrite,
		Reasoning:   p.Reasoning,
	}
}

type Pricing struct {
	Input       float64 `json:"input"`
	Output      float64 `json:"output"`
	Reasoning   float64 `json:"reasoning,omitempty"`
	CachedInput float64 `json:"cached_input,omitempty"`
	CacheWrite  float64 `json:"cache_write,omitempty"`
}

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
				rate = p.Output
			}
			c.Reasoning = float64(item.Count) / 1_000_000 * rate
		}
	}
	c.Total = c.Input + c.CacheRead + c.CacheWrite + c.Output + c.Reasoning
	c.Source = "calculated"
	return c
}

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
