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
	default:
		return provider
	}
}

// BasisProvider returns the creator/basis service used when a provider exposes
// another service's models without owning the canonical offering metadata.
// For example, the codex provider offers OpenAI-created models but remains a
// distinct service/provider for routing and capability overlay purposes.
func BasisProvider(provider string) string {
	switch provider {
	case "codex":
		return "openai"
	default:
		return CanonicalProvider(provider)
	}
}

// LookupServices returns catalog service IDs to consult for a configured
// provider/service. The exact service is checked first; if a basis service is
// different, it is appended as a fallback instead of collapsing them.
func LookupServices(provider string) []string {
	exact := CanonicalProvider(provider)
	basis := BasisProvider(provider)
	if basis == "" || basis == exact {
		return []string{exact}
	}
	return []string{exact, basis}
}

func ResolveWireModelIdentity(provider, model string) (WireModelIdentity, bool) {
	cat, err := LoadBuiltIn()
	if err != nil {
		return WireModelIdentity{}, false
	}
	return ResolveWireModelIdentityFromCatalog(cat, provider, model)
}

func ResolveWireModelIdentityFromCatalog(cat modeldb.Catalog, provider, model string) (WireModelIdentity, bool) {
	for _, serviceID := range LookupServices(provider) {
		rec, ok := cat.ResolveWireModel(serviceID, model)
		if !ok {
			continue
		}
		return WireModelIdentity{
			Creator: rec.Key.Creator,
			Family:  rec.Key.Family,
			Series:  rec.Key.Series,
			Version: rec.Key.Version,
			Variant: rec.Key.Variant,
		}, true
	}
	return WireModelIdentity{}, false
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
