package llm

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type serviceTestProvider struct {
	name   string
	models Models
	stream func(context.Context, Buildable) (Stream, error)
}

func (p serviceTestProvider) Name() string   { return p.name }
func (p serviceTestProvider) Models() Models { return p.models }
func (p serviceTestProvider) CreateStream(ctx context.Context, src Buildable) (Stream, error) {
	return p.stream(ctx, src)
}

type testRegistry struct {
	detected []DetectedProvider
	build    func(context.Context, DetectedProvider, *http.Client, []Option) (Provider, error)
}

func (r testRegistry) Detect(context.Context, DetectEnv, map[string]bool) ([]DetectedProvider, error) {
	return r.detected, nil
}
func (r testRegistry) Build(ctx context.Context, req DetectedProvider, client *http.Client, opts []Option) (Provider, error) {
	return r.build(ctx, req, client, opts)
}

func TestServiceCreateStream_UsesRegisteredProvider(t *testing.T) {
	p := serviceTestProvider{name: "fake", models: Models{{ID: "fake-model", Name: "Fake Model", Provider: "fake", Aliases: []string{ModelDefault}}}, stream: completedStream}
	service, err := New(WithProvider(p))
	require.NoError(t, err)
	stream, err := service.CreateStream(context.Background(), Request{Model: "fake-model", Messages: Messages{User("hi")}})
	require.NoError(t, err)
	count := 0
	for range stream {
		count++
	}
	assert.Greater(t, count, 0)
}

func TestServiceCreateStream_IntentAlias(t *testing.T) {
	p := serviceTestProvider{name: "fake", models: Models{{ID: "fake-model", Name: "Fake Model", Provider: "fake", Aliases: []string{ModelDefault}}}, stream: completedStream}
	svc, err := New(WithProvider(p), WithIntentAlias(ModelDefault, IntentSelector{Model: "fake-model"}))
	require.NoError(t, err)
	stream, err := svc.CreateStream(context.Background(), Request{Model: ModelDefault, Messages: Messages{User("hi")}})
	require.NoError(t, err)
	for range stream {
	}
}

func TestServiceCreateStream_FallbackOnRetriableError(t *testing.T) {
	failProvider := serviceTestProvider{name: "anthropic-primary", models: Models{{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Provider: "anthropic"}}, stream: func(context.Context, Buildable) (Stream, error) {
		return nil, NewErrAPIError("anthropic", 429, "rate limit")
	}}
	okProvider := serviceTestProvider{name: "anthropic-secondary", models: nil, stream: completedStream}
	svc, err := New(
		WithRegisteredProvider(RegisteredProvider{Name: "primary", ServiceID: "anthropic", Provider: failProvider}),
		WithRegisteredProvider(RegisteredProvider{Name: "secondary", ServiceID: "anthropic", Provider: okProvider}),
	)
	require.NoError(t, err)
	stream, err := svc.CreateStream(context.Background(), Request{Model: "claude-sonnet-4-6", Messages: Messages{User("hi")}})
	require.NoError(t, err)
	for range stream {
	}
}

func TestServiceCreateStream_NoFallbackOnNonRetriableError(t *testing.T) {
	fatalProvider := serviceTestProvider{name: "openai", models: Models{{ID: "gpt-4o", Name: "GPT-4o", Provider: "openai"}}, stream: func(context.Context, Buildable) (Stream, error) { return nil, errors.New("boom") }}
	otherProvider := serviceTestProvider{name: "openrouter", models: Models{{ID: "gpt-4o", Name: "GPT-4o", Provider: "openrouter"}}, stream: func(context.Context, Buildable) (Stream, error) {
		t.Fatal("unexpected fallback to second provider")
		return nil, nil
	}}
	svc, err := New(WithRegisteredProvider(RegisteredProvider{ServiceID: "openai", Provider: fatalProvider}), WithRegisteredProvider(RegisteredProvider{ServiceID: "openrouter", Provider: otherProvider}))
	require.NoError(t, err)
	_, err = svc.CreateStream(context.Background(), Request{Model: "gpt-4o", Messages: Messages{User("hi")}})
	require.Error(t, err)
	assert.EqualError(t, err, "boom")
}

func TestServiceWrapper_IsApplied(t *testing.T) {
	called := false
	p := serviceTestProvider{name: "openai", models: Models{{ID: "gpt-4o", Name: "GPT-4o", Provider: "openai"}}, stream: completedStream}
	wrapper := func(r RegisteredProvider, next Executor) Executor {
		return StreamFunc(func(ctx context.Context, src Buildable) (Stream, error) {
			called = true
			assert.Equal(t, "openai", r.ServiceID)
			return next.CreateStream(ctx, src)
		})
	}
	svc, err := New(WithRegisteredProvider(RegisteredProvider{ServiceID: "openai", Provider: p}), WithWrapper(wrapper))
	require.NoError(t, err)
	stream, err := svc.CreateStream(context.Background(), Request{Model: "gpt-4o", Messages: Messages{User("hi")}})
	require.NoError(t, err)
	for range stream {
	}
	assert.True(t, called)
}

func TestServiceNew_AutoDetectViaRegistry(t *testing.T) {
	reg := testRegistry{detected: []DetectedProvider{{Name: "test", Type: "test", Order: 1}}, build: func(context.Context, DetectedProvider, *http.Client, []Option) (Provider, error) {
		return serviceTestProvider{name: "test", models: Models{{ID: "test-model", Name: "Test Model", Provider: "test"}}, stream: completedStream}, nil
	}}
	svc, err := New(func(c *ServiceConfig) { c.Registry = reg; c.AutoDetect = true })
	require.NoError(t, err)
	stream, err := svc.CreateStream(context.Background(), Request{Model: "test-model", Messages: Messages{User("hi")}})
	require.NoError(t, err)
	for range stream {
	}
}

func TestServiceCreateStream_CatalogDrivenCandidateSelection(t *testing.T) {
	catalogOnly := serviceTestProvider{name: "anthropic-runtime", models: nil, stream: completedStream}
	other := serviceTestProvider{name: "openai", models: Models{{ID: "gpt-4o", Name: "GPT-4o", Provider: "openai"}}, stream: func(context.Context, Buildable) (Stream, error) {
		t.Fatal("unexpected selection of non-catalog-matching provider")
		return nil, nil
	}}
	svc, err := New(WithRegisteredProvider(RegisteredProvider{ServiceID: "anthropic", Provider: catalogOnly}), WithRegisteredProvider(RegisteredProvider{ServiceID: "openai", Provider: other}))
	require.NoError(t, err)
	stream, err := svc.CreateStream(context.Background(), Request{Model: "claude-sonnet-4-6", Messages: Messages{User("hi")}})
	require.NoError(t, err)
	for range stream {
	}
}

func TestServicePreferenceRule_PrefersConfiguredServiceForIntent(t *testing.T) {
	openaiP := serviceTestProvider{name: "openai", models: Models{{ID: "gpt-4o-mini", Name: "GPT-4o mini", Provider: "openai"}}, stream: func(context.Context, Buildable) (Stream, error) {
		t.Fatal("openai should not be chosen first")
		return nil, nil
	}}
	anthropicP := serviceTestProvider{name: "anthropic", models: Models{{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Provider: "anthropic", Aliases: []string{"fast-model"}}}, stream: completedStream}
	svc, err := New(
		WithRegisteredProvider(RegisteredProvider{ServiceID: "openai", Provider: openaiP}),
		WithRegisteredProvider(RegisteredProvider{ServiceID: "anthropic", Provider: anthropicP}),
		WithIntentAlias("fast", IntentSelector{Model: "fast-model"}),
		WithPreference(PreferenceRule{Intent: "fast", ServiceIDs: []string{"anthropic"}}),
	)
	require.NoError(t, err)
	stream, err := svc.CreateStream(context.Background(), Request{Model: "fast", Messages: Messages{User("hi")}})
	require.NoError(t, err)
	for range stream {
	}
}

func completedStream(context.Context, Buildable) (Stream, error) {
	pub, ch := NewEventPublisher()
	go func() {
		defer pub.Close()
		pub.Completed(CompletedEvent{StopReason: StopReasonEndTurn})
	}()
	return ch, nil
}

func TestServiceCreateStream_AmbiguousModelReturnsHelpfulError(t *testing.T) {
	openaiP := serviceTestProvider{name: "openai", models: Models{{ID: "shared-model", Name: "Shared", Provider: "openai", Aliases: []string{"ambiguous"}}}, stream: completedStream}
	anthropicP := serviceTestProvider{name: "anthropic", models: Models{{ID: "shared-model", Name: "Shared", Provider: "anthropic", Aliases: []string{"ambiguous"}}}, stream: completedStream}
	svc, err := New(
		WithRegisteredProvider(RegisteredProvider{ServiceID: "openai", Provider: openaiP}),
		WithRegisteredProvider(RegisteredProvider{ServiceID: "anthropic", Provider: anthropicP}),
	)
	require.NoError(t, err)
	resolved, _, err := svc.ExplainModel("ambiguous")
	require.NoError(t, err)
	resolved.Ambiguous = true
	err = ambiguousModelError(resolved)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `model "ambiguous" is ambiguous`)
	assert.Contains(t, err.Error(), "anthropic/")
}

func TestServiceExplainModel(t *testing.T) {
	anthropicP := serviceTestProvider{name: "anthropic", models: nil, stream: completedStream}
	svc, err := New(WithRegisteredProvider(RegisteredProvider{ServiceID: "anthropic", Provider: anthropicP}))
	require.NoError(t, err)
	resolved, candidates, err := svc.ExplainModel("claude-sonnet-4-6")
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-4-6", resolved.RequestedModel)
	require.NotEmpty(t, resolved.Offerings)
	require.Len(t, candidates, 1)
	assert.Equal(t, "anthropic", candidates[0].ServiceID)
}
