package llm

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/codewandler/llm/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogModelsForService(t *testing.T) {
	fragment, err := catalog.NewAnthropicAPISourceFromFile(filepath.Join("catalog", catalog.DefaultAnthropicFixturePath())).Fetch(context.Background())
	require.NoError(t, err)
	c := catalog.NewCatalog()
	require.NoError(t, catalog.MergeCatalogFragment(&c, fragment))

	models := CatalogModelsForService(c, "anthropic", CatalogModelProjectionOptions{IncludePricing: true, ExcludeBuiltinAliases: true})
	require.NotEmpty(t, models)

	sonnet, ok := models.ByID("claude-sonnet-4-6")
	require.True(t, ok)
	assert.Equal(t, "anthropic", sonnet.Provider)
	assert.Contains(t, sonnet.Aliases, "sonnet")
	assert.NotContains(t, sonnet.Aliases, ModelDefault)
	assert.NotContains(t, sonnet.Aliases, ModelFast)
	if assert.NotNil(t, sonnet.Pricing) {
		assert.Equal(t, 3.0, sonnet.Pricing.Input)
	}
}

func TestCatalogFactualAliasesForService(t *testing.T) {
	fragment, err := catalog.NewAnthropicAPISourceFromFile(filepath.Join("catalog", catalog.DefaultAnthropicFixturePath())).Fetch(context.Background())
	require.NoError(t, err)
	c := catalog.NewCatalog()
	require.NoError(t, catalog.MergeCatalogFragment(&c, fragment))

	aliases := CatalogFactualAliasesForService(c, "anthropic")
	assert.Equal(t, "claude-sonnet-4-6", aliases["sonnet"])
	assert.Equal(t, "claude-opus-4-7", aliases["opus"])
	_, hasDefault := aliases[ModelDefault]
	_, hasFast := aliases[ModelFast]
	_, hasPowerful := aliases[ModelPowerful]
	assert.False(t, hasDefault)
	assert.False(t, hasFast)
	assert.False(t, hasPowerful)
}

func TestCatalogModelsForRuntime(t *testing.T) {
	keyInstalled := catalog.NormalizeKey(catalog.ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	keyVisible := catalog.NormalizeKey(catalog.ModelKey{Creator: "anthropic", Family: "claude", Series: "opus", Version: "4.6"})
	base := catalog.NewCatalog()
	require.NoError(t, catalog.MergeCatalogFragment(&base, &catalog.Fragment{
		Services: []catalog.Service{{ID: "ollama", Name: "Ollama", Kind: catalog.ServiceKindLocal}},
		Models: []catalog.ModelRecord{
			{Key: keyInstalled, Name: "Claude Sonnet 4.6", Aliases: []string{"sonnet"}},
			{Key: keyVisible, Name: "Claude Opus 4.6", Aliases: []string{"opus"}},
		},
		Offerings: []catalog.Offering{
			{ServiceID: "ollama", WireModelID: "installed", ModelKey: keyInstalled, Aliases: []string{"fast", "sonnet"}},
			{ServiceID: "ollama", WireModelID: "visible", ModelKey: keyVisible, Aliases: []string{"powerful", "opus"}},
		},
	}))

	resolved, err := catalog.ResolveCatalog(context.Background(), base, catalog.RegisteredSource{
		Stage:     catalog.StageRuntime,
		Authority: catalog.AuthorityLocal,
		Source: catalog.SourceFunc{
			SourceID: "runtime-projection-test",
			FetchFunc: func(context.Context) (*catalog.Fragment, error) {
				return &catalog.Fragment{
					Runtimes:      []catalog.Runtime{{ID: "ollama-local", ServiceID: "ollama", Name: "Ollama Local", Local: true}},
					RuntimeAccess: []catalog.RuntimeAccess{{RuntimeID: "ollama-local", Offering: catalog.OfferingRef{ServiceID: "ollama", WireModelID: "installed"}, Routable: true, ResolvedWireID: "installed"}},
					RuntimeAcquisition: []catalog.RuntimeAcquisition{
						{RuntimeID: "ollama-local", Offering: catalog.OfferingRef{ServiceID: "ollama", WireModelID: "installed"}, Known: true, Status: "installed"},
						{RuntimeID: "ollama-local", Offering: catalog.OfferingRef{ServiceID: "ollama", WireModelID: "visible"}, Known: true, Acquirable: true, Status: "pullable", Action: "pull"},
					},
				}, nil
			},
		},
	})
	require.NoError(t, err)

	routable := CatalogModelsForRuntime(resolved, "ollama-local", true, CatalogModelProjectionOptions{ExcludeBuiltinAliases: true})
	require.Len(t, routable, 1)
	assert.Equal(t, "installed", routable[0].ID)
	assert.Contains(t, routable[0].Aliases, "sonnet")
	assert.NotContains(t, routable[0].Aliases, ModelFast)

	visible := CatalogModelsForRuntime(resolved, "ollama-local", false, CatalogModelProjectionOptions{ExcludeBuiltinAliases: true})
	require.Len(t, visible, 2)

	aliases := CatalogFactualAliasesForRuntime(resolved, "ollama-local")
	assert.Equal(t, "installed", aliases["sonnet"])
	_, hasOpus := aliases["opus"]
	assert.False(t, hasOpus)
}

func TestResolveCatalog_WithRuntimeSource(t *testing.T) {
	resolved, err := ResolveCatalog(context.Background(), catalog.RegisteredSource{
		Stage:     catalog.StageRuntime,
		Authority: catalog.AuthorityLocal,
		Source: catalog.SourceFunc{
			SourceID: "resolve-catalog-test",
			FetchFunc: func(context.Context) (*catalog.Fragment, error) {
				key := catalog.NormalizeKey(catalog.ModelKey{Creator: "openai", Family: "gpt", Version: "5.4"})
				return &catalog.Fragment{
					Services:           []catalog.Service{{ID: "codex", Name: "Codex", Kind: catalog.ServiceKindPlatform}},
					Models:             []catalog.ModelRecord{{Key: key, Name: "GPT-5.4"}},
					Offerings:          []catalog.Offering{{ServiceID: "codex", WireModelID: "gpt-5.4", ModelKey: key}},
					Runtimes:           []catalog.Runtime{{ID: "codex-local", ServiceID: "codex", Name: "Codex Local"}},
					RuntimeAccess:      []catalog.RuntimeAccess{{RuntimeID: "codex-local", Offering: catalog.OfferingRef{ServiceID: "codex", WireModelID: "gpt-5.4"}, Routable: true, ResolvedWireID: "gpt-5.4"}},
					RuntimeAcquisition: []catalog.RuntimeAcquisition{{RuntimeID: "codex-local", Offering: catalog.OfferingRef{ServiceID: "codex", WireModelID: "gpt-5.4"}, Known: true, Status: "available"}},
				}, nil
			},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, resolved.Services, "codex")
	assert.Contains(t, resolved.Runtimes, "codex-local")
}
