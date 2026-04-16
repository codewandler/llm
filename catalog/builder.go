package catalog

import "context"

type Builder struct {
	Sources []RegisteredSource
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
	for _, registered := range b.Sources {
		if registered.Stage == StageRuntime {
			continue
		}
		fragment, err := registered.Source.Fetch(ctx)
		if err != nil {
			return Catalog{}, err
		}
		if err := MergeCatalogFragment(&catalog, fragment); err != nil {
			return Catalog{}, err
		}
	}
	if err := ValidateCatalog(catalog); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}
