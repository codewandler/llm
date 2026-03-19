package auto

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/bedrock"
)

// mockTokenStore implements claude.TokenStore for testing.
type mockTokenStore struct {
	tokens map[string]*claude.Token
}

func newMockTokenStore() *mockTokenStore {
	return &mockTokenStore{
		tokens: make(map[string]*claude.Token),
	}
}

func (s *mockTokenStore) Load(ctx context.Context, key string) (*claude.Token, error) {
	return s.tokens[key], nil
}

func (s *mockTokenStore) Save(ctx context.Context, key string, token *claude.Token) error {
	s.tokens[key] = token
	return nil
}

func (s *mockTokenStore) Delete(ctx context.Context, key string) error {
	delete(s.tokens, key)
	return nil
}

func (s *mockTokenStore) List(ctx context.Context) ([]string, error) {
	var keys []string
	for k := range s.tokens {
		keys = append(keys, k)
	}
	return keys, nil
}

func TestNew_WithExplicitProviders(t *testing.T) {
	ctx := context.Background()

	// Test with explicit bedrock only
	p, err := New(ctx,
		WithoutAutoDetect(),
		WithBedrock(),
	)
	require.NoError(t, err)
	require.NotNil(t, p)

	assert.Equal(t, "auto", p.Name())
	assert.NotEmpty(t, p.Models())
}

func TestNew_WithName(t *testing.T) {
	ctx := context.Background()

	p, err := New(ctx,
		WithoutAutoDetect(),
		WithName("test-provider"),
		WithBedrock(),
	)
	require.NoError(t, err)
	assert.Equal(t, "test-provider", p.Name())
}

func TestNew_WithClaudeAccount(t *testing.T) {
	ctx := context.Background()
	store := newMockTokenStore()

	p, err := New(ctx,
		WithoutAutoDetect(),
		WithClaudeAccount("test-account", store),
		WithBedrock(),
	)
	require.NoError(t, err)
	require.NotNil(t, p)

	// Should have models from both providers
	models := p.Models()
	assert.NotEmpty(t, models)
}

func TestNew_WithClaudeStore(t *testing.T) {
	ctx := context.Background()
	store := newMockTokenStore()

	// Add some accounts to the store
	store.tokens["work"] = &claude.Token{AccessToken: "work-token"}
	store.tokens["personal"] = &claude.Token{AccessToken: "personal-token"}

	p, err := New(ctx,
		WithoutAutoDetect(),
		WithClaude(store),
		WithBedrock(),
	)
	require.NoError(t, err)
	require.NotNil(t, p)

	// Should have models - exact count depends on provider model lists
	models := p.Models()
	assert.NotEmpty(t, models)
}

func TestNew_NoProviders(t *testing.T) {
	ctx := context.Background()

	// Empty store and no auto-detect should fail
	store := newMockTokenStore()

	_, err := New(ctx,
		WithoutAutoDetect(),
		WithClaude(store), // Empty store
	)
	require.Error(t, err)
}

func TestBuildAliasTargets(t *testing.T) {
	tests := []struct {
		name         string
		instanceName string
		providerType string
		wantAliases  []string
	}{
		{
			name:         "claude provider",
			instanceName: "claude",
			providerType: ProviderClaude,
			wantAliases:  []string{AliasFast, AliasDefault, AliasPowerful},
		},
		{
			name:         "bedrock provider",
			instanceName: "bedrock",
			providerType: ProviderBedrock,
			wantAliases:  []string{AliasFast, AliasDefault, AliasPowerful},
		},
		{
			name:         "openai provider (no aliases)",
			instanceName: "openai",
			providerType: ProviderOpenAI,
			wantAliases:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targets := buildAliasTargets(tt.instanceName, tt.providerType)

			if tt.wantAliases == nil {
				assert.Nil(t, targets)
				return
			}

			require.NotNil(t, targets)
			for _, alias := range tt.wantAliases {
				_, ok := targets[alias]
				assert.True(t, ok, "missing alias %s", alias)
			}
		})
	}
}

func TestModelAliasesForProvider(t *testing.T) {
	// Claude should have model aliases
	claudeAliases := modelAliasesForProvider(ProviderClaude)
	require.NotNil(t, claudeAliases)
	assert.Equal(t, ClaudeSonnet, claudeAliases["sonnet"])
	assert.Equal(t, ClaudeOpus, claudeAliases["opus"])
	assert.Equal(t, ClaudeHaiku, claudeAliases["haiku"])

	// Anthropic should have model aliases
	anthropicAliases := modelAliasesForProvider(ProviderAnthropic)
	require.NotNil(t, anthropicAliases)
	assert.Equal(t, AnthropicSonnet, anthropicAliases["sonnet"])

	// OpenAI should have model aliases
	openaiAliases := modelAliasesForProvider(ProviderOpenAI)
	require.NotNil(t, openaiAliases)
	assert.Equal(t, "gpt-5.4", openaiAliases["flagship"])
	assert.Equal(t, "gpt-5.4-mini", openaiAliases["mini"])
	assert.Equal(t, "gpt-5.4-nano", openaiAliases["nano"])
	assert.Equal(t, "gpt-5.4-pro", openaiAliases["pro"])
	assert.Equal(t, "gpt-5.3-codex", openaiAliases["codex"])
	assert.Equal(t, "o4-mini", openaiAliases["o4"])
	assert.Equal(t, "o3", openaiAliases["o3"])
}

func TestConstants(t *testing.T) {
	// Verify constants are not empty
	assert.NotEmpty(t, ProviderClaude)
	assert.NotEmpty(t, ProviderBedrock)
	assert.NotEmpty(t, ProviderOpenAI)
	assert.NotEmpty(t, ProviderOpenRouter)
	assert.NotEmpty(t, ProviderAnthropic)

	assert.NotEmpty(t, EnvOpenAIKey)
	assert.NotEmpty(t, EnvOpenRouterKey)
	assert.NotEmpty(t, EnvAnthropicKey)

	assert.NotEmpty(t, AliasFast)
	assert.NotEmpty(t, AliasDefault)
	assert.NotEmpty(t, AliasPowerful)

	assert.NotEmpty(t, ClaudeOpus)
	assert.NotEmpty(t, ClaudeSonnet)
	assert.NotEmpty(t, ClaudeHaiku)

	assert.NotEmpty(t, bedrock.ModelOpusLatest)
	assert.NotEmpty(t, bedrock.ModelSonnetLatest)
	assert.NotEmpty(t, bedrock.ModelHaikuLatest)
}
