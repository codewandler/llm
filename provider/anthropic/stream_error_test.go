package anthropic

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestParseStream_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before ParseStream is called

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() }) // nolint:errcheck // prevent goroutine leak if test fails

	ch := ParseStream(ctx, pr, ParseOpts{
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

func TestParseStream_CancellationClosesBodyToUnblockRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	body := newBlockingReadCloser()
	ch := ParseStream(ctx, body, ParseOpts{Model: "claude-sonnet-4-5"})

	select {
	case <-body.readStarted:
		// scanner goroutine is currently blocked in Read
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for stream reader to start")
	}

	cancel()

	select {
	case <-body.closeCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for body close after cancellation")
	}

	for range ch {
		// drain stream until ParseStream exits
	}

	require.True(t, body.closedDuringRead.Load(), "expected cancellation to close the body and unblock Read")
}

// blockingReadCloser blocks Read until Close is called.
// It records whether Close happened while Read was active.
type blockingReadCloser struct {
	readStarted      chan struct{}
	unblockRead      chan struct{}
	closeCalled      chan struct{}
	startedOnce      sync.Once
	closeOnce        sync.Once
	closedDuringRead atomic.Bool
	reading          atomic.Bool
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{
		readStarted: make(chan struct{}),
		unblockRead: make(chan struct{}),
		closeCalled: make(chan struct{}),
	}
}

func (b *blockingReadCloser) Read(_ []byte) (int, error) {
	b.reading.Store(true)
	b.startedOnce.Do(func() { close(b.readStarted) })
	<-b.unblockRead
	b.reading.Store(false)
	return 0, io.EOF
}

func (b *blockingReadCloser) Close() error {
	b.closeOnce.Do(func() {
		if b.reading.Load() {
			b.closedDuringRead.Store(true)
		}
		close(b.closeCalled)
		b.startedOnce.Do(func() { close(b.readStarted) })
		close(b.unblockRead)
	})
	return nil
}

// failReader is an io.Reader that always returns an error.
type failReader struct{}

func (failReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
