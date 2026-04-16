package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterSourceFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"anthropic/claude-sonnet-4-6","canonical_slug":"claude-sonnet-4-6","name":"Claude Sonnet 4.6","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"pricing":{"prompt":"0.000003","completion":"0.000015","input_cache_read":"0.0000003"},"top_provider":{"context_length":200000,"max_completion_tokens":32000},"supported_parameters":["tools","tool_choice","temperature","response_format"]}]}`))
	}))
	defer server.Close()

	source := NewOpenRouterSource("test-key")
	source.BaseURL = server.URL
	source.Client = server.Client()

	fragment, err := source.Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, fragment.Services, 1)
	require.Len(t, fragment.Offerings, 1)
	require.Len(t, fragment.Models, 1)

	c := NewCatalog()
	require.NoError(t, MergeCatalogFragment(&c, fragment))
	require.NoError(t, ValidateCatalog(c))

	key := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	model, ok := c.Models[key]
	require.True(t, ok)
	assert.True(t, model.Capabilities.ToolUse)
	assert.True(t, model.Capabilities.StructuredOutput)

	offering, ok := c.Offerings[OfferingRef{ServiceID: "openrouter", WireModelID: "anthropic/claude-sonnet-4-6"}]
	require.True(t, ok)
	assert.Equal(t, key, offering.ModelKey)
	assert.Equal(t, []string{"claude-sonnet-4-6"}, offering.Aliases)
	assert.NotNil(t, offering.Pricing)
	assert.Equal(t, 3.0, offering.Pricing.Input)
	assert.Equal(t, 15.0, offering.Pricing.Output)
	assert.Equal(t, 0.3, offering.Pricing.CachedInput)
}
