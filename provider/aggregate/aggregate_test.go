package aggregate

import (
	"context"
	"errors"
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a test provider that can be configured to succeed or fail.
type mockProvider struct {
	name       string
	models     []llm.Model
	returnErr  error
	streamFunc func(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error)
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Models() []llm.Model { return m.models }

func (m *mockProvider) CreateStream(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	if m.streamFunc != nil {
		return m.streamFunc(ctx, opts)
	}
	ch := make(chan llm.StreamEvent, 1)
	go func() {
		ch <- llm.StreamEvent{Type: llm.StreamEventDone}
		close(ch)
	}()
	return ch, nil
}

func mockFactory(prov *mockProvider) Factory {
	return func(opts ...llm.Option) llm.Provider {
		return prov
	}
}

func TestNew(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		prov1 := &mockProvider{name: "prov1", models: []llm.Model{{ID: "model-a", Name: "ModelA", Provider: "prov1"}}}

		cfg := Config{
			Name: "my-aggregate",
			Providers: []ProviderInstanceConfig{
				{Name: "prov1", Type: "mock"},
			},
			Aliases: map[string][]AliasTarget{
				"smart": {
					{Provider: "prov1", Model: "model-a"},
				},
			},
		}

		factories := map[string]Factory{
			"mock": mockFactory(prov1),
		}

		agg, err := New(cfg, factories)
		require.NoError(t, err)
		assert.Equal(t, "my-aggregate", agg.Name())
	})

	t.Run("default name", func(t *testing.T) {
		prov := &mockProvider{name: "prov"}
		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "prov", Type: "mock"},
			},
			Aliases: map[string][]AliasTarget{
				"test": {{Provider: "prov", Model: "model"}},
			},
		}

		factories := map[string]Factory{"mock": mockFactory(prov)}
		agg, err := New(cfg, factories)
		require.NoError(t, err)
		assert.Equal(t, "aggregate", agg.Name())
	})

	t.Run("no providers", func(t *testing.T) {
		cfg := Config{Providers: []ProviderInstanceConfig{}}
		_, err := New(cfg, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNoProviders)
	})

	t.Run("unknown provider type", func(t *testing.T) {
		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "prov", Type: "unknown"},
			},
			Aliases: map[string][]AliasTarget{
				"test": {{Provider: "prov", Model: "model"}},
			},
		}
		_, err := New(cfg, map[string]Factory{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown provider type")
	})
}

func TestResolveTarget(t *testing.T) {
	prov1 := &mockProvider{name: "prov1"}
	prov2 := &mockProvider{name: "prov2"}

	agg := &Provider{
		name: "test",
		providers: map[string]llm.Provider{
			"prov1": prov1,
			"prov2": prov2,
		},
		localAliases: map[string]map[string]string{
			"prov1": {"alias-a": "model-1"},
		},
	}

	t.Run("direct model ID", func(t *testing.T) {
		p, modelID, err := agg.resolveTarget(AliasTarget{Provider: "prov1", Model: "gpt-4"})
		require.NoError(t, err)
		assert.Equal(t, prov1, p)
		assert.Equal(t, "gpt-4", modelID)
	})

	t.Run("local alias", func(t *testing.T) {
		p, modelID, err := agg.resolveTarget(AliasTarget{Provider: "prov1", Model: "alias-a"})
		require.NoError(t, err)
		assert.Equal(t, prov1, p)
		assert.Equal(t, "model-1", modelID)
	})

	t.Run("unknown provider", func(t *testing.T) {
		_, _, err := agg.resolveTarget(AliasTarget{Provider: "unknown", Model: "model"})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrProviderNotFound)
	})
}

func TestResolveAllTargets(t *testing.T) {
	prov1 := &mockProvider{name: "prov1"}
	prov2 := &mockProvider{name: "prov2"}

	agg := &Provider{
		name: "test",
		providers: map[string]llm.Provider{
			"prov1": prov1,
			"prov2": prov2,
		},
		aliases: map[string][]AliasTarget{
			"multi": {
				{Provider: "prov1", Model: "model-a"},
				{Provider: "prov2", Model: "model-b"},
			},
		},
		localAliases: map[string]map[string]string{
			"prov1": {"model-a": "actual-model-a"},
		},
	}

	t.Run("single target", func(t *testing.T) {
		targets, err := agg.resolveAllTargets("multi")
		require.NoError(t, err)
		require.Len(t, targets, 2)
		assert.Equal(t, prov1, targets[0].provider)
		assert.Equal(t, "actual-model-a", targets[0].modelID)
		assert.Equal(t, prov2, targets[1].provider)
		assert.Equal(t, "model-b", targets[1].modelID)
	})

	t.Run("unknown alias", func(t *testing.T) {
		_, err := agg.resolveAllTargets("unknown")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnknownAlias)
	})
}

func TestCreateStream(t *testing.T) {
	t.Run("successful stream", func(t *testing.T) {
		prov := &mockProvider{
			name: "prov1",
			streamFunc: func(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
				ch := make(chan llm.StreamEvent, 1)
				go func() {
					ch <- llm.StreamEvent{Type: llm.StreamEventDelta, Delta: "hello"}
					ch <- llm.StreamEvent{Type: llm.StreamEventDone}
					close(ch)
				}()
				return ch, nil
			},
		}

		agg := &Provider{
			name:      "test",
			providers: map[string]llm.Provider{"prov1": prov},
			aliases: map[string][]AliasTarget{
				"smart": {{Provider: "prov1", Model: "gpt-4"}},
			},
			localAliases: map[string]map[string]string{},
		}

		stream, err := agg.CreateStream(context.Background(), llm.StreamOptions{
			Model:    "smart",
			Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
		})
		require.NoError(t, err)

		var events []llm.StreamEvent
		for evt := range stream {
			events = append(events, evt)
		}
		assert.Len(t, events, 2)
	})

	t.Run("failover to second provider", func(t *testing.T) {
		prov1 := &mockProvider{
			name:      "prov1",
			returnErr: errors.New("HTTP429: rate limit exceeded"),
		}
		prov2 := &mockProvider{
			name: "prov2",
			streamFunc: func(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
				ch := make(chan llm.StreamEvent, 1)
				go func() {
					ch <- llm.StreamEvent{Type: llm.StreamEventDone}
					close(ch)
				}()
				return ch, nil
			},
		}

		agg := &Provider{
			name: "test",
			providers: map[string]llm.Provider{
				"prov1": prov1,
				"prov2": prov2,
			},
			aliases: map[string][]AliasTarget{
				"smart": {
					{Provider: "prov1", Model: "gpt-4"},
					{Provider: "prov2", Model: "claude"},
				},
			},
			localAliases: map[string]map[string]string{},
		}

		stream, err := agg.CreateStream(context.Background(), llm.StreamOptions{
			Model:    "smart",
			Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
		})
		require.NoError(t, err)
		<-stream
	})

	t.Run("non-retriable error fails immediately", func(t *testing.T) {
		prov1 := &mockProvider{
			name:      "prov1",
			returnErr: errors.New("authentication failed"),
		}
		prov2 := &mockProvider{name: "prov2"}

		agg := &Provider{
			name: "test",
			providers: map[string]llm.Provider{
				"prov1": prov1,
				"prov2": prov2,
			},
			aliases: map[string][]AliasTarget{
				"smart": {
					{Provider: "prov1", Model: "gpt-4"},
					{Provider: "prov2", Model: "claude"},
				},
			},
			localAliases: map[string]map[string]string{},
		}

		_, err := agg.CreateStream(context.Background(), llm.StreamOptions{
			Model:    "smart",
			Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prov1")
	})

	t.Run("all targets fail with retriable errors", func(t *testing.T) {
		prov1 := &mockProvider{
			name:      "prov1",
			returnErr: errors.New("HTTP429: rate limit"),
		}
		prov2 := &mockProvider{
			name:      "prov2",
			returnErr: errors.New("HTTP 503: service unavailable"),
		}

		agg := &Provider{
			name: "test",
			providers: map[string]llm.Provider{
				"prov1": prov1,
				"prov2": prov2,
			},
			aliases: map[string][]AliasTarget{
				"smart": {
					{Provider: "prov1", Model: "gpt-4"},
					{Provider: "prov2", Model: "claude"},
				},
			},
			localAliases: map[string]map[string]string{},
		}

		_, err := agg.CreateStream(context.Background(), llm.StreamOptions{
			Model:    "smart",
			Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "all targets failed")
	})

	t.Run("unknown alias", func(t *testing.T) {
		agg := &Provider{
			name:         "test",
			providers:    map[string]llm.Provider{},
			aliases:      map[string][]AliasTarget{},
			localAliases: map[string]map[string]string{},
		}

		_, err := agg.CreateStream(context.Background(), llm.StreamOptions{
			Model:    "unknown",
			Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnknownAlias)
	})
}

func TestIsRetriableError(t *testing.T) {
	tests := []struct {
		err       error
		retriable bool
	}{
		{errors.New("HTTP429: rate limit exceeded"), true},
		{errors.New("HTTP 429: too many requests"), true},
		{errors.New("503 service unavailable"), true},
		{errors.New("quota exceeded"), true},
		{errors.New("rate_limit"), true},
		{errors.New("insufficient quota"), true},
		{errors.New("usage limit exceeded"), true},
		{errors.New("authentication failed"), false},
		{errors.New("invalid API key"), false},
		{errors.New("model not found"), false},
		{errors.New("bad request"), false},
		{nil, false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := isRetriableError(tt.err)
			assert.Equal(t, tt.retriable, result)
		})
	}
}

func TestModels(t *testing.T) {
	prov := &mockProvider{name: "prov"}
	agg := &Provider{
		name: "test",
		models: []llm.Model{
			{ID: "smart", Name: "smart", Provider: "aggregate"},
			{ID: "fast", Name: "fast", Provider: "aggregate"},
		},
		providers:    map[string]llm.Provider{"prov": prov},
		aliases:      map[string][]AliasTarget{},
		localAliases: map[string]map[string]string{},
	}

	models := agg.Models()
	require.Len(t, models, 2)
	assert.Equal(t, "smart", models[0].ID)
	assert.Equal(t, "fast", models[1].ID)
}
