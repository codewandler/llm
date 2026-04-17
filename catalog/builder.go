package catalog

import "context"

type Builder struct {
	Sources []RegisteredSource
}

type fetchedBuildSource struct {
	registered RegisteredSource
	fragment   *Fragment
}

func DefaultBuildSources() []RegisteredSource {
	sources := []RegisteredSource{
		{Stage: StageBuild, Authority: AuthorityCanonical, Source: NewAnthropicStaticSource()},
		{Stage: StageBuild, Authority: AuthorityCanonical, Source: NewMiniMaxStaticSource()},
		{Stage: StageBuild, Authority: AuthorityEnrichment, Source: NewModelsDevSource()},
	}
	if src := NewOpenAISourceFromEnv(); src.APIKey != "" {
		sources = append(sources, RegisteredSource{Stage: StageBuild, Authority: AuthorityTrusted, Source: src})
	}
	if src := NewOpenRouterSourceFromEnv(); src.APIKey != "" {
		sources = append(sources, RegisteredSource{Stage: StageBuild, Authority: AuthorityTrusted, Source: src})
	}
	return sources
}

func (b Builder) Build(ctx context.Context) (Catalog, error) {
	catalog := NewCatalog()
	fetched := make([]fetchedBuildSource, 0, len(b.Sources))
	for _, registered := range b.Sources {
		if registered.Stage == StageRuntime {
			continue
		}
		fragment, err := registered.Source.Fetch(ctx)
		if err != nil {
			return Catalog{}, err
		}
		fetched = append(fetched, fetchedBuildSource{registered: registered, fragment: fragment})
	}

	creatorRoots := make(map[ModelKey]struct{})
	for _, item := range fetched {
		if sourceMergeRole(item.registered) != mergeRoleCreatorRoot {
			continue
		}
		if err := MergeCatalogFragment(&catalog, item.fragment); err != nil {
			return Catalog{}, err
		}
		recordCreatorRoots(creatorRoots, item.fragment)
	}

	creatorIndex := newCreatorRootIndex(creatorRoots)
	for _, item := range fetched {
		if sourceMergeRole(item.registered) != mergeRoleOfferingEnriching {
			continue
		}
		rebindFragmentToCreatorRoots(item.fragment, creatorIndex)
		if err := MergeCatalogFragment(&catalog, item.fragment); err != nil {
			return Catalog{}, err
		}
	}
	if err := ValidateCatalog(catalog); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func recordCreatorRoots(dst map[ModelKey]struct{}, frag *Fragment) {
	if frag == nil {
		return
	}
	for _, model := range frag.Models {
		dst[NormalizeKey(model.Key)] = struct{}{}
	}
}
