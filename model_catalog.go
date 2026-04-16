package llm

import (
	"fmt"
	"sort"
	"sync"

	"github.com/codewandler/llm/catalog"
	"github.com/codewandler/llm/usage"
)

type CatalogModelProjectionOptions struct {
	ProviderName         string
	IncludePricing       bool
	ExcludeIntentAliases bool
}

type CatalogSnapshot = catalog.Catalog
type ResolvedCatalogSnapshot = catalog.ResolvedCatalog

var (
	builtInCatalogOnce sync.Once
	builtInCatalog     catalog.Catalog
	builtInCatalogErr  error
)

func LoadBuiltInCatalog() (catalog.Catalog, error) {
	builtInCatalogOnce.Do(func() {
		builtInCatalog, builtInCatalogErr = catalog.LoadBuiltIn()
	})
	if builtInCatalogErr != nil {
		return catalog.Catalog{}, builtInCatalogErr
	}
	return builtInCatalog, nil
}

func CatalogModelsForService(c catalog.Catalog, serviceID string, opts CatalogModelProjectionOptions) Models {
	serviceID = normalizeCatalogPart(serviceID)
	out := make(Models, 0)
	for _, offering := range c.OfferingsByService(serviceID) {
		model, ok := c.Models[offering.ModelKey]
		if !ok {
			continue
		}
		providerName := firstNonEmptyString(opts.ProviderName, serviceID)
		out = append(out, projectCatalogModel(providerName, offering, model, opts))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func CatalogModelsForRuntime(c catalog.ResolvedCatalog, runtimeID string, routableOnly bool, opts CatalogModelProjectionOptions) Models {
	runtimeID = normalizeCatalogPart(runtimeID)
	providerName := opts.ProviderName
	if providerName == "" {
		providerName = runtimeID
	}
	refs := make(map[catalog.OfferingRef]struct{})
	for key, acquisition := range c.RuntimeAcquisition {
		if key.RuntimeID != runtimeID || !acquisition.Known {
			continue
		}
		if routableOnly {
			access, ok := c.RuntimeAccess[catalog.RuntimeAccessKey{RuntimeID: runtimeID, ServiceID: acquisition.Offering.ServiceID, WireModelID: acquisition.Offering.WireModelID}]
			if !ok || !access.Routable {
				continue
			}
		}
		refs[acquisition.Offering] = struct{}{}
	}
	out := make(Models, 0, len(refs))
	for _, ref := range sortedCatalogOfferingRefs(refs) {
		offering, ok := c.Offerings[ref]
		if !ok {
			continue
		}
		model, ok := c.Models[offering.ModelKey]
		if !ok {
			continue
		}
		entry := projectCatalogModel(providerName, offering, model, opts)
		if access, ok := c.RuntimeAccess[catalog.RuntimeAccessKey{RuntimeID: runtimeID, ServiceID: ref.ServiceID, WireModelID: ref.WireModelID}]; ok && access.ResolvedWireID != "" {
			entry.ID = access.ResolvedWireID
		}
		out = append(out, entry)
	}
	return out
}

func CatalogFactualAliasesForService(c catalog.Catalog, serviceID string) map[string]string {
	serviceID = normalizeCatalogPart(serviceID)
	result := make(map[string]string)
	conflicts := make(map[string]struct{})
	for _, offering := range c.OfferingsByService(serviceID) {
		model, ok := c.Models[offering.ModelKey]
		if !ok {
			continue
		}
		for _, alias := range projectedCatalogAliases(offering, model, true) {
			if _, conflict := conflicts[alias]; conflict {
				continue
			}
			if existing, ok := result[alias]; ok && existing != offering.WireModelID {
				delete(result, alias)
				conflicts[alias] = struct{}{}
				continue
			}
			result[alias] = offering.WireModelID
		}
	}
	return result
}

func CatalogFactualAliasesForRuntime(c catalog.ResolvedCatalog, runtimeID string) map[string]string {
	runtimeID = normalizeCatalogPart(runtimeID)
	result := make(map[string]string)
	conflicts := make(map[string]struct{})
	for _, offering := range c.RoutableOfferings(runtimeID) {
		model, ok := c.Models[offering.ModelKey]
		if !ok {
			continue
		}
		for _, alias := range projectedCatalogAliases(offering, model, true) {
			if _, conflict := conflicts[alias]; conflict {
				continue
			}
			if existing, ok := result[alias]; ok && existing != offering.WireModelID {
				delete(result, alias)
				conflicts[alias] = struct{}{}
				continue
			}
			result[alias] = offering.WireModelID
		}
	}
	return result
}

func CatalogModelForOffering(c catalog.Catalog, ref catalog.OfferingRef, opts CatalogModelProjectionOptions) (Model, bool) {
	offering, ok := c.OfferingByRef(ref)
	if !ok {
		return Model{}, false
	}
	model, ok := c.ModelByKey(offering.ModelKey)
	if !ok {
		return Model{}, false
	}
	providerName := firstNonEmptyString(opts.ProviderName, offering.ServiceID)
	return projectCatalogModel(providerName, offering, model, opts), true
}

func CatalogCostCalculator(c catalog.Catalog) usage.CostCalculator {
	pricingByProvider := make(map[string]map[string]usage.Pricing)
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
		if pricingByProvider[ref.ServiceID] == nil {
			pricingByProvider[ref.ServiceID] = make(map[string]usage.Pricing)
		}
		pricingByProvider[ref.ServiceID][ref.WireModelID] = usage.Pricing{
			Input:       pricing.Input,
			Output:      pricing.Output,
			Reasoning:   pricing.Reasoning,
			CachedInput: pricing.CachedInput,
			CacheWrite:  pricing.CacheWrite,
		}
	}
	return usage.CostCalculatorFunc(func(provider, model string, tokens usage.TokenItems) (usage.Cost, bool) {
		provider = canonicalPricingProvider(provider)
		models, ok := pricingByProvider[provider]
		if !ok {
			return usage.Cost{}, false
		}
		pricing, ok := models[model]
		if !ok {
			return usage.Cost{}, false
		}
		return usage.CalcCost(tokens, pricing), true
	})
}

func BuiltInCatalogCostCalculator() usage.CostCalculator {
	c, err := LoadBuiltInCatalog()
	if err != nil {
		return usage.CostCalculatorFunc(func(string, string, usage.TokenItems) (usage.Cost, bool) {
			return usage.Cost{}, false
		})
	}
	return CatalogCostCalculator(c)
}

func projectCatalogModel(providerName string, offering catalog.Offering, model catalog.ModelRecord, opts CatalogModelProjectionOptions) Model {
	entry := Model{
		ID:       offering.WireModelID,
		Name:     firstNonEmptyString(model.Name, offering.WireModelID),
		Provider: providerName,
		Aliases:  projectedCatalogAliases(offering, model, opts.ExcludeIntentAliases),
	}
	if opts.IncludePricing {
		pricing := offering.Pricing
		if pricing == nil {
			pricing = model.ReferencePricing
		}
		if pricing != nil {
			entry.Pricing = &usage.Pricing{
				Input:       pricing.Input,
				Output:      pricing.Output,
				Reasoning:   pricing.Reasoning,
				CachedInput: pricing.CachedInput,
				CacheWrite:  pricing.CacheWrite,
			}
		}
	}
	return entry
}

func projectedCatalogAliases(offering catalog.Offering, model catalog.ModelRecord, excludeIntent bool) []string {
	aliases := mergeAliasesSlices(nil, offering.Aliases)
	aliases = mergeAliasesSlices(aliases, model.Aliases)
	if !excludeIntent {
		return aliases
	}
	out := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		if isIntentAlias(alias) {
			continue
		}
		out = append(out, alias)
	}
	return out
}

func mergeAliasesSlices(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, values := range [][]string{a, b} {
		for _, value := range values {
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func isIntentAlias(alias string) bool {
	switch alias {
	case ModelDefault, ModelFast, ModelPowerful, "codex":
		return true
	default:
		return false
	}
}

func normalizeCatalogPart(v string) string { return normalizeCatalogKeyPart(v) }

func normalizeCatalogKeyPart(v string) string {
	if v == "" {
		return ""
	}
	out := make([]byte, 0, len(v))
	prevDash := false
	for i := 0; i < len(v); i++ {
		b := v[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		switch b {
		case '_', ' ':
			b = '-'
		}
		if b == '-' {
			if prevDash {
				continue
			}
			prevDash = true
		} else {
			prevDash = false
		}
		out = append(out, b)
	}
	return string(out)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func canonicalPricingProvider(provider string) string {
	switch provider {
	case "claude":
		return "anthropic"
	default:
		return provider
	}
}

func sortedCatalogOfferingRefs(items map[catalog.OfferingRef]struct{}) []catalog.OfferingRef {
	refs := make([]catalog.OfferingRef, 0, len(items))
	for ref := range items {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ServiceID != refs[j].ServiceID {
			return refs[i].ServiceID < refs[j].ServiceID
		}
		return refs[i].WireModelID < refs[j].WireModelID
	})
	return refs
}

func MustLoadBuiltInCatalog() catalog.Catalog {
	c, err := LoadBuiltInCatalog()
	if err != nil {
		panic(fmt.Sprintf("load built-in catalog: %v", err))
	}
	return c
}
