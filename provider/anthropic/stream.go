package anthropic

import (
	"context"
	"io"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/internal/sse"
)

// CostFn calculates token costs for a given model and populates the cost
// fields on usage. Providers that reuse ParseStream with different pricing
// (e.g. MiniMax) supply their own CostFn instead of wrapping the publisher.
type CostFn func(model string, usage *llm.Usage)

// ParseOpts configures how ParseStream processes an Anthropic-format SSE body.
type ParseOpts struct {
	RequestedModel string
	ResolvedModel  string

	// CostFn overrides the default Anthropic cost calculation.
	// When nil, FillCost (Anthropic pricing) is used.
	CostFn CostFn
}

// ParseStream reads an Anthropic-format SSE response body in a background
// goroutine and returns a stream of structured events.
//
// Ownership: ParseStream takes ownership of body and closes it when the
// stream ends or ctx is cancelled.
func ParseStream(ctx context.Context, body io.ReadCloser, opts ParseOpts) llm.Stream {
	pub, ch := llm.NewEventPublisher()
	go func() {
		defer body.Close()
		parseStream(ctx, body, pub, opts)
	}()
	return ch
}

// parseStream is the blocking core that reads SSE lines from body and
// publishes events to pub. It closes pub when done.
func parseStream(ctx context.Context, body io.ReadCloser, pub llm.Publisher, opts ParseOpts) {
	defer pub.Close()

	proc := newStreamProcessor(opts, pub)
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
