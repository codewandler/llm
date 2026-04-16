package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAISourceFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.4-mini","owned_by":"openai"},{"id":"o3","owned_by":"openai"}]}`))
	}))
	defer server.Close()

	source := NewOpenAISource("test-key")
	source.BaseURL = server.URL
	source.Client = server.Client()

	fragment, err := source.Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, fragment.Services, 1)
	require.Len(t, fragment.Offerings, 2)

	c := NewCatalog()
	require.NoError(t, MergeCatalogFragment(&c, fragment))
	require.NoError(t, ValidateCatalog(c))

	key := NormalizeKey(ModelKey{Creator: "openai", Family: "gpt", Version: "5.4", Variant: "mini"})
	model, ok := c.Models[key]
	require.True(t, ok)
	assert.False(t, model.Canonical)
	_, ok = c.Offerings[OfferingRef{ServiceID: "openai", WireModelID: "gpt-5.4-mini"}]
	assert.True(t, ok)
}
