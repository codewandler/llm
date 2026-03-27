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
		{ID: "provider/a", Name: "Alpha", Aliases: []string{"FAST"}},
		{ID: "provider/b", Name: "Beta", Aliases: []string{"slow"}},
	}

	got := filterModels(models, "fast")
	require.Len(t, got, 1)
	require.Equal(t, "provider/a", got[0].ID)
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
