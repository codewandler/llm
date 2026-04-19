package modelcatalog

import (
	"context"
	"sort"

	modeldb "github.com/codewandler/modeldb"
)

func LoadMergedBuiltIn() (modeldb.Catalog, error) {
	base, err := LoadBuiltIn()
	if err != nil {
		return modeldb.Catalog{}, err
	}
	for _, frag := range []*modeldb.Fragment{codexOverlayFragment(base), openrouterOverlayFragment(base)} {
		if err := modeldb.MergeCatalogFragment(&base, frag); err != nil {
			return modeldb.Catalog{}, err
		}
	}
	return base, nil
}

func ResolveMerged(ctx context.Context, sources ...modeldb.RegisteredSource) (modeldb.ResolvedCatalog, error) {
	base, err := LoadMergedBuiltIn()
	if err != nil {
		return modeldb.ResolvedCatalog{}, err
	}
	return ResolveWithBase(ctx, base, sources...)
}

func codexOverlayFragment(base modeldb.Catalog) *modeldb.Fragment {
	type spec struct {
		id              string
		reasoning       bool
		reasoningEffort bool
	}
	ids := []spec{
		{"gpt-5.4", true, true}, {"gpt-5.4-mini", true, true}, {"gpt-5.4-nano", true, true}, {"gpt-5.4-pro", true, true},
		{"gpt-5", true, true}, {"gpt-5-mini", true, true}, {"gpt-5-nano", true, true}, {"gpt-5-pro", true, true},
		{"gpt-5-codex", true, true}, {"gpt-5.3-codex", true, true}, {"gpt-5.3-codex-spark", true, true},
		{"gpt-5.2", true, true}, {"gpt-5.2-pro", true, true}, {"gpt-5.2-codex", true, true},
		{"gpt-5.1", true, true}, {"gpt-5.1-codex", true, true}, {"gpt-5.1-codex-max", true, true}, {"gpt-5.1-codex-mini", true, true},
		{"codex-auto-review", true, true},
	}
	frag := &modeldb.Fragment{
		Services: []modeldb.Service{{
			ID:       "codex",
			Name:     "Codex",
			Kind:     modeldb.ServiceKindDirect,
			Operator: "openai",
			DocsURL:  "https://chatgpt.com/codex",
		}},
		Offerings: make([]modeldb.Offering, 0, len(ids)),
	}
	for _, spec := range ids {
		identity, ok := ResolveWireModelIdentityFromCatalog(base, "openai", spec.id)
		if !ok {
			continue
		}
		offering := modeldb.Offering{
			ServiceID:           "codex",
			WireModelID:         spec.id,
			ModelKey:            modeldb.NormalizeKey(modeldb.ModelKey{Creator: identity.Creator, Family: identity.Family, Series: identity.Series, Version: identity.Version, Variant: identity.Variant}),
			SupportedParameters: []string{},
			APITypes:            []string{"openai-responses"},
		}
		if spec.reasoning {
			offering.SupportedParameters = append(offering.SupportedParameters, "reasoning")
		}
		if spec.reasoningEffort {
			offering.SupportedParameters = append(offering.SupportedParameters, "reasoning_effort", "reasoning_summary")
		}
		frag.Offerings = append(frag.Offerings, offering)
	}
	sort.Slice(frag.Offerings, func(i, j int) bool { return frag.Offerings[i].WireModelID < frag.Offerings[j].WireModelID })
	return frag
}

func openrouterOverlayFragment(base modeldb.Catalog) *modeldb.Fragment {
	type spec struct {
		id              string
		reasoning       bool
		reasoningEffort bool
	}
	ids := []spec{
		{"openai/gpt-5.4", true, true}, {"openai/gpt-5.4-mini", true, true},
		{"openai/gpt-5", true, true}, {"openai/gpt-5-codex", true, true}, {"openai/gpt-5.3-codex", true, true},
		{"openai/gpt-5.2", true, true}, {"openai/gpt-5.2-codex", true, true},
		{"openai/gpt-5.1", true, true}, {"openai/gpt-5.1-codex", true, true}, {"openai/gpt-5.1-codex-max", true, true}, {"openai/gpt-5.1-codex-mini", true, true},
		{"openai/o3-mini", true, true},
	}
	frag := &modeldb.Fragment{Offerings: make([]modeldb.Offering, 0, len(ids))}
	for _, spec := range ids {
		identity, ok := ResolveWireModelIdentityFromCatalog(base, "openrouter", spec.id)
		if !ok {
			continue
		}
		offering := modeldb.Offering{
			ServiceID:   "openrouter",
			WireModelID: spec.id,
			ModelKey:    modeldb.NormalizeKey(modeldb.ModelKey{Creator: identity.Creator, Family: identity.Family, Series: identity.Series, Version: identity.Version, Variant: identity.Variant}),
			APITypes:    []string{"openai-responses"},
		}
		if spec.reasoning {
			offering.SupportedParameters = append(offering.SupportedParameters, "reasoning", "include_reasoning")
		}
		if spec.reasoningEffort {
			offering.SupportedParameters = append(offering.SupportedParameters, "reasoning_effort")
		}
		frag.Offerings = append(frag.Offerings, offering)
	}
	sort.Slice(frag.Offerings, func(i, j int) bool { return frag.Offerings[i].WireModelID < frag.Offerings[j].WireModelID })
	return frag
}
