package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaRuntimeSourceFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tags", r.URL.Path)
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen2.5:0.5b"},{"name":"custom-model"}]}`))
	}))
	defer server.Close()

	source := NewOllamaRuntimeSource()
	source.BaseURL = server.URL
	source.Client = server.Client()
	source.KnownModels = []KnownRuntimeModel{{ID: "qwen2.5:0.5b", Name: "Qwen 2.5 0.5B"}, {ID: "llama3.2", Name: "Llama 3.2"}}

	base := NewCatalog()
	resolved, err := ResolveCatalog(context.Background(), base, RegisteredSource{Stage: StageRuntime, Authority: AuthorityLocal, Source: source})
	require.NoError(t, err)

	routable := resolved.RoutableOfferings("ollama-local")
	assert.Len(t, routable, 2)
	visible := resolved.VisibleButNotRoutableOfferings("ollama-local")
	assert.Len(t, visible, 1)
	acquirable := resolved.AcquirableOfferings("ollama-local")
	assert.Len(t, acquirable, 1)
	assert.Equal(t, "llama3.2", acquirable[0].WireModelID)

	customRef := OfferingRef{ServiceID: "ollama", WireModelID: "custom-model"}
	customOffering, ok := resolved.Offerings[customRef]
	require.True(t, ok)
	customModel, ok := resolved.Models[customOffering.ModelKey]
	require.True(t, ok)
	assert.False(t, customModel.Canonical)
	assert.Equal(t, "local", customModel.Key.Creator)
}
