package anthropic

import (
	"context"
	"io"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/internal/sse"
)

// StreamMeta carries provider-level metadata forwarded into ParseStream.
type StreamMeta struct {
	RequestedModel string
	ResolvedModel  string
	StartTime      time.Time
}

// ParseStream reads an Anthropic-format SSE response body and publishes
// structured events to pub. It blocks until the stream ends or ctx is done.
func ParseStream(ctx context.Context, body io.ReadCloser, pub llm.Publisher, meta StreamMeta) {
	defer pub.Close()
	defer body.Close()

	proc := newStreamProcessor(meta, pub)
	err := sse.ForEachDataLine(ctx, body, func(ev sse.Event) bool {
		return proc.dispatch(ev.Data)
	})
	if err != nil {
		if ctx.Err() != nil {
			pub.Error(llm.NewErrContextCancelled(llm.ProviderNameAnthropic, err))
			return
		}
		pub.Error(llm.NewErrStreamRead(llm.ProviderNameAnthropic, err))
	}
}
