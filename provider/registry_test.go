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
	// NOTE: Cannot use t.Parallel() - these tests modify shared environment variables

	// Clear env vars to test defaults
	os.Unsetenv("OPENROUTER_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("OPENAI_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("OLLAMA_BASE_URL")

	reg := NewDefaultRegistry()
	require.NotNil(t, reg)

	// Should have Ollama provider (no API key providers without API keys)
	ollamaProvider, err := reg.Provider("ollama")
	require.NoError(t, err)
	assert.NotNil(t, ollamaProvider)

	// Anthropic should not be registered without API key
	_, err = reg.Provider("anthropic")
	assert.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrNotFound)

	// OpenRouter should not be registered without API key
	_, err = reg.Provider("openrouter")
	assert.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrNotFound)
}

func TestNewDefaultWithAnthropicAPIKey(t *testing.T) {
	// NOTE: Cannot use t.Parallel() - these tests modify shared environment variables

	// Set Anthropic API key
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	reg := NewDefaultRegistry()
	require.NotNil(t, reg)

	// Should have Anthropic provider
	anthropicProvider, err := reg.Provider("anthropic")
	require.NoError(t, err)
	assert.NotNil(t, anthropicProvider)
}

func TestNewDefaultWithOpenRouter(t *testing.T) {
	// NOTE: Cannot use t.Parallel() - these tests modify shared environment variables

	// Set OpenRouter API key
	os.Setenv("OPENROUTER_API_KEY", "test-key")
	defer os.Unsetenv("OPENROUTER_API_KEY")

	reg := NewDefaultRegistry()
	require.NotNil(t, reg)

	// Should have OpenRouter provider
	orProvider, err := reg.Provider("openrouter")
	require.NoError(t, err)
	assert.NotNil(t, orProvider)

	ollamaProvider, err := reg.Provider("ollama")
	require.NoError(t, err)
	assert.NotNil(t, ollamaProvider)
}

func TestNewDefaultWithCustomOllamaURL(t *testing.T) {
	// NOTE: Cannot use t.Parallel() - these tests modify shared environment variables

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
	reg := NewDefaultRegistry()

	tests := []struct {
		name      string
		modelRef  string
		wantProv  string
		wantModel string
		wantErr   bool
	}{
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
	reg := NewDefaultRegistry()
	models := reg.AllModels()

	// Should have models from Ollama (11+)
	assert.NotEmpty(t, models)
	assert.GreaterOrEqual(t, len(models), 11)

	// Check that models have correct provider names
	var hasOllama bool
	for _, m := range models {
		if m.Provider == "ollama" {
			hasOllama = true
		}
	}
	assert.True(t, hasOllama)
}

func TestCreateStream(t *testing.T) {
	reg := NewDefaultRegistry()

	// Test that CreateStream correctly resolves model reference
	// We won't actually send a message, just verify the resolution works
	ctx := context.Background()
	opts := llm.StreamOptions{
		Model: "ollama/llama3.2:1b",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "test"},
		},
	}

	// This will fail because ollama likely isn't running in CI,
	// but we can verify the model reference resolution works by checking the error
	_, err := reg.CreateStream(ctx, opts)
	// Either succeeds (if ollama is set up) or fails with ollama-specific error (not resolution error)
	if err != nil {
		assert.NotErrorIs(t, err, llm.ErrNotFound, "should not fail with NotFound - provider resolved correctly")
		assert.NotErrorIs(t, err, llm.ErrBadRequest, "should not fail with BadRequest - model format correct")
	}
}
