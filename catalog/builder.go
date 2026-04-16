package catalog

import "context"

type Builder struct {
	Sources []RegisteredSource
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
