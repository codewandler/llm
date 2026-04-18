package modelview

import (
	"context"
	"testing"

	"github.com/codewandler/llm"
	modeldb "github.com/codewandler/modeldb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelsForService(t *testing.T) {
	c := testAnthropicCatalog(t)
	models := ModelsForService(c, "anthropic", ProjectionOptions{IncludePricing: true, ExcludeBuiltinAliases: true})
	require.NotEmpty(t, models)
	sonnet, ok := models.ByID("claude-sonnet-4-6")
	require.True(t, ok)
	assert.Equal(t, "anthropic", sonnet.Provider)
	assert.Contains(t, sonnet.Aliases, "sonnet")
	assert.NotContains(t, sonnet.Aliases, llm.ModelDefault)
	if assert.NotNil(t, sonnet.Pricing) {
		assert.Equal(t, 3.0, sonnet.Pricing.Input)
	}
}

func TestFactualAliasesForService(t *testing.T) {
	c := testAnthropicCatalog(t)
	aliases := FactualAliasesForService(c, "anthropic")
	assert.Equal(t, "claude-sonnet-4-6", aliases["sonnet"])
	assert.Equal(t, "claude-opus-4-7", aliases["opus"])
}

func TestModelsForRuntime(t *testing.T) {
	keyInstalled := modeldb.NormalizeKey(modeldb.ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	keyVisible := modeldb.NormalizeKey(modeldb.ModelKey{Creator: "anthropic", Family: "claude", Series: "opus", Version: "4.6"})
	base := modeldb.NewCatalog()
	require.NoError(t, modeldb.MergeCatalogFragment(&base, &modeldb.Fragment{Services: []modeldb.Service{{ID: "ollama", Name: "Ollama", Kind: modeldb.ServiceKindLocal}}, Models: []modeldb.ModelRecord{{Key: keyInstalled, Name: "Claude Sonnet 4.6", Aliases: []string{"sonnet"}}, {Key: keyVisible, Name: "Claude Opus 4.6", Aliases: []string{"opus"}}}, Offerings: []modeldb.Offering{{ServiceID: "ollama", WireModelID: "installed", ModelKey: keyInstalled, Aliases: []string{"fast", "sonnet"}}, {ServiceID: "ollama", WireModelID: "visible", ModelKey: keyVisible, Aliases: []string{"powerful", "opus"}}}}))
	resolved, err := modeldb.ResolveCatalog(context.Background(), base, modeldb.RegisteredSource{Stage: modeldb.StageRuntime, Authority: modeldb.AuthorityLocal, Source: modeldb.SourceFunc{SourceID: "runtime-projection-test", FetchFunc: func(context.Context) (*modeldb.Fragment, error) {
		return &modeldb.Fragment{Runtimes: []modeldb.Runtime{{ID: "ollama-local", ServiceID: "ollama", Name: "Ollama Local", Local: true}}, RuntimeAccess: []modeldb.RuntimeAccess{{RuntimeID: "ollama-local", Offering: modeldb.OfferingRef{ServiceID: "ollama", WireModelID: "installed"}, Routable: true, ResolvedWireID: "installed"}}, RuntimeAcquisition: []modeldb.RuntimeAcquisition{{RuntimeID: "ollama-local", Offering: modeldb.OfferingRef{ServiceID: "ollama", WireModelID: "installed"}, Known: true, Status: "installed"}, {RuntimeID: "ollama-local", Offering: modeldb.OfferingRef{ServiceID: "ollama", WireModelID: "visible"}, Known: true, Acquirable: true, Status: "pullable", Action: "pull"}}}, nil
	}}})
	require.NoError(t, err)
	routable := ModelsForRuntime(resolved, "ollama-local", true, ProjectionOptions{ExcludeBuiltinAliases: true})
	require.Len(t, routable, 1)
	assert.Equal(t, "installed", routable[0].ID)
	aliases := FactualAliasesForRuntime(resolved, "ollama-local")
	assert.Equal(t, "installed", aliases["sonnet"])
}

func testAnthropicCatalog(t *testing.T) modeldb.Catalog {
	t.Helper()
	sonnetKey := modeldb.NormalizeKey(modeldb.ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	opusKey := modeldb.NormalizeKey(modeldb.ModelKey{Creator: "anthropic", Family: "claude", Series: "opus", Version: "4.7"})
	c := modeldb.NewCatalog()
	require.NoError(t, modeldb.MergeCatalogFragment(&c, &modeldb.Fragment{Services: []modeldb.Service{{ID: "anthropic", Name: "Anthropic", Kind: modeldb.ServiceKindDirect}}, Models: []modeldb.ModelRecord{{Key: sonnetKey, Name: "Claude Sonnet 4.6", Aliases: []string{"claude-sonnet-4-6", "sonnet"}, Canonical: true, ReferencePricing: &modeldb.Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75}}, {Key: opusKey, Name: "Claude Opus 4.7", Aliases: []string{"claude-opus-4-7", "opus"}, Canonical: true}}, Offerings: []modeldb.Offering{{ServiceID: "anthropic", WireModelID: "claude-sonnet-4-6", ModelKey: sonnetKey}, {ServiceID: "anthropic", WireModelID: "claude-opus-4-7", ModelKey: opusKey}}}))
	return c
}
