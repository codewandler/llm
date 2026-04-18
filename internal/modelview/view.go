package modelview

import (
	"context"
	"net/http"
	"sort"

	"github.com/codewandler/llm"
	modelcatalog "github.com/codewandler/llm/internal/modelcatalog"
	"github.com/codewandler/llm/usage"
	modeldb "github.com/codewandler/modeldb"
)

type ProjectionOptions struct {
	ProviderName          string
	IncludePricing        bool
	ExcludeBuiltinAliases bool
}

func ModelsForService(c modeldb.Catalog, serviceID string, opts ProjectionOptions) llm.Models {
	serviceID = normalizeCatalogPart(serviceID)
	providerName := firstNonEmptyString(opts.ProviderName, serviceID)
	return modelsFromCatalogView(modeldb.ServiceView(c, serviceID, modeldb.ViewOptions{}), providerName, opts)
}

func ModelsForRuntime(c modeldb.ResolvedCatalog, runtimeID string, routableOnly bool, opts ProjectionOptions) llm.Models {
	runtimeID = normalizeCatalogPart(runtimeID)
	providerName := opts.ProviderName
	if providerName == "" {
		providerName = runtimeID
	}
	view := modeldb.RuntimeView(c, runtimeID, modeldb.ViewOptions{RoutableOnly: routableOnly, VisibleOnly: !routableOnly})
	return modelsFromCatalogView(view, providerName, opts)
}

func VisibleModelsForRuntime(ctx context.Context, base modeldb.Catalog, runtimeID string, source modeldb.Source, opts ProjectionOptions) (llm.Models, error) {
	resolved, err := modelcatalog.ResolveWithBase(ctx, base, modeldb.RegisteredSource{
		Stage:     modeldb.StageRuntime,
		Authority: modeldb.AuthorityLocal,
		Source:    source,
	})
	if err != nil {
		return nil, err
	}
	return ModelsForRuntime(resolved, runtimeID, false, opts), nil
}

func VisibleModelsForOllamaRuntime(ctx context.Context, client *http.Client, baseURL string, opts ProjectionOptions) (llm.Models, error) {
	base, err := modelcatalog.LoadBuiltIn()
	if err != nil {
		return nil, err
	}
	return VisibleModelsForRuntime(ctx, base, "ollama-local", modelcatalog.NewOllamaRuntimeSource(client, baseURL), opts)
}

func VisibleModelsForDockerMRRuntime(ctx context.Context, client *http.Client, baseURL string, opts ProjectionOptions) (llm.Models, error) {
	base, err := modelcatalog.LoadBuiltIn()
	if err != nil {
		return nil, err
	}
	return VisibleModelsForRuntime(ctx, base, "dockermr-local", modelcatalog.NewDockerMRRuntimeSource(client, baseURL), opts)
}

func FactualAliasesForService(c modeldb.Catalog, serviceID string) map[string]string {
	serviceID = normalizeCatalogPart(serviceID)
	return aliasMapFromView(modeldb.ServiceView(c, serviceID, modeldb.ViewOptions{}))
}

func FactualAliasesForRuntime(c modeldb.ResolvedCatalog, runtimeID string) map[string]string {
	runtimeID = normalizeCatalogPart(runtimeID)
	return aliasMapFromView(modeldb.RuntimeView(c, runtimeID, modeldb.ViewOptions{RoutableOnly: true}))
}

func ModelForOffering(c modeldb.Catalog, ref modeldb.OfferingRef, opts ProjectionOptions) (llm.Model, bool) {
	offering, ok := c.OfferingByRef(ref)
	if !ok {
		return llm.Model{}, false
	}
	model, ok := c.ModelByKey(offering.ModelKey)
	if !ok {
		return llm.Model{}, false
	}
	providerName := firstNonEmptyString(opts.ProviderName, offering.ServiceID)
	return projectCatalogModel(providerName, offering, model, opts), true
}

func projectCatalogModel(providerName string, offering modeldb.Offering, model modeldb.ModelRecord, opts ProjectionOptions) llm.Model {
	entry := llm.Model{ID: offering.WireModelID, Name: firstNonEmptyString(model.Name, offering.WireModelID), Provider: providerName, Aliases: projectedCatalogAliases(offering, model, opts.ExcludeBuiltinAliases)}
	if opts.IncludePricing {
		pricing := offering.Pricing
		if pricing == nil {
			pricing = model.ReferencePricing
		}
		if pricing != nil {
			entry.Pricing = &usage.Pricing{Input: pricing.Input, Output: pricing.Output, Reasoning: pricing.Reasoning, CachedInput: pricing.CachedInput, CacheWrite: pricing.CacheWrite}
		}
	}
	return entry
}

func projectedCatalogAliases(offering modeldb.Offering, model modeldb.ModelRecord, excludeBuiltin bool) []string {
	aliases := mergeAliasesSlices(nil, offering.Aliases)
	aliases = mergeAliasesSlices(aliases, model.Aliases)
	if !excludeBuiltin {
		return aliases
	}
	out := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		if isBuiltinAlias(alias) {
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

func isBuiltinAlias(alias string) bool {
	switch alias {
	case llm.ModelDefault, llm.ModelFast, llm.ModelPowerful:
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
func modelsFromCatalogView(view modeldb.View, providerName string, opts ProjectionOptions) llm.Models {
	out := make(llm.Models, 0)
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
func aliasMapFromView(view modeldb.View) map[string]string {
	result := make(map[string]string)
	conflicts := make(map[string]struct{})
	for _, alias := range view.AliasNames() {
		if isBuiltinAlias(alias) {
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
