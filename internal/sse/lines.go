package sse

import (
	"bufio"
	"context"
	"io"
	"strings"
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
// Context cancellation is honoured during blocking reads: if ctx is cancelled
// while waiting for the next line, ForEachDataLine returns ctx.Err() promptly.
func ForEachDataLine(ctx context.Context, r io.Reader, fn func(Event) bool) error {
	lines := make(chan scanResult, 16)

	go func() {
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

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case res, ok := <-lines:
			if !ok {
				// channel closed — scanner finished cleanly
				return nil
			}
			if res.err != nil {
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
					return nil
				}
				pendingEvent = ""
			}
		}
	}
}
