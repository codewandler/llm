package anthropic

import (
	"context"
	"io"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/internal/sse"
)

// ParseOpts configures how ParseStream processes an Anthropic-format SSE body.
type ParseOpts struct {
	Model string

	// ProviderName is used in ModelResolvedEvent.Resolver when the API returns
	// a different model than was requested. Set to the provider's own name
	// (e.g. "anthropic", "claude", "minimax").
	ProviderName string

	// ResponseHeaders contains HTTP response headers, used to extract rate-limit info.
	// Keys should be lowercase header names.
	ResponseHeaders map[string]string

	// RequestParams, when non-zero, is published as a RequestEvent at
	// the beginning of the stream. Build it with llm.ProviderRequestFromHTTP
	// after constructing the outgoing *http.Request.
	RequestParams llm.ProviderRequest

	// LLMRequest is the final llm.Request used to build the provider request.
	// Included in the RequestEvent for observability.
	LLMRequest llm.Request
}

// ParseStream reads an Anthropic-format SSE response body in a background
// goroutine and returns a stream of structured events.
//
// Ownership: ParseStream takes ownership of body and closes it when the
// stream ends or ctx is cancelled.
func ParseStream(ctx context.Context, body io.ReadCloser, opts ParseOpts) llm.Stream {
	pub, ch := llm.NewEventPublisher()
	PublishRequestParams(pub, opts)
	go func() {
		parseStream(ctx, body, pub, opts)
	}()
	return ch
}

// ParseStreamWith starts parsing on an existing publisher. The caller is
// responsible for having already created the publisher and optionally published
// early events (e.g. RequestEvent) before the HTTP call.
//
// Ownership: takes ownership of body and closes pub when done.
func ParseStreamWith(ctx context.Context, body io.ReadCloser, pub llm.Publisher, opts ParseOpts) {
	go func() {
		parseStream(ctx, body, pub, opts)
	}()
}

// PublishRequestParams emits a RequestEvent at the start of every stream.
// Exported so that providers using ParseStreamWith can call it before the HTTP request.
func PublishRequestParams(pub llm.Publisher, opts ParseOpts) {
	pub.Publish(&llm.RequestEvent{
		OriginalRequest: opts.LLMRequest,
		ProviderRequest: opts.RequestParams,
	})
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
