package llm

import (
	"context"
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
	providerName := firstNonEmptyString(opts.ProviderName, serviceID)
	return modelsFromCatalogView(catalog.ServiceView(c, serviceID, catalog.ViewOptions{}), providerName, opts)
}

func CatalogModelsForRuntime(c catalog.ResolvedCatalog, runtimeID string, routableOnly bool, opts CatalogModelProjectionOptions) Models {
	runtimeID = normalizeCatalogPart(runtimeID)
	providerName := opts.ProviderName
	if providerName == "" {
		providerName = runtimeID
	}
	view := catalog.RuntimeView(c, runtimeID, catalog.ViewOptions{RoutableOnly: routableOnly, VisibleOnly: !routableOnly})
	return modelsFromCatalogView(view, providerName, opts)
}

func CatalogVisibleModelsForRuntime(ctx context.Context, base catalog.Catalog, runtimeID string, source catalog.Source, opts CatalogModelProjectionOptions) (Models, error) {
	resolved, err := catalog.ResolveCatalog(ctx, base, catalog.RegisteredSource{
		Stage:     catalog.StageRuntime,
		Authority: catalog.AuthorityLocal,
		Source:    source,
	})
	if err != nil {
		return nil, err
	}
	return CatalogModelsForRuntime(resolved, runtimeID, false, opts), nil
}

func CatalogFactualAliasesForService(c catalog.Catalog, serviceID string) map[string]string {
	serviceID = normalizeCatalogPart(serviceID)
	return aliasMapFromView(catalog.ServiceView(c, serviceID, catalog.ViewOptions{}))
}

func CatalogFactualAliasesForRuntime(c catalog.ResolvedCatalog, runtimeID string) map[string]string {
	runtimeID = normalizeCatalogPart(runtimeID)
	return aliasMapFromView(catalog.RuntimeView(c, runtimeID, catalog.ViewOptions{RoutableOnly: true}))
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

func modelsFromCatalogView(view catalog.View, providerName string, opts CatalogModelProjectionOptions) Models {
	out := make(Models, 0)
	for _, item := range view.List() {
		entry := projectCatalogModel(providerName, item.Offering, item.Model, opts)
		if item.Access != nil && item.Access.ResolvedWireID != "" {
			entry.ID = item.Access.ResolvedWireID
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func aliasMapFromView(view catalog.View) map[string]string {
	result := make(map[string]string)
	conflicts := make(map[string]struct{})
	for _, alias := range view.AliasNames() {
		if isIntentAlias(alias) {
			continue
		}
		items := view.ResolveAll(alias)
		if len(items) != 1 {
			conflicts[alias] = struct{}{}
			delete(result, alias)
			continue
		}
		if _, conflict := conflicts[alias]; conflict {
			continue
		}
		result[alias] = items[0].Offering.WireModelID
	}
	return result
}

func MustLoadBuiltInCatalog() catalog.Catalog {
	c, err := LoadBuiltInCatalog()
	if err != nil {
		panic(fmt.Sprintf("load built-in catalog: %v", err))
	}
	return c
}
