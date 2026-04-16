package catalog

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceViewResolve(t *testing.T) {
	fragment, err := NewAnthropicStaticSource().Fetch(context.Background())
	require.NoError(t, err)
	c := NewCatalog()
	require.NoError(t, MergeCatalogFragment(&c, fragment))

	view := ServiceView(c, "anthropic", ViewOptions{})
	item, ok := view.Resolve("sonnet")
	require.True(t, ok)
	assert.Equal(t, "claude-sonnet-4-6", item.Offering.WireModelID)

	all := view.ResolveAll("sonnet")
	require.Len(t, all, 1)
	assert.Contains(t, view.AliasNames(), "sonnet")
}

func TestRuntimeViewVisibleAndPreferenceOverlay(t *testing.T) {
	resolved, err := ResolveCatalog(context.Background(), NewCatalog(), RegisteredSource{
		Stage:     StageRuntime,
		Authority: AuthorityLocal,
		Source: SourceFunc{
			SourceID: "runtime-view-test",
			FetchFunc: func(context.Context) (*Fragment, error) {
				sonnetKey := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
				opusKey := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "opus", Version: "4.6"})
				return &Fragment{
					Services: []Service{{ID: "ollama", Name: "Ollama", Kind: ServiceKindLocal}},
					Models: []ModelRecord{
						{Key: sonnetKey, Name: "Claude Sonnet 4.6", Aliases: []string{"sonnet"}},
						{Key: opusKey, Name: "Claude Opus 4.6", Aliases: []string{"opus"}},
					},
					Offerings: []Offering{
						{ServiceID: "ollama", WireModelID: "installed", ModelKey: sonnetKey, Aliases: []string{"sonnet"}},
						{ServiceID: "ollama", WireModelID: "pullable", ModelKey: opusKey, Aliases: []string{"opus"}},
					},
					Runtimes:      []Runtime{{ID: "ollama-local", ServiceID: "ollama", Name: "Ollama Local", Local: true}},
					RuntimeAccess: []RuntimeAccess{{RuntimeID: "ollama-local", Offering: OfferingRef{ServiceID: "ollama", WireModelID: "installed"}, Routable: true, ResolvedWireID: "installed"}},
					RuntimeAcquisition: []RuntimeAcquisition{
						{RuntimeID: "ollama-local", Offering: OfferingRef{ServiceID: "ollama", WireModelID: "installed"}, Known: true, Status: "installed"},
						{RuntimeID: "ollama-local", Offering: OfferingRef{ServiceID: "ollama", WireModelID: "pullable"}, Known: true, Acquirable: true, Status: "pullable", Action: "pull"},
					},
				}, nil
			},
		},
	})
	require.NoError(t, err)

	view := RuntimeView(resolved, "ollama-local", ViewOptions{
		VisibleOnly: true,
		AliasOverlay: &AliasOverlay{Bindings: []AliasBinding{{
			Name:   "favorite-sonnet",
			Target: OfferingRef{ServiceID: "ollama", WireModelID: "installed"},
		}}},
		PreferenceOverlay: &PreferenceOverlay{PreferredFamilies: []string{"opus"}},
	})

	items := view.List()
	require.Len(t, items, 2)
	assert.Equal(t, "pullable", items[0].Offering.WireModelID)
	assert.Equal(t, "installed", items[1].Offering.WireModelID)

	item, ok := view.Resolve("favorite-sonnet")
	require.True(t, ok)
	assert.Equal(t, "installed", item.Offering.WireModelID)
	assert.Len(t, view.ResolveAll("favorite-sonnet"), 1)
}
