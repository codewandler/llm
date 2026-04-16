package cmds

import (
	"bytes"
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
		{ID: "gpt-5", Name: "GPT-5", Provider: "openai", Aliases: []string{"flagship", "openai/flagship"}},
		{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Provider: "anthropic", Aliases: []string{"sonnet"}},
	}

	out := captureStdout(t, func() {
		printModelsSection(models, modelsOptions{filter: "gpt"})
	})

	require.Contains(t, out, "MODELS\n")
	require.Contains(t, out, "  anthropic (1)\n")
	require.Contains(t, out, "  openai (1)\n")
	require.Contains(t, out, "aliases: flagship")
	require.NotContains(t, out, "openai/flagship")
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
