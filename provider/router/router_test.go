package router

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

// mockProvider is a test provider that can be configured to succeed or fail.
type mockProvider struct {
	name       string
	models     llm.Models
	returnErr  error
	streamFunc func(ctx context.Context, opts llm.Request) (llm.Stream, error)
}

func (m *mockProvider) Name() string                              { return m.name }
func (m *mockProvider) Models() llm.Models                        { return m.models }
func (m *mockProvider) Resolve(modelID string) (llm.Model, error) { return m.models.Resolve(modelID) }
func (m *mockProvider) CreateStream(ctx context.Context, opts llm.Request) (llm.Stream, error) {
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	if m.streamFunc != nil {
		return m.streamFunc(ctx, opts)
	}
	ch := make(chan llm.Envelope, 1)
	go func() {
		ch <- llm.Envelope{Type: llm.StreamEventCompleted, Data: &llm.CompletedEvent{}}
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
		prov1 := &mockProvider{
			name:   "prov1",
			models: llm.Models{{ID: "model-a", Name: "ModelA", Provider: "prov1"}},
		}

		cfg := Config{
			Name: "my-aggregate",
			Providers: []ProviderInstanceConfig{
				{Name: "prov1", Type: "mock"},
			},
			Aliases: map[string][]AliasTarget{
				"smart": {{Provider: "prov1", Model: "model-a"}},
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
		prov := &mockProvider{
			name:   "prov",
			models: []llm.Model{{ID: "model", Name: "Model", Provider: "prov"}},
		}
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
		assert.Equal(t, "router", agg.Name())
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

func TestModels(t *testing.T) {
	t.Run("collects models from providers", func(t *testing.T) {
		prov1 := &mockProvider{
			name: "prov1",
			models: []llm.Model{
				{ID: "model-a", Name: "Model A", Provider: "mock"},
			},
		}
		prov2 := &mockProvider{
			name: "prov2",
			models: []llm.Model{
				{ID: "model-b", Name: "Model B", Provider: "mock"},
			},
		}

		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "prov1", Type: "type1"},
				{Name: "prov2", Type: "type2"},
			},
		}

		factories := map[string]Factory{
			"type1": mockFactory(prov1),
			"type2": mockFactory(prov2),
		}

		agg, err := New(cfg, factories)
		require.NoError(t, err)

		models := agg.Models()
		require.Len(t, models, 2)

		// Check that full IDs are constructed
		var foundProv1, foundProv2 bool
		for _, m := range models {
			if m.ID == "prov1/type1/model-a" {
				foundProv1 = true
				assert.Equal(t, "Model A", m.Name)
				assert.Equal(t, "prov1/type1", m.Provider)
			}
			if m.ID == "prov2/type2/model-b" {
				foundProv2 = true
				assert.Equal(t, "Model B", m.Name)
				assert.Equal(t, "prov2/type2", m.Provider)
			}
		}
		assert.True(t, foundProv1, "should have prov1/model-a")
		assert.True(t, foundProv2, "should have prov2/model-b")
	})

	t.Run("adds aliases to models", func(t *testing.T) {
		prov := &mockProvider{
			name: "prov",
			models: []llm.Model{
				{ID: "claude-sonnet", Name: "Claude Sonnet", Provider: "anthropic"},
			},
		}

		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{
					Name:         "work-claude",
					Type:         "anthropic",
					ModelAliases: map[string]string{"sonnet": "claude-sonnet"},
				},
			},
			Aliases: map[string][]AliasTarget{
				"smart": {{Provider: "work-claude", Model: "sonnet"}},
			},
		}

		factories := map[string]Factory{"anthropic": mockFactory(prov)}
		agg, err := New(cfg, factories)
		require.NoError(t, err)

		models := agg.Models()
		require.Len(t, models, 1)

		model := models[0]
		assert.Equal(t, "work-claude/anthropic/claude-sonnet", model.ID)
		// Global alias "smart" is accessible bare
		assert.Contains(t, model.Aliases, "smart")
		// Local alias "sonnet" is only accessible with prefix, not bare
		assert.NotContains(t, model.Aliases, "sonnet")
		assert.Contains(t, model.Aliases, "work-claude/anthropic/smart")
		assert.Contains(t, model.Aliases, "work-claude/anthropic/sonnet")
		assert.Contains(t, model.Aliases, "anthropic/smart")
		assert.Contains(t, model.Aliases, "anthropic/sonnet")
	})
}

func TestResolve(t *testing.T) {
	prov := &mockProvider{
		name: "prov",
		models: []llm.Model{
			{ID: "claude-sonnet", Name: "Claude Sonnet", Provider: "anthropic"},
		},
	}

	cfg := Config{
		Providers: []ProviderInstanceConfig{
			{
				Name:         "work-claude",
				Type:         "anthropic",
				ModelAliases: map[string]string{"sonnet": "claude-sonnet"},
			},
		},
		Aliases: map[string][]AliasTarget{
			"smart": {{Provider: "work-claude", Model: "sonnet"}},
		},
	}

	factories := map[string]Factory{"anthropic": mockFactory(prov)}
	agg, err := New(cfg, factories)
	require.NoError(t, err)

	tests := []struct {
		input   string
		wantID  string
		wantErr bool
	}{
		// Global alias works bare
		{"smart", "work-claude/anthropic/claude-sonnet", false},
		// Local alias does NOT work bare - must be prefixed
		{"sonnet", "", true},
		// Full model ToolCallID works
		{"claude-sonnet", "work-claude/anthropic/claude-sonnet", false},
		{"anthropic/claude-sonnet", "work-claude/anthropic/claude-sonnet", false},
		{"work-claude/anthropic/claude-sonnet", "work-claude/anthropic/claude-sonnet", false},
		// Local alias works with prefix
		{"work-claude/anthropic/sonnet", "work-claude/anthropic/claude-sonnet", false},
		{"anthropic/sonnet", "work-claude/anthropic/claude-sonnet", false},
		{"unknown", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			model, err := agg.Resolve(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrUnknownModel)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantID, model.ID)
			}
		})
	}
}

func TestCreateStream(t *testing.T) {
	t.Run("successful stream", func(t *testing.T) {
		prov := &mockProvider{
			name: "prov1",
			streamFunc: func(ctx context.Context, opts llm.Request) (llm.Stream, error) {
				ch := make(chan llm.Envelope, 2)
				go func() {
					ch <- llm.Envelope{Type: llm.StreamEventDelta, Data: llm.TextDelta("hello")}
					ch <- llm.Envelope{Type: llm.StreamEventCompleted, Data: &llm.CompletedEvent{}}
					close(ch)
				}()
				return ch, nil
			},
			models: []llm.Model{{ID: "gpt-4", Name: "GPT-4", Provider: "openai"}},
		}

		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "prov1", Type: "openai"},
			},
		}

		factories := map[string]Factory{"openai": mockFactory(prov)}
		agg, err := New(cfg, factories)
		require.NoError(t, err)

		stream, err := agg.CreateStream(context.Background(), llm.Request{
			Model:    "gpt-4",
			Messages: llm.Messages{llm.User("hi")},
		})
		require.NoError(t, err)

		var events []llm.Envelope
		for evt := range stream {
			events = append(events, evt)
		}
		assert.Len(t, events, 4) // created, model_resolved, delta, done
		assert.Equal(t, llm.StreamEventCreated, events[0].Type)
		assert.Equal(t, llm.StreamEventModelResolved, events[1].Type)
		if ev, ok := events[1].Data.(*llm.ModelResolvedEvent); ok {
			assert.Equal(t, "router", ev.Resolver) // router's default name
			assert.Equal(t, "gpt-4", ev.Name)
			assert.NotEmpty(t, ev.Resolved)
		}
		assert.Equal(t, llm.StreamEventDelta, events[2].Type)
		assert.Equal(t, llm.StreamEventCompleted, events[3].Type)
	})

	t.Run("failover to second provider", func(t *testing.T) {
		prov1 := &mockProvider{
			name:      "prov1",
			returnErr: errors.New("HTTP429: rate limit exceeded"),
			models:    []llm.Model{{ID: "gpt-4", Name: "GPT-4", Provider: "openai"}},
		}
		prov2 := &mockProvider{
			name: "prov2",
			streamFunc: func(ctx context.Context, opts llm.Request) (llm.Stream, error) {
				ch := make(chan llm.Envelope, 1)
				go func() {
					ch <- llm.Envelope{Type: llm.StreamEventCompleted, Data: &llm.CompletedEvent{}}
					close(ch)
				}()
				return ch, nil
			},
			models: []llm.Model{{ID: "claude", Name: "Claude", Provider: "anthropic"}},
		}

		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "prov1", Type: "openai"},
				{Name: "prov2", Type: "anthropic"},
			},
			Aliases: map[string][]AliasTarget{
				"smart": {
					{Provider: "prov1", Model: "gpt-4"},
					{Provider: "prov2", Model: "claude"},
				},
			},
		}

		factories := map[string]Factory{
			"openai":    mockFactory(prov1),
			"anthropic": mockFactory(prov2),
		}
		agg, err := New(cfg, factories)
		require.NoError(t, err)

		stream, err := agg.CreateStream(context.Background(), llm.Request{
			Model:    "smart",
			Messages: llm.Messages{llm.User("hi")},
		})
		require.NoError(t, err)
		<-stream
	})

	t.Run("non-retriable error fails immediately", func(t *testing.T) {
		prov1 := &mockProvider{
			name:      "prov1",
			returnErr: errors.New("authentication failed"),
			models:    []llm.Model{{ID: "gpt-4", Name: "GPT-4", Provider: "openai"}},
		}
		prov2 := &mockProvider{
			name:   "prov2",
			models: []llm.Model{{ID: "claude", Name: "Claude", Provider: "anthropic"}},
		}

		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "prov1", Type: "openai"},
				{Name: "prov2", Type: "anthropic"},
			},
			Aliases: map[string][]AliasTarget{
				"smart": {
					{Provider: "prov1", Model: "gpt-4"},
					{Provider: "prov2", Model: "claude"},
				},
			},
		}

		factories := map[string]Factory{
			"openai":    mockFactory(prov1),
			"anthropic": mockFactory(prov2),
		}
		agg, err := New(cfg, factories)
		require.NoError(t, err)

		_, err = agg.CreateStream(context.Background(), llm.Request{
			Model:    "smart",
			Messages: llm.Messages{llm.User("hi")},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prov1")
	})

	t.Run("all targets fail with retriable errors", func(t *testing.T) {
		prov1 := &mockProvider{
			name:      "prov1",
			returnErr: llm.NewErrAPIError("prov1", 429, "rate limit"),
			models:    []llm.Model{{ID: "gpt-4", Name: "GPT-4", Provider: "openai"}},
		}
		prov2 := &mockProvider{
			name:      "prov2",
			returnErr: llm.NewErrAPIError("prov2", 503, "service unavailable"),
			models:    []llm.Model{{ID: "claude", Name: "Claude", Provider: "anthropic"}},
		}

		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "prov1", Type: "openai"},
				{Name: "prov2", Type: "anthropic"},
			},
			Aliases: map[string][]AliasTarget{
				"smart": {
					{Provider: "prov1", Model: "gpt-4"},
					{Provider: "prov2", Model: "claude"},
				},
			},
		}

		factories := map[string]Factory{
			"openai":    mockFactory(prov1),
			"anthropic": mockFactory(prov2),
		}
		agg, err := New(cfg, factories)
		require.NoError(t, err)

		_, err = agg.CreateStream(context.Background(), llm.Request{
			Model:    "smart",
			Messages: llm.Messages{llm.User("hi")},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, llm.ErrNoProviders)
		// Original provider errors must not be swallowed.
		assert.ErrorIs(t, err, llm.ErrAPIError)
		assert.Contains(t, err.Error(), "429")
		assert.Contains(t, err.Error(), "503")
	})

	t.Run("single provider 402 preserves original error", func(t *testing.T) {
		prov := &mockProvider{
			name:      "openrouter",
			returnErr: llm.NewErrAPIError("openrouter", 402, "Payment Required: insufficient credits"),
			models:    []llm.Model{{ID: "gpt-4", Name: "GPT-4", Provider: "openrouter"}},
		}

		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "openrouter", Type: "openrouter"},
			},
		}

		factories := map[string]Factory{"openrouter": mockFactory(prov)}
		agg, err := New(cfg, factories)
		require.NoError(t, err)

		_, err = agg.CreateStream(context.Background(), llm.Request{
			Model:    "gpt-4",
			Messages: llm.Messages{llm.User("hi")},
		})
		require.Error(t, err)
		// ErrNoProviders because all targets exhausted.
		assert.ErrorIs(t, err, llm.ErrNoProviders)
		// But the original 402 must be accessible — not swallowed.
		assert.ErrorIs(t, err, llm.ErrAPIError)
		assert.Contains(t, err.Error(), "402")
		assert.Contains(t, err.Error(), "insufficient credits")
	})

	t.Run("unknown model", func(t *testing.T) {
		prov := &mockProvider{
			name:   "prov",
			models: []llm.Model{{ID: "model", Name: "Model", Provider: "type"}},
		}

		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "prov", Type: "type"},
			},
		}

		factories := map[string]Factory{"type": mockFactory(prov)}
		agg, err := New(cfg, factories)
		require.NoError(t, err)

		_, err = agg.CreateStream(context.Background(), llm.Request{
			Model:    "unknown",
			Messages: llm.Messages{llm.User("hi")},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnknownModel)
	})

	t.Run("emits ProviderFailoverEvent then ModelResolvedEvent on failover", func(t *testing.T) {
		prov1 := &mockProvider{
			name:      "prov1",
			returnErr: llm.NewErrAPIError("prov1", 429, "rate limited"),
			models:    []llm.Model{{ID: "m", Provider: "type1"}},
		}
		prov2 := &mockProvider{
			name: "prov2",
			streamFunc: func(_ context.Context, _ llm.Request) (llm.Stream, error) {
				ch := make(chan llm.Envelope, 1)
				go func() {
					ch <- llm.Envelope{Type: llm.StreamEventCompleted, Data: &llm.CompletedEvent{}}
					close(ch)
				}()
				return ch, nil
			},
			models: []llm.Model{{ID: "m", Provider: "type2"}},
		}

		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "prov1", Type: "type1"},
				{Name: "prov2", Type: "type2"},
			},
			Aliases: map[string][]AliasTarget{
				"fast": {
					{Provider: "prov1", Model: "m"},
					{Provider: "prov2", Model: "m"},
				},
			},
		}
		agg, err := New(cfg, map[string]Factory{
			"type1": mockFactory(prov1),
			"type2": mockFactory(prov2),
		})
		require.NoError(t, err)

		stream, err := agg.CreateStream(context.Background(), llm.Request{
			Model:    "fast",
			Messages: llm.Messages{llm.User("hi")},
		})
		require.NoError(t, err)

		var events []llm.Envelope
		for evt := range stream {
			events = append(events, evt)
		}

		// Expect: created, provider_failover, model_resolved, completed
		require.GreaterOrEqual(t, len(events), 3)
		assert.Equal(t, llm.StreamEventCreated, events[0].Type)
		assert.Equal(t, llm.StreamEventProviderFailover, events[1].Type)
		if fev, ok := events[1].Data.(*llm.ProviderFailoverEvent); ok {
			assert.Equal(t, "prov1", fev.Provider)
			assert.Equal(t, "prov2", fev.FailoverProvider)
			assert.NotNil(t, fev.Error)
		}
		assert.Equal(t, llm.StreamEventModelResolved, events[2].Type)
		if mev, ok := events[2].Data.(*llm.ModelResolvedEvent); ok {
			assert.Equal(t, "fast", mev.Name)
			assert.NotEmpty(t, mev.Resolved)
		}
	})

	t.Run("all retriable failures return ErrAllProvidersFailed", func(t *testing.T) {
		prov1 := &mockProvider{
			name:      "prov1",
			returnErr: llm.NewErrAPIError("prov1", 429, "rate limited"),
			models:    []llm.Model{{ID: "m", Provider: "type1"}},
		}
		prov2 := &mockProvider{
			name:      "prov2",
			returnErr: llm.NewErrAPIError("prov2", 503, "unavailable"),
			models:    []llm.Model{{ID: "m", Provider: "type2"}},
		}

		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "prov1", Type: "type1"},
				{Name: "prov2", Type: "type2"},
			},
			Aliases: map[string][]AliasTarget{
				"fast": {
					{Provider: "prov1", Model: "m"},
					{Provider: "prov2", Model: "m"},
				},
			},
		}
		agg, err := New(cfg, map[string]Factory{
			"type1": mockFactory(prov1),
			"type2": mockFactory(prov2),
		})
		require.NoError(t, err)

		stream, err := agg.CreateStream(context.Background(), llm.Request{
			Model:    "fast",
			Messages: llm.Messages{llm.User("hi")},
		})
		// No stream returned — error only.
		require.Error(t, err)
		assert.Nil(t, stream)
		assert.ErrorIs(t, err, llm.ErrNoProviders)
	})
}

func TestIsRetriableError(t *testing.T) {
	mkpe := func(msg string, statusCode int) *llm.ProviderError {
		return &llm.ProviderError{
			Sentinel:   llm.ErrAPIError,
			Provider:   "test",
			Message:    msg,
			StatusCode: statusCode,
		}
	}
	tests := []struct {
		pe        *llm.ProviderError
		retriable bool
	}{
		{mkpe("rate limit exceeded", 429), true},
		{mkpe("too many requests", 429), true},
		{mkpe("service unavailable", 503), true},
		{mkpe("quota exceeded", 0), true},
		{mkpe("rate_limit", 0), true},
		{mkpe("insufficient quota", 0), true},
		{mkpe("usage limit exceeded", 0), true},
		{mkpe("payment required", 402), true},
		{mkpe("authentication failed", 401), false},
		{mkpe("invalid API key", 403), false},
		{mkpe("model not found", 404), false},
		{mkpe("bad request", 400), false},
		{nil, false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := isRetriableError(tt.pe)
			assert.Equal(t, tt.retriable, result)
		})
	}
}
