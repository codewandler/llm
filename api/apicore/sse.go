// api/apicore/sse.go
package apicore

import (
	"bufio"
	"context"
	"io"
	"strings"
	"sync"
)

// sseEvent is one SSE payload with its optional event name.
type sseEvent struct {
	name string
	data string
}

// scanResult carries one raw line from the background scanner goroutine.
type scanResult struct {
	line string
	err  error // non-nil on scanner error
}

// forEachDataLine scans an SSE stream and invokes fn for each data event.
//
// It supports both plain `data: ...` streams and named events using
// `event: ...` followed by `data: ...`.
//
// For closable readers, context cancellation and early termination close the
// reader to unblock any in-flight Read and wait for the scanner goroutine to exit.
func forEachDataLine(ctx context.Context, r io.Reader, fn func(sseEvent) bool) error {
	lines := make(chan scanResult, 16)
	scannerDone := make(chan struct{})

	var closeReader func()
	if closer, ok := r.(io.Closer); ok {
		var once sync.Once
		closeReader = func() {
			once.Do(func() { _ = closer.Close() })
		}
	} else {
		closeReader = func() {}
	}

	go func() {
		defer close(scannerDone)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			lines <- scanResult{line: scanner.Text()}
		}
		if err := scanner.Err(); err != nil {
			lines <- scanResult{err: err}
		}
		close(lines)
	}()

	var pendingName string
	stop := func(err error) error {
		closeReader()
		// Drain any buffered lines so the scanner goroutine is never stuck on a
		// channel send after we've stopped reading. Without this, a full buffer
		// combined with closeReader() unblocking scanner.Scan() could deadlock:
		// the goroutine tries to send its next line, nobody reads, scannerDone
		// is never closed, and <-scannerDone blocks forever.
		for range lines {
		}
		<-scannerDone
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return stop(ctx.Err())
		case res, ok := <-lines:
			if !ok {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return nil // scanner finished cleanly; scannerDone will close via defer
			}
			if res.err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return res.err
			}
			line := res.line
			switch {
			case strings.HasPrefix(line, "event:"):
				pendingName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data := strings.TrimPrefix(line, "data:")
				data = strings.TrimPrefix(data, " ")
				if !fn(sseEvent{name: pendingName, data: data}) {
					return stop(nil)
				}
				pendingName = ""
			}
		}
	}
}
