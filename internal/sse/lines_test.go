package sse

import (
	"context"
	"fmt"
	"io"
	"strings"
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
