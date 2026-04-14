package auto

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/provider/anthropic"
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

func TestDetectProviders_DoesNotAutoDetectCodexLocal(t *testing.T) {
	// Create a synthetic ~/.codex/auth.json that would previously trigger
	// automatic Codex provider registration.
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".codex", "auth.json"),
		[]byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"synthetic-access-token","account_id":"test-account"}}`),
		0o600,
	))

	t.Setenv("HOME", home)
	t.Setenv(EnvOpenAIKey, "test-openai-key")

	providers := detectProviders(nil, nil, map[string]bool{
		ProviderClaude:     true,
		ProviderAnthropic:  true,
		ProviderBedrock:    true,
		ProviderOpenRouter: true,
		ProviderMiniMax:    true,
	})

	// Only the regular openai provider should be detected; codex is opt-in only.
	require.Len(t, providers, 1)
	assert.Equal(t, ProviderOpenAI, providers[0].name)
	assert.Equal(t, ProviderOpenAI, providers[0].providerType)
}

func TestWithCodexLocal_UsesChatGPTPrefix(t *testing.T) {
	// Write a synthetic ~/.codex/auth.json in a temp HOME.
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".codex", "auth.json"),
		[]byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"synthetic-access-token","account_id":"test-account"}}`),
		0o600,
	))
	t.Setenv("HOME", home)

	ctx := context.Background()
	p, err := New(ctx,
		WithoutAutoDetect(),
		WithCodexLocal(),
	)
	require.NoError(t, err)
	require.NotNil(t, p)

	// All models should be under the "chatgpt" prefix, NOT "openai".
	models := p.Models()
	require.NotEmpty(t, models)
	for _, m := range models {
		assert.Contains(t, m.ID, "chatgpt/", "model %q should have chatgpt/ prefix, got %q", m.Name, m.ID)
		assert.NotContains(t, m.ID, "openai/", "model %q must not have openai/ prefix", m.Name)
	}

	// Only Codex-category models should be present.
	for _, m := range models {
		assert.Contains(t, m.Name, "Codex", "non-Codex model should not appear in chatgpt provider: %q", m.Name)
	}
}

func TestBuildAliasTargets(t *testing.T) {
	tests := []struct {
		name         string
		instanceName string
		providerType string
		wantAliases  []string
		wantCodex    bool
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
			name:         "openai provider",
			instanceName: "openai",
			providerType: ProviderOpenAI,
			wantAliases:  []string{AliasFast, AliasDefault, AliasPowerful},
			wantCodex:    false,
		},
		{
			name:         "chatgpt provider wires codex alias",
			instanceName: "chatgpt",
			providerType: ProviderChatGPT,
			wantAliases:  []string{AliasFast, AliasDefault, AliasPowerful, AliasCodex},
			wantCodex:    true,
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
			if !tt.wantCodex {
				assert.NotContains(t, targets, AliasCodex)
			}
			for _, alias := range tt.wantAliases {
				_, ok := targets[alias]
				assert.True(t, ok, "missing alias %s", alias)
			}
		})
	}
}

func TestModelAliasesForProvider(t *testing.T) {
	// Claude should have model aliases (from anthropic package)
	claudeAliases := modelAliasesForProvider(ProviderClaude)
	require.NotNil(t, claudeAliases)
	assert.Equal(t, anthropic.ModelSonnet, claudeAliases["sonnet"])
	assert.Equal(t, anthropic.ModelOpus, claudeAliases["opus"])
	assert.Equal(t, anthropic.ModelHaiku, claudeAliases["haiku"])

	// Anthropic should have same aliases
	anthropicAliases := modelAliasesForProvider(ProviderAnthropic)
	require.NotNil(t, anthropicAliases)
	assert.Equal(t, anthropic.ModelSonnet, anthropicAliases["sonnet"])

	// OpenAI should have model aliases (full GPT + o-series set)
	openaiAliases := modelAliasesForProvider(ProviderOpenAI)
	require.NotNil(t, openaiAliases)
	assert.Equal(t, "gpt-5.4", openaiAliases["flagship"])
	assert.Equal(t, "gpt-5.4-mini", openaiAliases["mini"])
	assert.Equal(t, "gpt-5.4-nano", openaiAliases["nano"])
	assert.Equal(t, "gpt-5.4-pro", openaiAliases["pro"])
	assert.Equal(t, "gpt-5.3-codex", openaiAliases["codex"])
	assert.Equal(t, "o4-mini", openaiAliases["o4"])
	assert.Equal(t, "o3", openaiAliases["o3"])

	// ChatGPT provider should have only codex aliases
	chatgptAliases := modelAliasesForProvider(ProviderChatGPT)
	require.NotNil(t, chatgptAliases)
	assert.Equal(t, "gpt-5.3-codex", chatgptAliases["codex"], "chatgpt/codex -> gpt-5.3-codex")
	assert.Equal(t, "gpt-5.1-codex-mini", chatgptAliases["mini"], "chatgpt/mini -> gpt-5.1-codex-mini")
	// Should NOT have general-purpose GPT or o-series aliases
	_, hasFlagship := chatgptAliases["flagship"]
	assert.False(t, hasFlagship, "chatgpt aliases must not include flagship (non-codex model)")
}

func TestConstants(t *testing.T) {
	// Verify constants are not empty
	assert.NotEmpty(t, ProviderClaude)
	assert.NotEmpty(t, ProviderBedrock)
	assert.NotEmpty(t, ProviderChatGPT)
	assert.NotEmpty(t, ProviderOpenAI)
	assert.NotEmpty(t, ProviderOpenRouter)
	assert.NotEmpty(t, ProviderAnthropic)

	// ChatGPT and OpenAI must be distinct to avoid routing clashes
	assert.NotEqual(t, ProviderChatGPT, ProviderOpenAI)

	assert.NotEmpty(t, EnvOpenAIKey)
	assert.NotEmpty(t, EnvOpenRouterKey)
	assert.NotEmpty(t, EnvAnthropicKey)

	assert.NotEmpty(t, AliasFast)
	assert.NotEmpty(t, AliasDefault)
	assert.NotEmpty(t, AliasPowerful)
	assert.NotEmpty(t, AliasCodex)

	// Model constants are now in provider packages
	assert.NotEmpty(t, anthropic.ModelOpus)
	assert.NotEmpty(t, anthropic.ModelSonnet)
	assert.NotEmpty(t, anthropic.ModelHaiku)

	assert.NotEmpty(t, bedrock.ModelOpusLatest)
	assert.NotEmpty(t, bedrock.ModelSonnetLatest)
	assert.NotEmpty(t, bedrock.ModelHaikuLatest)
}
