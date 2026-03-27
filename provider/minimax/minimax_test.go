package minimax

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestProviderNameAndModels(t *testing.T) {
	t.Parallel()

	p := New()
	assert.Equal(t, providerName, p.Name())

	models := p.Models()
	require.NotEmpty(t, models)
	assert.Equal(t, providerName, models[0].Provider)
}

func TestProviderModels_HaveCorrectIDs(t *testing.T) {
	t.Parallel()

	p := New()
	models := p.Models()

	expectedIDs := map[string]bool{
		ModelM27: true,
		ModelM25: true,
		ModelM21: true,
		ModelM2:  true,
	}

	for _, m := range models {
		assert.True(t, expectedIDs[m.ID], "unexpected model ToolCallID: %s", m.ID)
		delete(expectedIDs, m.ID)
	}
	assert.Empty(t, expectedIDs, "missing expected models")
}

func TestNewAPIRequestHeaders(t *testing.T) {
	t.Parallel()

	p := New(WithLLMOpts(llm.WithBaseURL("https://api.test"), llm.WithAPIKey("test-key")))

	body := []byte(`{"ok":true}`)
	req, err := p.newAPIRequest(context.Background(), "token-123", body)
	require.NoError(t, err)

	assert.Equal(t, "https://api.test/v1/messages", req.URL.String())
	assert.Equal(t, "token-123", req.Header.Get("x-api-key"))
	assert.Equal(t, anthropicVersion, req.Header.Get("Anthropic-Version"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, "application/json", req.Header.Get("Accept"))
}

func TestNew_DefaultOptions(t *testing.T) {
	t.Parallel()

	p := New()
	assert.Equal(t, defaultBaseURL, p.opts.BaseURL)
}

func TestWithLLMOpts(t *testing.T) {
	t.Parallel()

	p := New(WithLLMOpts(
		llm.WithBaseURL("https://custom.api.com"),
		llm.WithAPIKey("custom-key"),
	))

	assert.Equal(t, "https://custom.api.com", p.opts.BaseURL)
	assert.NotNil(t, p.opts.APIKeyFunc, "APIKeyFunc should be set")
}

func TestCreateStream_MissingAPIKey(t *testing.T) {
	t.Parallel()

	p := New(WithLLMOpts(llm.WithAPIKey("")))
	_, err := p.CreateStream(context.Background(), llm.Request{
		Model:    ModelM27,
		Messages: llm.Messages{llm.User("hello")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API key")
}

func TestCreateStream_EmptyModel(t *testing.T) {
	t.Parallel()

	p := New(WithLLMOpts(llm.WithAPIKey("test-key")))
	_, err := p.CreateStream(context.Background(), llm.Request{
		Model:    "", // empty model
		Messages: llm.Messages{llm.User("hello")},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")
}
