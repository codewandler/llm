package codex

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchModels(t *testing.T) {
	if !isCodexAvailable() {
		t.Skip("skipping: no local auth (~/.codex/auth.json)")
	}
	auth, err := LoadAuth()
	require.NoError(t, err)
	p := New(auth)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	models, err := p.FetchModels(ctx)
	require.NoError(t, err, "FetchModels() must not return an error")
	require.NotEmpty(t, models, "FetchModels() must return at least one model")

	for _, m := range models {
		assert.NotEmpty(t, m.ID, "every model must have a non-empty ID")
		assert.NotEmpty(t, m.Name, "every model must have a non-empty Name")
		assert.Equal(t, p.Name(), m.Provider, "every model must carry the provider name")
		t.Logf("model: %s (%s)", m.ID, m.Name)
	}
}
