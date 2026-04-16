package catalog

import "context"

type Resolver struct {
	Sources []RegisteredSource
}

func (r Resolver) Resolve(ctx context.Context, base Catalog) (ResolvedCatalog, error) {
	resolved := NewResolvedCatalog(base)
	for _, registered := range r.Sources {
		if registered.Stage != StageRuntime {
			continue
		}
		fragment, err := registered.Source.Fetch(ctx)
		if err != nil {
			return ResolvedCatalog{}, err
		}
		if err := MergeResolvedFragment(&resolved, fragment); err != nil {
			return ResolvedCatalog{}, err
		}
	}
	if err := ValidateResolvedCatalog(resolved); err != nil {
		return ResolvedCatalog{}, err
	}
	return resolved, nil
}

func ResolveCatalog(ctx context.Context, base Catalog, sources ...RegisteredSource) (ResolvedCatalog, error) {
	return Resolver{Sources: sources}.Resolve(ctx, base)
}
