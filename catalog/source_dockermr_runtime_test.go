package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDockerMRRuntimeSourceFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/engines/llama.cpp/v1/models", r.URL.Path)
		_, _ = w.Write([]byte(`{"data":[{"id":"ai/qwen2.5","name":"Qwen2.5 7B"},{"id":"ai/private-model","name":"Private Model"}]}`))
	}))
	defer server.Close()

	source := NewDockerMRRuntimeSource()
	source.BaseURL = server.URL + "/engines/llama.cpp"
	source.Client = server.Client()
	source.KnownModels = []KnownRuntimeModel{{ID: "ai/qwen2.5", Name: "Qwen2.5 7B"}, {ID: "ai/llama3.2", Name: "Llama 3.2"}}

	resolved, err := ResolveCatalog(context.Background(), NewCatalog(), RegisteredSource{Stage: StageRuntime, Authority: AuthorityLocal, Source: source})
	require.NoError(t, err)

	routable := resolved.RoutableOfferings("dockermr-local")
	assert.Len(t, routable, 2)
	visible := resolved.VisibleButNotRoutableOfferings("dockermr-local")
	assert.Len(t, visible, 1)
	assert.Equal(t, "ai/llama3.2", visible[0].WireModelID)

	privateRef := OfferingRef{ServiceID: "dockermr", WireModelID: "ai/private-model"}
	privateOffering, ok := resolved.Offerings[privateRef]
	require.True(t, ok)
	privateModel, ok := resolved.Models[privateOffering.ModelKey]
	require.True(t, ok)
	assert.Equal(t, "local", privateModel.Key.Creator)
	assert.False(t, privateModel.Canonical)
}
