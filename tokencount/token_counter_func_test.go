package tokencount

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenCounterFunc(t *testing.T) {
	t.Parallel()

	called := false
	fn := TokenCounterFunc(func(ctx context.Context, req TokenCountRequest) (*TokenCount, error) {
		called = true
		if req.Model != "test" {
			return nil, errors.New("unexpected model")
		}
		return &TokenCount{InputTokens: 5}, nil
	})

	count, err := fn.CountTokens(context.Background(), TokenCountRequest{Model: "test"})
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, 5, count.InputTokens)
}
