package providercore

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestNewProvider_Validation(t *testing.T) {
	t.Parallel()

	t.Run("missing provider name", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, "providercore: WithProviderName is required", func() {
			NewProvider(NewOptions(
				WithAPIHint(llm.ApiTypeOpenAIChatCompletion),
				WithModels(llm.Models{{ID: "m"}}),
			))
		})
	})

	t.Run("missing api hint", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, "providercore: WithAPIHint or WithAPIHintResolver is required", func() {
			NewProvider(NewOptions(
				WithProviderName("test"),
				WithModels(llm.Models{{ID: "m"}}),
			))
		})
	})

	t.Run("missing models", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, "providercore: WithModels, WithModelsFunc, or WithCachedModelsFunc is required", func() {
			NewProvider(NewOptions(
				WithProviderName("test"),
				WithAPIHint(llm.ApiTypeOpenAIChatCompletion),
			))
		})
	})
}

func TestProvider_Name(t *testing.T) {
	t.Parallel()

	p := NewProvider(NewOptions(
		WithProviderName("my-provider"),
		WithAPIHint(llm.ApiTypeOpenAIChatCompletion),
		WithModels(llm.Models{{ID: "m"}}),
	))
	assert.Equal(t, "my-provider", p.Name())
}

func TestProvider_Models_Static(t *testing.T) {
	t.Parallel()

	models := llm.Models{
		{ID: "m1", Name: "Model 1", Provider: "test"},
		{ID: "m2", Name: "Model 2", Provider: "test"},
	}
	p := NewProvider(NewOptions(
		WithProviderName("test"),
		WithAPIHint(llm.ApiTypeOpenAIChatCompletion),
		WithModels(models),
	))
	assert.Equal(t, models, p.Models())
}

func TestProvider_Models_Cached(t *testing.T) {
	t.Parallel()

	callCount := 0
	p := NewProvider(NewOptions(
		WithProviderName("test"),
		WithAPIHint(llm.ApiTypeOpenAIChatCompletion),
		WithCachedModelsFunc(func(ctx context.Context) (llm.Models, error) {
			callCount++
			return llm.Models{{ID: "cached-m", Provider: "test"}}, nil
		}),
	))

	m1 := p.Models()
	m2 := p.Models()
	assert.Equal(t, llm.Models{{ID: "cached-m", Provider: "test"}}, m1)
	assert.Equal(t, m1, m2)
	assert.Equal(t, 1, callCount, "CachedModelsFunc should only be called once")
}

func TestProvider_Models_Uncached(t *testing.T) {
	t.Parallel()

	callCount := 0
	p := NewProvider(NewOptions(
		WithProviderName("test"),
		WithAPIHint(llm.ApiTypeOpenAIChatCompletion),
		WithModelsFunc(func(ctx context.Context) (llm.Models, error) {
			callCount++
			return llm.Models{{ID: "dynamic-m", Provider: "test"}}, nil
		}),
	))

	_ = p.Models()
	_ = p.Models()
	assert.Equal(t, 2, callCount, "ModelsFunc should be called each time")
}

func TestProvider_CreateStream(t *testing.T) {
	t.Parallel()

	sseBody := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}`,
		"",
		`data: {"id":"chatcmpl-1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseBody)
	}))
	defer server.Close()

	p := NewProvider(NewOptions(
		WithProviderName("test"),
		WithBaseURL(server.URL),
		WithAPIHint(llm.ApiTypeOpenAIChatCompletion),
		WithModels(llm.Models{{ID: "gpt-test"}}),
	), llm.WithBaseURL(server.URL))

	stream, err := p.CreateStream(context.Background(), llm.Request{
		Model:    "gpt-test",
		Messages: llm.Messages{llm.User("hi")},
	})
	require.NoError(t, err)

	var sawDelta bool
	var sawCompleted bool
	for ev := range stream {
		switch ev.Type {
		case llm.StreamEventDelta:
			sawDelta = true
		case llm.StreamEventCompleted:
			sawCompleted = true
		}
	}
	assert.True(t, sawDelta)
	assert.True(t, sawCompleted)
}

func TestProvider_Options(t *testing.T) {
	t.Parallel()

	p := NewProvider(NewOptions(
		WithProviderName("test"),
		WithAPIHint(llm.ApiTypeOpenAIChatCompletion),
		WithModels(llm.Models{{ID: "m"}}),
	), llm.WithAPIKey("test-key"))

	opts := p.Options()
	require.NotNil(t, opts)
	key, err := opts.ResolveAPIKey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test-key", key)
}

func TestOption_AppliesToBothTargets(t *testing.T) {
	t.Parallel()

	opt := WithProviderName("dual-test")

	var cc clientConfig
	opt.applyToClientConfig(&cc)
	assert.Equal(t, "dual-test", cc.ProviderName)

	o := NewOptions(opt)
	assert.Equal(t, "dual-test", o.providerName)
}
