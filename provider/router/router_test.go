package router

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
	streamFunc func(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error)
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Models() []llm.Model { return m.models }

func (m *mockProvider) CreateStream(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
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
		prov1 := &mockProvider{
			name:   "prov1",
			models: []llm.Model{{ID: "model-a", Name: "ModelA", Provider: "prov1"}},
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
		// Full model ID works
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
			streamFunc: func(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
				ch := make(chan llm.StreamEvent, 1)
				go func() {
					ch <- llm.StreamEvent{Type: llm.StreamEventDelta, Delta: llm.TextDelta(nil, "hello")}
					ch <- llm.StreamEvent{Type: llm.StreamEventDone}
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

		stream, err := agg.CreateStream(context.Background(), llm.StreamRequest{
			Model:    "gpt-4",
			Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
		})
		require.NoError(t, err)

		var events []llm.StreamEvent
		for evt := range stream {
			events = append(events, evt)
		}
		assert.Len(t, events, 4) // created, routed, delta, done
		assert.Equal(t, llm.StreamEventCreated, events[0].Type)
		assert.Equal(t, llm.StreamEventRouted, events[1].Type)
		assert.Equal(t, llm.StreamEventDelta, events[2].Type)
		assert.Equal(t, llm.StreamEventDone, events[3].Type)
	})

	t.Run("failover to second provider", func(t *testing.T) {
		prov1 := &mockProvider{
			name:      "prov1",
			returnErr: errors.New("HTTP429: rate limit exceeded"),
			models:    []llm.Model{{ID: "gpt-4", Name: "GPT-4", Provider: "openai"}},
		}
		prov2 := &mockProvider{
			name: "prov2",
			streamFunc: func(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
				ch := make(chan llm.StreamEvent, 1)
				go func() {
					ch <- llm.StreamEvent{Type: llm.StreamEventDone}
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

		stream, err := agg.CreateStream(context.Background(), llm.StreamRequest{
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

		_, err = agg.CreateStream(context.Background(), llm.StreamRequest{
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
			models:    []llm.Model{{ID: "gpt-4", Name: "GPT-4", Provider: "openai"}},
		}
		prov2 := &mockProvider{
			name:      "prov2",
			returnErr: errors.New("HTTP 503: service unavailable"),
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

		_, err = agg.CreateStream(context.Background(), llm.StreamRequest{
			Model:    "smart",
			Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, llm.ErrNoProviders)
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

		_, err = agg.CreateStream(context.Background(), llm.StreamRequest{
			Model:    "unknown",
			Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnknownModel)
	})
}

// makePerInstanceFactory returns a Factory that maps instance names to pre-built providers.
// This is needed because router.New calls the same factory key for each instance,
// so we need a stateful factory that vends the right provider per call order.
func makePerInstanceFactory(provs ...*mockProvider) Factory {
	idx := 0
	return func(opts ...llm.Option) llm.Provider {
		p := provs[idx]
		idx++
		return p
	}
}

func TestCreateStream_MidStreamFailover(t *testing.T) {
	t.Run("failover on in-stream overload error", func(t *testing.T) {
		// prov1 returns a 200 OK but then emits a retriable error event mid-stream
		// (e.g. Anthropic's "overloaded_error" SSE error event).
		prov1 := &mockProvider{
			name: "prov1",
			streamFunc: func(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
				ch := make(chan llm.StreamEvent, 4)
				go func() {
					defer close(ch)
					ch <- llm.StreamEvent{Type: llm.StreamEventCreated}
					ch <- llm.StreamEvent{
						Type: llm.StreamEventError,
						Error: &llm.ProviderError{
							Sentinel: llm.ErrProviderError,
							Provider: "prov1",
							Message:  "Overloaded",
						},
					}
				}()
				return ch, nil
			},
			models: []llm.Model{{ID: "claude", Name: "Claude", Provider: "anthropic"}},
		}

		prov2 := &mockProvider{
			name: "prov2",
			streamFunc: func(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
				ch := make(chan llm.StreamEvent, 4)
				go func() {
					defer close(ch)
					ch <- llm.StreamEvent{Type: llm.StreamEventDelta, Delta: llm.TextDelta(nil, "hello from fallback")}
					ch <- llm.StreamEvent{Type: llm.StreamEventDone}
				}()
				return ch, nil
			},
			models: []llm.Model{{ID: "claude", Name: "Claude", Provider: "anthropic"}},
		}

		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "prov1", Type: "anthropic"},
				{Name: "prov2", Type: "anthropic"},
			},
			Aliases: map[string][]AliasTarget{
				"smart": {
					{Provider: "prov1", Model: "claude"},
					{Provider: "prov2", Model: "claude"},
				},
			},
		}

		r, err := New(cfg, map[string]Factory{
			"anthropic": makePerInstanceFactory(prov1, prov2),
		})
		require.NoError(t, err)

		stream, err := r.CreateStream(context.Background(), llm.StreamRequest{
			Model:    "smart",
			Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
		})
		require.NoError(t, err)

		var events []llm.StreamEvent
		for evt := range stream {
			events = append(events, evt)
		}

		// Must not contain an error event — failover should have happened
		for _, evt := range events {
			assert.NotEqual(t, llm.StreamEventError, evt.Type,
				"expected failover, got error event: %v", evt.Error)
		}

		// Must contain the fallback delta
		var gotFallback bool
		for _, evt := range events {
			if evt.Type == llm.StreamEventDelta && evt.Delta != nil && evt.Delta.Text == "hello from fallback" {
				gotFallback = true
			}
		}
		assert.True(t, gotFallback, "expected delta from fallback provider")

		// Must contain a second Routed event for the fallback
		var routedCount int
		for _, evt := range events {
			if evt.Type == llm.StreamEventRouted {
				routedCount++
			}
		}
		assert.Equal(t, 2, routedCount, "expected initial routed + failover routed events")
	})

	t.Run("non-retriable in-stream error is not retried", func(t *testing.T) {
		prov1 := &mockProvider{
			name: "prov1",
			streamFunc: func(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
				ch := make(chan llm.StreamEvent, 4)
				go func() {
					defer close(ch)
					ch <- llm.StreamEvent{Type: llm.StreamEventCreated}
					ch <- llm.StreamEvent{
						Type: llm.StreamEventError,
						Error: &llm.ProviderError{
							Sentinel: llm.ErrProviderError,
							Provider: "prov1",
							Message:  "invalid_api_key",
						},
					}
				}()
				return ch, nil
			},
			models: []llm.Model{{ID: "claude", Name: "Claude", Provider: "anthropic"}},
		}
		prov2Called := false
		prov2 := &mockProvider{
			name: "prov2",
			streamFunc: func(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
				prov2Called = true
				ch := make(chan llm.StreamEvent, 2)
				close(ch)
				return ch, nil
			},
			models: []llm.Model{{ID: "claude", Name: "Claude", Provider: "anthropic"}},
		}

		cfg := Config{
			Providers: []ProviderInstanceConfig{
				{Name: "prov1", Type: "anthropic"},
				{Name: "prov2", Type: "anthropic"},
			},
			Aliases: map[string][]AliasTarget{
				"smart": {
					{Provider: "prov1", Model: "claude"},
					{Provider: "prov2", Model: "claude"},
				},
			},
		}

		r, err := New(cfg, map[string]Factory{
			"anthropic": makePerInstanceFactory(prov1, prov2),
		})
		require.NoError(t, err)

		stream, err := r.CreateStream(context.Background(), llm.StreamRequest{
			Model:    "smart",
			Messages: llm.Messages{&llm.UserMsg{Content: "hi"}},
		})
		require.NoError(t, err)

		var gotError bool
		for evt := range stream {
			if evt.Type == llm.StreamEventError {
				gotError = true
			}
		}
		assert.True(t, gotError, "expected error event to be forwarded")
		assert.False(t, prov2Called, "prov2 should not be called for non-retriable error")
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
