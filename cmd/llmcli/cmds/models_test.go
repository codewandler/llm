package cmds

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestPrintAliasEntry_SortsTargetsDeterministically(t *testing.T) {
	out := captureStdout(t, func() {
		printAliasEntry("fast", []string{"z-model", "a-model", "m-model"})
	})

	require.Equal(t, "  fast:\n    a-model\n    m-model\n    z-model\n", out)
}

func TestFilterModels_MatchesByAliasCaseInsensitive(t *testing.T) {
	models := []llm.Model{
		{ID: "provider/a", Name: "Alpha", Provider: "anthropic", Aliases: []string{"FAST"}},
		{ID: "provider/b", Name: "Beta", Provider: "openai", Aliases: []string{"slow"}},
	}

	got := filterModels(models, modelsOptions{filter: "fast"})
	require.Len(t, got, 1)
	require.Equal(t, "provider/a", got[0].ID)
}

func TestFilterModels_MatchesByProviderAndAlias(t *testing.T) {
	models := []llm.Model{
		{ID: "claude-sonnet", Name: "Claude Sonnet", Provider: "work/claude", Aliases: []string{"sonnet", "work/claude/sonnet"}},
		{ID: "gpt-5", Name: "GPT-5", Provider: "openai", Aliases: []string{"flagship"}},
	}

	got := filterModels(models, modelsOptions{provider: "claude", alias: "sonnet"})
	require.Len(t, got, 1)
	require.Equal(t, "claude-sonnet", got[0].ID)
}

func TestBuildAliasMap_HidesSyntheticAliasesByDefault(t *testing.T) {
	models := []llm.Model{{ID: "claude-sonnet", Aliases: []string{"sonnet", "claude/sonnet", "work/claude/sonnet"}}}

	got := buildAliasMap(models, false)
	require.Contains(t, got, "sonnet")
	require.NotContains(t, got, "claude/sonnet")
	require.NotContains(t, got, "work/claude/sonnet")
}

func TestPrintModelsSection_GroupsByProviderAndShowsAliasesWhenFiltered(t *testing.T) {
	models := []llm.Model{
		{ID: "gpt-5", Name: "GPT-5", Provider: "openai", Aliases: []string{"default", "flagship", "openai/flagship"}},
		{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Provider: "anthropic", Aliases: []string{"sonnet"}},
	}

	out := captureStdout(t, func() {
		printModelsSection(models, modelsOptions{filter: "gpt"})
	})

	require.Contains(t, out, "MODELS\n")
	require.Contains(t, out, "  anthropic (1)\n")
	require.Contains(t, out, "  openai (1)\n")
	require.Contains(t, out, "overlay aliases: default")
	require.Contains(t, out, "aliases: flagship")
	require.NotContains(t, out, "openai/flagship")
}

func TestSplitAliases_ClassifiesFriendlyAndSynthetic(t *testing.T) {
	got := splitAliases([]string{"default", "flagship", "openai/flagship", "work/openai/flagship", "flagship"})
	require.Equal(t, []string{"default"}, got.Overlay)
	require.Equal(t, []string{"flagship"}, got.Friendly)
	require.Equal(t, []string{"openai/flagship", "work/openai/flagship"}, got.Synthetic)
}

func TestSplitAliases_TreatsCodexAsFriendlyNotOverlay(t *testing.T) {
	got := splitAliases([]string{"codex", "openai/codex"})
	require.Nil(t, got.Overlay)
	require.Equal(t, []string{"codex"}, got.Friendly)
	require.Equal(t, []string{"openai/codex"}, got.Synthetic)
}

func TestPrintModelsJSON_HidesSyntheticAliasesByDefault(t *testing.T) {
	models := []llm.Model{{
		ID:       "gpt-5",
		Name:     "GPT-5",
		Provider: "openai",
		Aliases:  []string{"flagship", "openai/flagship"},
	}}

	out := captureStdout(t, func() {
		require.NoError(t, printModelsJSON(models, modelsOptions{}))
	})

	var got []modelRecord
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Aliases)
	require.Nil(t, got[0].Aliases.Overlay)
	require.Equal(t, []string{"flagship"}, got[0].Aliases.Friendly)
	require.Nil(t, got[0].Aliases.Synthetic)
}

func TestPrintModelsJSON_IncludesSyntheticAliasesWhenRequested(t *testing.T) {
	models := []llm.Model{{
		ID:       "gpt-5",
		Name:     "GPT-5",
		Provider: "openai",
		Aliases:  []string{"flagship", "openai/flagship"},
	}}

	out := captureStdout(t, func() {
		require.NoError(t, printModelsJSON(models, modelsOptions{allAliases: true}))
	})

	var got []modelRecord
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Aliases)
	require.Nil(t, got[0].Aliases.Overlay)
	require.Equal(t, []string{"flagship"}, got[0].Aliases.Friendly)
	require.Equal(t, []string{"openai/flagship"}, got[0].Aliases.Synthetic)
}

func TestPrintModelsJSON_SeparatesOverlayAliases(t *testing.T) {
	models := []llm.Model{{
		ID:       "claude-sonnet-4-6",
		Name:     "Claude Sonnet 4.6",
		Provider: "anthropic",
		Aliases:  []string{"default", "fast", "sonnet"},
	}}

	out := captureStdout(t, func() {
		require.NoError(t, printModelsJSON(models, modelsOptions{}))
	})

	var got []modelRecord
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Aliases)
	require.Equal(t, []string{"default", "fast"}, got[0].Aliases.Overlay)
	require.Equal(t, []string{"sonnet"}, got[0].Aliases.Friendly)
}

func TestPrintModelsJSON_OmitsAliasesWhenEmpty(t *testing.T) {
	models := []llm.Model{{ID: "plain", Name: "Plain", Provider: "local"}}

	out := captureStdout(t, func() {
		require.NoError(t, printModelsJSON(models, modelsOptions{}))
	})

	var got []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got, 1)
	_, hasAliases := got[0]["aliases"]
	require.False(t, hasAliases)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	fn()

	require.NoError(t, w.Close())
	os.Stdout = orig

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	return buf.String()
}
