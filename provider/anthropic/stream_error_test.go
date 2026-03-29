package anthropic

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestParseStream_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before ParseStream is called

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() }) // nolint:errcheck // prevent goroutine leak if test fails

	ch := ParseStream(ctx, io.NopCloser(pr), ParseOpts{
		Model: "claude-sonnet-4-5",
	})

	var errEnv *llm.Envelope
	for env := range ch {
		if env.Type == llm.StreamEventError {
			e := env
			errEnv = &e
		}
	}

	require.NotNil(t, errEnv, "expected a StreamEventError envelope")
	errEvt, ok := errEnv.Data.(*llm.ErrorEvent)
	require.True(t, ok, "Data should be *llm.ErrorEvent")
	var pe *llm.ProviderError
	require.ErrorAs(t, errEvt.Error, &pe)
	assert.True(t,
		errors.Is(pe.Sentinel, llm.ErrContextCancelled) || errors.Is(pe.Cause, context.Canceled),
		"error should reflect context cancellation, got: %v", pe,
	)
}

func TestParseStream_ReadError(t *testing.T) {
	ch := ParseStream(context.Background(), io.NopCloser(failReader{}), ParseOpts{
		Model: "claude-sonnet-4-5",
	})

	var errEnv *llm.Envelope
	for env := range ch {
		if env.Type == llm.StreamEventError {
			e := env
			errEnv = &e
		}
	}

	require.NotNil(t, errEnv, "expected a StreamEventError envelope")
	errEvt, ok := errEnv.Data.(*llm.ErrorEvent)
	require.True(t, ok, "Data should be *llm.ErrorEvent")
	var pe *llm.ProviderError
	require.ErrorAs(t, errEvt.Error, &pe)
	assert.ErrorIs(t, pe.Sentinel, llm.ErrStreamRead)
}

// failReader is an io.Reader that always returns an error.
type failReader struct{}

func (failReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
