package catalog

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCatalogAppliesRuntimeSources(t *testing.T) {
	base := NewCatalog()
	resolved, err := ResolveCatalog(context.Background(), base, RegisteredSource{
		Stage:     StageRuntime,
		Authority: AuthorityLocal,
		Source: SourceFunc{
			SourceID: "runtime-test",
			FetchFunc: func(context.Context) (*Fragment, error) {
				key := NormalizeKey(ModelKey{Creator: "local", Family: "test", Variant: "demo"})
				return &Fragment{
					Services:      []Service{{ID: "demo", Name: "Demo", Kind: ServiceKindLocal}},
					Models:        []ModelRecord{{Key: key, Name: "Demo Model", Canonical: false}},
					Offerings:     []Offering{{ServiceID: "demo", WireModelID: "demo-model", ModelKey: key}},
					Runtimes:      []Runtime{{ID: "demo-local", ServiceID: "demo", Name: "Demo Local", Local: true}},
					RuntimeAccess: []RuntimeAccess{{RuntimeID: "demo-local", Offering: OfferingRef{ServiceID: "demo", WireModelID: "demo-model"}, Routable: true, ResolvedWireID: "demo-model"}},
				}, nil
			},
		},
	})
	require.NoError(t, err)
	assert.Len(t, resolved.Runtimes, 1)
	assert.Len(t, resolved.RoutableOfferings("demo-local"), 1)
}
