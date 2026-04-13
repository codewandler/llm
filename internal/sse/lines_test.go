package sse

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForEachDataLine_Normal(t *testing.T) {
	body := strings.NewReader(strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start"}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","delta":{"text":"hello"}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n"))

	var events []Event
	err := ForEachDataLine(context.Background(), body, func(ev Event) bool {
		events = append(events, ev)
		return true
	})
	require.NoError(t, err)
	require.Len(t, events, 3)
	assert.Equal(t, "message_start", events[0].Name)
	assert.Contains(t, events[0].Data, "message_start")
	assert.Equal(t, "message_stop", events[2].Name)
}

func TestForEachDataLine_FnReturnsFalse(t *testing.T) {
	body := strings.NewReader(strings.Join([]string{
		`data: first`,
		"",
		`data: second`,
		"",
	}, "\n"))

	var seen []string
	err := ForEachDataLine(context.Background(), body, func(ev Event) bool {
		seen = append(seen, ev.Data)
		return false // stop after first
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"first"}, seen)
}

func TestForEachDataLine_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() }) // nolint:errcheck

	done := make(chan error, 1)
	go func() {
		done <- ForEachDataLine(ctx, pr, func(Event) bool { return true })
	}()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("ForEachDataLine did not return within 200ms after context cancellation")
	}
}

func TestForEachDataLine_ContextCancelledMidStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() }) // nolint:errcheck

	// Write one event then block.
	go func() {
		_, _ = fmt.Fprintln(pw, "data: first")
		_, _ = fmt.Fprintln(pw, "")
		// Cancel after the first event is written, then block indefinitely.
		cancel()
		// Keep pipe open so scanner blocks on next read.
		select {}
	}()

	var seen []string
	done := make(chan error, 1)
	go func() {
		done <- ForEachDataLine(ctx, pr, func(ev Event) bool {
			seen = append(seen, ev.Data)
			return true
		})
	}()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, []string{"first"}, seen)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ForEachDataLine did not return within 500ms after mid-stream cancellation")
	}
}

func TestForEachDataLine_ReadError(t *testing.T) {
	err := ForEachDataLine(context.Background(), io.NopCloser(errReader{}), func(Event) bool { return true })
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

// errReader returns an error on every read.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestForEachDataLine_CancellationClosesReadCloser(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	body := newBlockingReadCloser()

	done := make(chan error, 1)
	go func() {
		done <- ForEachDataLine(ctx, body, func(Event) bool { return true })
	}()

	select {
	case <-body.readStarted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for read to start")
	}

	cancel()

	select {
	case <-body.closeCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for Close after cancellation")
	}

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ForEachDataLine did not return after cancellation")
	}
}

func TestForEachDataLine_FnReturnsFalseClosesReadCloser(t *testing.T) {
	body := newStreamingReadCloser([]byte("data: first\n\n"))

	done := make(chan error, 1)
	go func() {
		done <- ForEachDataLine(context.Background(), body, func(Event) bool { return false })
	}()

	select {
	case <-body.readStarted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for read to start")
	}

	select {
	case <-body.closeCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for Close after callback stop")
	}

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("ForEachDataLine did not return after callback stop")
	}
}

// blockingReadCloser blocks Read until Close is called.
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

// streamingReadCloser returns one SSE payload, then blocks until Close is called.
type streamingReadCloser struct {
	payload          []byte
	readStarted      chan struct{}
	closeCalled      chan struct{}
	startedOnce      sync.Once
	closeOnce        sync.Once
	payloadDelivered atomic.Bool
	reading          atomic.Bool
	unblockRead      chan struct{}
}

func newStreamingReadCloser(payload []byte) *streamingReadCloser {
	return &streamingReadCloser{
		payload:     payload,
		readStarted: make(chan struct{}),
		closeCalled: make(chan struct{}),
		unblockRead: make(chan struct{}),
	}
}

func (s *streamingReadCloser) Read(p []byte) (int, error) {
	s.startedOnce.Do(func() { close(s.readStarted) })
	if !s.payloadDelivered.Swap(true) {
		return copy(p, s.payload), nil
	}
	s.reading.Store(true)
	<-s.unblockRead
	s.reading.Store(false)
	return 0, io.EOF
}

func (s *streamingReadCloser) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeCalled)
		close(s.unblockRead)
	})
	return nil
}
