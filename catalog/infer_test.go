package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInferAnthropicModelKey(t *testing.T) {
	key, ok := inferAnthropicModelKey("claude-sonnet-4-5-20250929")
	require.True(t, ok)
	assert.Equal(t, NormalizeKey(ModelKey{
		Creator:     "anthropic",
		Family:      "claude",
		Series:      "sonnet",
		Version:     "4.5",
		ReleaseDate: "2025-09-29",
	}), key)
}

func TestInferOpenAIModelKey(t *testing.T) {
	key, ok := inferOpenAIModelKey("gpt-5.4-mini")
	require.True(t, ok)
	assert.Equal(t, NormalizeKey(ModelKey{
		Creator: "openai",
		Family:  "gpt",
		Version: "5.4",
		Variant: "mini",
	}), key)
}

func TestInferOpenRouterModelKey(t *testing.T) {
	key, ok := inferOpenRouterModelKey("anthropic/claude-opus-4-6:free")
	require.True(t, ok)
	assert.Equal(t, NormalizeKey(ModelKey{
		Creator: "anthropic",
		Family:  "claude",
		Series:  "opus",
		Version: "4.6",
	}), key)
}
