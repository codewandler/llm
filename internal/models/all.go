package models

import (
	"context"
	"sync"

	modeldb "github.com/codewandler/modeldb"
)

var (
	builtInOnce sync.Once
	builtIn     modeldb.Catalog
	builtInErr  error
)

func LoadBuiltIn() (modeldb.Catalog, error) {
	builtInOnce.Do(func() {
		builtIn, builtInErr = modeldb.LoadBuiltIn()
	})
	if builtInErr != nil {
		return modeldb.Catalog{}, builtInErr
	}
	return builtIn, nil
}

func MustLoadBuiltIn() modeldb.Catalog {
	c, err := LoadBuiltIn()
	if err != nil {
		panic(err)
	}
	return c
}

func Resolve(ctx context.Context, sources ...modeldb.RegisteredSource) (modeldb.ResolvedCatalog, error) {
	base, err := LoadBuiltIn()
	if err != nil {
		return modeldb.ResolvedCatalog{}, err
	}
	return ResolveWithBase(ctx, base, sources...)
}

func ResolveWithBase(ctx context.Context, base modeldb.Catalog, sources ...modeldb.RegisteredSource) (modeldb.ResolvedCatalog, error) {
	return modeldb.ResolveCatalog(ctx, base, sources...)
}
