package models

import (
	"context"
	"sync"

	"github.com/codewandler/llm/catalog"
)

var (
	builtInOnce sync.Once
	builtIn     catalog.Catalog
	builtInErr  error
)

func LoadBuiltIn() (catalog.Catalog, error) {
	builtInOnce.Do(func() {
		builtIn, builtInErr = catalog.LoadBuiltIn()
	})
	if builtInErr != nil {
		return catalog.Catalog{}, builtInErr
	}
	return builtIn, nil
}

func MustLoadBuiltIn() catalog.Catalog {
	c, err := LoadBuiltIn()
	if err != nil {
		panic(err)
	}
	return c
}

func Resolve(ctx context.Context, sources ...catalog.RegisteredSource) (catalog.ResolvedCatalog, error) {
	base, err := LoadBuiltIn()
	if err != nil {
		return catalog.ResolvedCatalog{}, err
	}
	return ResolveWithBase(ctx, base, sources...)
}

func ResolveWithBase(ctx context.Context, base catalog.Catalog, sources ...catalog.RegisteredSource) (catalog.ResolvedCatalog, error) {
	return catalog.ResolveCatalog(ctx, base, sources...)
}
