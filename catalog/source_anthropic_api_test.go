package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicAPISourceFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, defaultAnthropicAPIVersion, r.Header.Get("anthropic-version"))
		_, _ = w.Write([]byte(`{"data":[{"type":"model","id":"claude-sonnet-4-6","display_name":"Claude Sonnet 4.6","created_at":"2026-02-17T00:00:00Z","max_input_tokens":1000000,"max_tokens":128000,"capabilities":{"batch":{"supported":true},"citations":{"supported":true},"code_execution":{"supported":true},"context_management":{"supported":true},"effort":{"supported":true},"image_input":{"supported":true},"pdf_input":{"supported":true},"structured_outputs":{"supported":true},"thinking":{"supported":true,"types":{"enabled":{"supported":true},"adaptive":{"supported":true}}} }},{"type":"model","id":"claude-sonnet-4-5-20250929","display_name":"Claude Sonnet 4.5","created_at":"2025-09-29T00:00:00Z","max_input_tokens":1000000,"max_tokens":64000,"capabilities":{"batch":{"supported":true},"citations":{"supported":true},"code_execution":{"supported":true},"context_management":{"supported":true},"effort":{"supported":false},"image_input":{"supported":true},"pdf_input":{"supported":true},"structured_outputs":{"supported":true},"thinking":{"supported":true,"types":{"enabled":{"supported":true},"adaptive":{"supported":false}}} }}]}`))
	}))
	defer server.Close()

	source := NewAnthropicAPISource("test-key")
	source.BaseURL = server.URL
	source.Client = server.Client()

	fragment, err := source.Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, fragment.Services, 1)
	require.Len(t, fragment.Models, 2)
	require.Len(t, fragment.Offerings, 2)

	c := NewCatalog()
	require.NoError(t, MergeCatalogFragment(&c, fragment))
	require.NoError(t, ValidateCatalog(c))

	latestKey := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	latest, ok := c.Models[latestKey]
	require.True(t, ok)
	assert.True(t, latest.Canonical)
	assert.Contains(t, latest.Aliases, "claude-sonnet-4-6")
	assert.Contains(t, latest.Aliases, "sonnet")
	assert.True(t, latest.Capabilities.Reasoning)
	assert.True(t, latest.Capabilities.ReasoningEffort)
	assert.True(t, latest.Capabilities.AdaptiveThinking)
	assert.True(t, latest.Capabilities.StructuredOutput)
	assert.Equal(t, 1000000, latest.Limits.ContextWindow)
	assert.Equal(t, 128000, latest.Limits.MaxOutput)
	assert.Equal(t, "2026-02-17", latest.LastUpdated)
	if assert.NotNil(t, latest.ReferencePricing) {
		assert.Equal(t, 3.0, latest.ReferencePricing.Input)
		assert.Equal(t, 15.0, latest.ReferencePricing.Output)
	}

	releaseKey := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.5", ReleaseDate: "2025-09-29"})
	release, ok := c.Models[releaseKey]
	require.True(t, ok)
	assert.Contains(t, release.Aliases, "claude-sonnet-4-5")
	assert.NotContains(t, release.Aliases, "sonnet")
	assert.False(t, release.Capabilities.ReasoningEffort)

	offering, ok := c.Offerings[OfferingRef{ServiceID: "anthropic", WireModelID: "claude-sonnet-4-6"}]
	require.True(t, ok)
	assert.Equal(t, latestKey, offering.ModelKey)
	assert.Equal(t, []string{"anthropic-messages"}, offering.APITypes)
	assert.Empty(t, offering.Aliases)
	if assert.NotNil(t, offering.Pricing) {
		assert.Equal(t, 0.30, offering.Pricing.CachedInput)
	}
}

func TestAnthropicAPISourceFetchFromFile(t *testing.T) {
	source := NewAnthropicAPISourceFromFile(DefaultAnthropicFixturePath())
	fragment, err := source.Fetch(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, fragment.Models)
	require.NotEmpty(t, fragment.Offerings)

	c := NewCatalog()
	require.NoError(t, MergeCatalogFragment(&c, fragment))
	require.NoError(t, ValidateCatalog(c))

	sonnetKey := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	sonnet, ok := c.Models[sonnetKey]
	require.True(t, ok)
	assert.Contains(t, sonnet.Aliases, "sonnet")

	opusKey := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "opus", Version: "4.7"})
	opus, ok := c.Models[opusKey]
	require.True(t, ok)
	assert.Contains(t, opus.Aliases, "opus")
	assert.NotContains(t, opus.Aliases, "powerful")
	assert.NotContains(t, opus.Aliases, "default")
	assert.NotContains(t, opus.Aliases, "fast")
	assert.Equal(t, "2026-04-14", opus.LastUpdated)
}
