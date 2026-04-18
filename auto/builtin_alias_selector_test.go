package auto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/codex"
	"github.com/codewandler/llm/provider/minimax"
	"github.com/codewandler/llm/provider/openai"
)

func TestResolveBuiltinAliasModels_AnthropicCatalogBacked(t *testing.T) {
	models, ok := resolveBuiltinAliasModels(ProviderAnthropic)
	require.True(t, ok)
	assert.Equal(t, anthropic.ModelHaiku, models.fast)
	assert.Equal(t, anthropic.ModelSonnet, models.normal)
	assert.Equal(t, anthropic.ModelOpus, models.powerful)
}

func TestResolveBuiltinAliasModels_OpenAICatalogBacked(t *testing.T) {
	models, ok := resolveBuiltinAliasModels(ProviderOpenAI)
	require.True(t, ok)
	assert.Equal(t, openai.ModelGPT54Mini, models.fast)
	assert.Equal(t, openai.ModelGPT54, models.normal)
	assert.Equal(t, openai.ModelGPT54Pro, models.powerful)
}

func TestResolveBuiltinAliasModels_CodexFallback(t *testing.T) {
	models, ok := resolveBuiltinAliasModels(ProviderCodex)
	require.True(t, ok)
	assert.Equal(t, codex.FastModelID(), models.fast)
	assert.Equal(t, codex.DefaultModelID(), models.normal)
	assert.Equal(t, codex.PowerfulModelID(), models.powerful)
}

func TestResolveBuiltinAliasModels_MinimaxFallsBack(t *testing.T) {
	models, ok := resolveBuiltinAliasModels(ProviderMiniMax)
	require.True(t, ok)
	assert.Equal(t, minimax.ModelM27, models.fast)
	assert.Equal(t, minimax.ModelM27, models.normal)
	assert.Equal(t, minimax.ModelM27, models.powerful)
}
