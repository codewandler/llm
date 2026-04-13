package sse

import (
	"bufio"
	"context"
	"io"
	"strings"
	"sync"
)

// Event represents one SSE payload with its optional event name.
type Event struct {
	Name string
	Data string
}

// scanResult carries one line from the background scanner goroutine.
type scanResult struct {
	line string
	err  error // non-nil on scanner error; io.EOF signals clean end
}

// ForEachDataLine scans an SSE stream and invokes fn for each data line.
//
// It supports both plain `data: ...` streams and named events using
// `event: ...` followed by `data: ...`.
//
// For closable readers, cancellation and early termination close the reader to
// unblock any in-flight Read and wait for the scanner goroutine to exit.
func ForEachDataLine(ctx context.Context, r io.Reader, fn func(Event) bool) error {
	lines := make(chan scanResult, 16)
	scannerDone := make(chan struct{})

	var closeReader func()
	if closer, ok := r.(io.Closer); ok {
		var once sync.Once
		closeReader = func() {
			once.Do(func() {
				_ = closer.Close()
			})
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

	var pendingEvent string
	stop := func(err error) error {
		closeReader()
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
				// channel closed — scanner finished cleanly
				return nil
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
				pendingEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data := strings.TrimPrefix(line, "data:")
				data = strings.TrimPrefix(data, " ")
				if !fn(Event{Name: pendingEvent, Data: data}) {
					return stop(nil)
				}
				pendingEvent = ""
			}
		}
	}
}
