package auto

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	providerregistry "github.com/codewandler/llm/internal/providerregistry"
	"github.com/codewandler/llm/provider/anthropic/claude"
)

// mockTokenStore implements claude.TokenStore for testing.
type mockTokenStore struct{ tokens map[string]*claude.Token }

func newMockTokenStore() *mockTokenStore {
	return &mockTokenStore{tokens: make(map[string]*claude.Token)}
}
func (s *mockTokenStore) Load(context.Context, string) (*claude.Token, error) { return nil, nil }
func (s *mockTokenStore) Save(context.Context, string, *claude.Token) error   { return nil }
func (s *mockTokenStore) Delete(context.Context, string) error                { return nil }
func (s *mockTokenStore) List(context.Context) ([]string, error) {
	keys := make([]string, 0, len(s.tokens))
	for k := range s.tokens {
		keys = append(keys, k)
	}
	return keys, nil
}

func TestNew_WithExplicitProviders(t *testing.T) {
	ctx := context.Background()
	svc, err := New(ctx, WithoutAutoDetect(), WithOpenAI())
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestNew_WithClaudeAccount(t *testing.T) {
	ctx := context.Background()
	store := newMockTokenStore()
	svc, err := New(ctx, WithoutAutoDetect(), WithClaudeAccount("test-account", store), WithOpenAI())
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestNew_WithClaudeStore(t *testing.T) {
	ctx := context.Background()
	store := newMockTokenStore()
	store.tokens["work"] = &claude.Token{AccessToken: "work-token"}
	store.tokens["personal"] = &claude.Token{AccessToken: "personal-token"}
	svc, err := New(ctx, WithoutAutoDetect(), WithClaude(store), WithOpenAI())
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestNew_NoProviders(t *testing.T) {
	ctx := context.Background()
	store := newMockTokenStore()
	_, err := New(ctx, WithoutAutoDetect(), WithClaude(store))
	require.Error(t, err)
}

func TestWithCodexLocal_BuildsService(t *testing.T) {
	ctx := context.Background()
	svc, err := New(ctx, WithoutAutoDetect(), WithCodexLocal(), WithOpenAI())
	if err != nil {
		t.Skip("codex local auth not available in test environment")
	}
	require.NotNil(t, svc)
}

func TestBuildBuiltinIntentAliases(t *testing.T) {
	aliases := builtinIntentAliases()
	assert.Contains(t, aliases, AliasFast)
	assert.Contains(t, aliases, AliasDefault)
	assert.Contains(t, aliases, AliasPowerful)
}

func TestProviderRegistryRegisterClaudeAccounts(t *testing.T) {
	store := newMockTokenStore()
	store.tokens["work"] = &claude.Token{AccessToken: "work-token"}
	store.tokens["personal"] = &claude.Token{AccessToken: "personal-token"}
	items, err := providerregistry.RegisterClaudeAccounts(context.Background(), store)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "personal", items[0].Name)
	assert.Equal(t, "work", items[1].Name)
}

func TestWithoutBuiltinAliases_StillBuildsService(t *testing.T) {
	ctx := context.Background()
	svc, err := New(ctx, WithoutAutoDetect(), WithoutBuiltinAliases(), WithOpenAI())
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestWithGlobalAlias_BuildsIntentAlias(t *testing.T) {
	ctx := context.Background()
	svc, err := New(ctx, WithoutAutoDetect(), WithOpenAI(), WithGlobalAlias("review", "openai/gpt-4o"))
	require.NoError(t, err)
	require.NotNil(t, svc)

	stream, err := svc.CreateStream(ctx, llm.Request{Model: "review", Messages: llm.Messages{llm.User("hi")}})
	if err == nil {
		for range stream {
		}
	}
}
