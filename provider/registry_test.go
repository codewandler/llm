package provider

import (
	"context"
	"os"
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDefault(t *testing.T) {
	t.Parallel()

	// Clear env vars to test defaults
	os.Unsetenv("OPENROUTER_API_KEY")
	os.Unsetenv("OLLAMA_BASE_URL")

	reg := NewDefaultRegistry()
	require.NotNil(t, reg)

	// Should have Claude Code and Ollama providers (no OpenRouter without API key)
	ccProvider, err := reg.Provider("anthropic:claude-code")
	require.NoError(t, err)
	assert.NotNil(t, ccProvider)

	ollamaProvider, err := reg.Provider("ollama")
	require.NoError(t, err)
	assert.NotNil(t, ollamaProvider)

	// OpenRouter should not be registered without API key
	_, err = reg.Provider("openrouter")
	assert.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrNotFound)
}

func TestNewDefaultWithOpenRouter(t *testing.T) {
	t.Parallel()

	// Set OpenRouter API key
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer os.Unsetenv("OPENROUTER_API_KEY")

	reg := NewDefaultRegistry()
	require.NotNil(t, reg)

	// Should have all three providers
	ccProvider, err := reg.Provider("anthropic:claude-code")
	require.NoError(t, err)
	assert.NotNil(t, ccProvider)

	ollamaProvider, err := reg.Provider("ollama")
	require.NoError(t, err)
	assert.NotNil(t, ollamaProvider)

	orProvider, err := reg.Provider("openrouter")
	require.NoError(t, err)
	assert.NotNil(t, orProvider)
}

func TestNewDefaultWithCustomOllamaURL(t *testing.T) {
	t.Parallel()

	customURL := "http://custom:11434"
	os.Setenv("OLLAMA_BASE_URL", customURL)
	defer os.Unsetenv("OLLAMA_BASE_URL")

	reg := NewDefaultRegistry()
	require.NotNil(t, reg)

	ollamaProvider, err := reg.Provider("ollama")
	require.NoError(t, err)
	assert.NotNil(t, ollamaProvider)
}

func TestResolveModel(t *testing.T) {
	t.Parallel()

	reg := NewDefaultRegistry()

	tests := []struct {
		name      string
		modelRef  string
		wantProv  string
		wantModel string
		wantErr   bool
	}{
		{
			name:      "claude code model",
			modelRef:  "anthropic:claude-code/sonnet",
			wantProv:  "anthropic:claude-code",
			wantModel: "sonnet",
		},
		{
			name:      "ollama model",
			modelRef:  "ollama/llama3.2:1b",
			wantProv:  "ollama",
			wantModel: "llama3.2:1b",
		},
		{
			name:     "invalid format",
			modelRef: "invalid",
			wantErr:  true,
		},
		{
			name:     "unknown provider",
			modelRef: "unknown/model",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov, modelID, err := reg.ResolveModel(tt.modelRef)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantProv, prov.Name())
			assert.Equal(t, tt.wantModel, modelID)
		})
	}
}

func TestAllModels(t *testing.T) {
	t.Parallel()

	reg := NewDefaultRegistry()
	models := reg.AllModels()

	// Should have models from Claude Code (3) and Ollama (11)
	assert.NotEmpty(t, models)
	assert.GreaterOrEqual(t, len(models), 14)

	// Check that models have correct provider names
	var hasClaude, hasOllama bool
	for _, m := range models {
		if m.Provider == "anthropic:claude-code" {
			hasClaude = true
		}
		if m.Provider == "ollama" {
			hasOllama = true
		}
	}
	assert.True(t, hasClaude)
	assert.True(t, hasOllama)
}

func TestSendMessage(t *testing.T) {
	t.Parallel()

	reg := NewDefaultRegistry()

	// Test that SendMessage correctly resolves model reference
	// We won't actually send a message, just verify the resolution works
	ctx := context.Background()
	opts := llm.SendOptions{
		Model: "anthropic:claude-code/sonnet",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "test"},
		},
	}

	// This will fail because claude CLI likely isn't authenticated in CI,
	// but we can verify the model reference resolution works by checking the error
	_, err := reg.SendMessage(ctx, opts)
	// Either succeeds (if claude is set up) or fails with claude-specific error (not resolution error)
	if err != nil {
		assert.NotErrorIs(t, err, llm.ErrNotFound, "should not fail with NotFound - provider resolved correctly")
		assert.NotErrorIs(t, err, llm.ErrBadRequest, "should not fail with BadRequest - model format correct")
	}
}
