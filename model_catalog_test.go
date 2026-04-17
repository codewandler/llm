package llm

import (
	"context"
	"path/filepath"
	"testing"

	modeldb "github.com/codewandler/modeldb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogModelsForService(t *testing.T) {
	fragment, err := modeldb.NewAnthropicAPISourceFromFile(filepath.Join("catalog", modeldb.DefaultAnthropicFixturePath())).Fetch(context.Background())
	require.NoError(t, err)
	c := modeldb.NewCatalog()
	require.NoError(t, modeldb.MergeCatalogFragment(&c, fragment))

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
	fragment, err := modeldb.NewAnthropicAPISourceFromFile(filepath.Join("catalog", modeldb.DefaultAnthropicFixturePath())).Fetch(context.Background())
	require.NoError(t, err)
	c := modeldb.NewCatalog()
	require.NoError(t, modeldb.MergeCatalogFragment(&c, fragment))

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
	keyInstalled := modeldb.NormalizeKey(modeldb.ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	keyVisible := modeldb.NormalizeKey(modeldb.ModelKey{Creator: "anthropic", Family: "claude", Series: "opus", Version: "4.6"})
	base := modeldb.NewCatalog()
	require.NoError(t, modeldb.MergeCatalogFragment(&base, &modeldb.Fragment{
		Services: []modeldb.Service{{ID: "ollama", Name: "Ollama", Kind: modeldb.ServiceKindLocal}},
		Models: []modeldb.ModelRecord{
			{Key: keyInstalled, Name: "Claude Sonnet 4.6", Aliases: []string{"sonnet"}},
			{Key: keyVisible, Name: "Claude Opus 4.6", Aliases: []string{"opus"}},
		},
		Offerings: []modeldb.Offering{
			{ServiceID: "ollama", WireModelID: "installed", ModelKey: keyInstalled, Aliases: []string{"fast", "sonnet"}},
			{ServiceID: "ollama", WireModelID: "visible", ModelKey: keyVisible, Aliases: []string{"powerful", "opus"}},
		},
	}))

	resolved, err := modeldb.ResolveCatalog(context.Background(), base, modeldb.RegisteredSource{
		Stage:     modeldb.StageRuntime,
		Authority: modeldb.AuthorityLocal,
		Source: modeldb.SourceFunc{
			SourceID: "runtime-projection-test",
			FetchFunc: func(context.Context) (*modeldb.Fragment, error) {
				return &modeldb.Fragment{
					Runtimes:      []modeldb.Runtime{{ID: "ollama-local", ServiceID: "ollama", Name: "Ollama Local", Local: true}},
					RuntimeAccess: []modeldb.RuntimeAccess{{RuntimeID: "ollama-local", Offering: modeldb.OfferingRef{ServiceID: "ollama", WireModelID: "installed"}, Routable: true, ResolvedWireID: "installed"}},
					RuntimeAcquisition: []modeldb.RuntimeAcquisition{
						{RuntimeID: "ollama-local", Offering: modeldb.OfferingRef{ServiceID: "ollama", WireModelID: "installed"}, Known: true, Status: "installed"},
						{RuntimeID: "ollama-local", Offering: modeldb.OfferingRef{ServiceID: "ollama", WireModelID: "visible"}, Known: true, Acquirable: true, Status: "pullable", Action: "pull"},
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
	resolved, err := ResolveCatalog(context.Background(), modeldb.RegisteredSource{
		Stage:     modeldb.StageRuntime,
		Authority: modeldb.AuthorityLocal,
		Source: modeldb.SourceFunc{
			SourceID: "resolve-catalog-test",
			FetchFunc: func(context.Context) (*modeldb.Fragment, error) {
				key := modeldb.NormalizeKey(modeldb.ModelKey{Creator: "openai", Family: "gpt", Version: "5.4"})
				return &modeldb.Fragment{
					Services:           []modeldb.Service{{ID: "codex", Name: "Codex", Kind: modeldb.ServiceKindPlatform}},
					Models:             []modeldb.ModelRecord{{Key: key, Name: "GPT-5.4"}},
					Offerings:          []modeldb.Offering{{ServiceID: "codex", WireModelID: "gpt-5.4", ModelKey: key}},
					Runtimes:           []modeldb.Runtime{{ID: "codex-local", ServiceID: "codex", Name: "Codex Local"}},
					RuntimeAccess:      []modeldb.RuntimeAccess{{RuntimeID: "codex-local", Offering: modeldb.OfferingRef{ServiceID: "codex", WireModelID: "gpt-5.4"}, Routable: true, ResolvedWireID: "gpt-5.4"}},
					RuntimeAcquisition: []modeldb.RuntimeAcquisition{{RuntimeID: "codex-local", Offering: modeldb.OfferingRef{ServiceID: "codex", WireModelID: "gpt-5.4"}, Known: true, Status: "available"}},
				}, nil
			},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, resolved.Services, "codex")
	assert.Contains(t, resolved.Runtimes, "codex-local")
}
