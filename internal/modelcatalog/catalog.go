package modelcatalog

import (
	"context"
	"net/http"
	"sync"

	modeldb "github.com/codewandler/modeldb"
)

var (
	builtInOnce sync.Once
	builtIn     modeldb.Catalog
	builtInErr  error
)

type Snapshot = modeldb.Catalog
type ResolvedSnapshot = modeldb.ResolvedCatalog

type WireModelIdentity struct {
	Creator string
	Family  string
	Series  string
	Version string
	Variant string
}

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

func CanonicalProvider(provider string) string {
	switch provider {
	case "claude":
		return "anthropic"
	case "codex":
		return "openai"
	default:
		return provider
	}
}

func ResolveWireModelIdentity(provider, model string) (WireModelIdentity, bool) {
	cat, err := LoadBuiltIn()
	if err != nil {
		return WireModelIdentity{}, false
	}
	return ResolveWireModelIdentityFromCatalog(cat, provider, model)
}

func ResolveWireModelIdentityFromCatalog(cat modeldb.Catalog, provider, model string) (WireModelIdentity, bool) {
	rec, ok := cat.ResolveWireModel(CanonicalProvider(provider), model)
	if !ok {
		return WireModelIdentity{}, false
	}
	return WireModelIdentity{
		Creator: rec.Key.Creator,
		Family:  rec.Key.Family,
		Series:  rec.Key.Series,
		Version: rec.Key.Version,
		Variant: rec.Key.Variant,
	}, true
}

func NewOllamaRuntimeSource(client *http.Client, baseURL string) modeldb.Source {
	source := modeldb.NewOllamaRuntimeSource()
	source.BaseURL = baseURL
	source.Client = client
	return source
}

func NewDockerMRRuntimeSource(client *http.Client, baseURL string) modeldb.Source {
	source := modeldb.NewDockerMRRuntimeSource()
	source.BaseURL = baseURL
	source.Client = client
	return source
}
