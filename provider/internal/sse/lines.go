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

// ForEachDataLine scans an SSE stream and invokes fn for each data line.
//
// It supports both plain `data: ...` streams and named events using
// `event: ...` followed by `data: ...`.
func ForEachDataLine(ctx context.Context, r io.Reader, fn func(Event) bool) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var pendingEvent string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			pendingEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(data, " ") {
				data = data[1:]
			}
			if !fn(Event{Name: pendingEvent, Data: data}) {
				return nil
			}
			pendingEvent = ""
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
