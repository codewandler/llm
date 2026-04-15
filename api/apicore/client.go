// api/apicore/client.go
package apicore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Client[Req] is a generic HTTP+SSE streaming client. Req is the wire request
// type. All protocol-specific behavior is injected via options and ParserFactory.
//
// Thread-safe: a single Client may be shared across goroutines.
type Client[Req any] struct {
	baseURL      string
	path         string
	httpClient   *http.Client
	headers      http.Header        // static headers merged into every request
	headerFunc   HeaderFunc[Req]    // dynamic headers (auth, model-conditional)
	transform    TransformFunc[Req] // surgical request modification before serialization
	parseHook    ParseHook[Req]     // optional: emit provider-specific events per SSE event
	responseHook ResponseHook[Req]  // optional: inspect HTTP response metadata
	parser       ParserFactory      // creates a per-stream, stateful EventHandler
	errParser    ErrorParser        // optional: parse HTTP error bodies into typed errors
	logger       *slog.Logger
}

// HeaderFunc returns headers to add to each HTTP request. It receives the
// typed wire request so headers can be conditional on request fields
// (e.g. model-dependent beta headers).
type HeaderFunc[Req any] func(ctx context.Context, req *Req) (http.Header, error)

// TransformFunc allows inspection and surgical modification of the fully-built
// wire request before JSON serialization. The returned value is what gets
// serialized; wrap req in an anonymous struct to inject extra fields.
type TransformFunc[Req any] func(req *Req) any

// ParserFactory creates a fresh, stateful EventHandler for a single stream.
// Called once per Stream() invocation. Per-stream state (tool accumulators,
// block indices, etc.) lives inside the returned closure.
type ParserFactory func() EventHandler

// EventHandler processes a single raw SSE event. Returns StreamResult.
// name is the SSE event name (empty for Chat Completions data-only streams).
// data is the raw JSON payload bytes.
type EventHandler func(name string, data []byte) StreamResult

// ParseHook is called for each SSE event after the EventHandler. It receives
// the original wire request and the raw SSE event. If it returns a non-nil
// value, that value is emitted as a standalone StreamResult immediately after
// the standard event.
type ParseHook[Req any] func(req *Req, eventName string, data []byte) any

// ResponseHook is called after the HTTP response is received, before stream
// parsing begins. It is called on every response (including 2xx). The body is
// NOT accessible (owned by the SSE parser). Used for header extraction
// (rate limits, request IDs, provider quotas, per-model metrics).
type ResponseHook[Req any] func(req *Req, meta ResponseMeta)

// ResponseMeta is the HTTP response metadata visible to ResponseHook.
type ResponseMeta struct {
	StatusCode int
	Headers    http.Header
}

// ErrorParser converts a non-2xx HTTP response body into a typed error.
// If nil, Stream() returns *HTTPError with status + raw body.
type ErrorParser func(statusCode int, body []byte) error

// HTTPError is the default error returned for non-2xx responses when no
// ErrorParser is configured.
type HTTPError struct {
	StatusCode int
	Body       []byte
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Body)
}

// StreamHandle is the result of a successful Stream() call.
type StreamHandle struct {
	Events  <-chan StreamResult
	Request *http.Request // the outgoing HTTP request (for observability)
	Headers http.Header   // cloned response headers (rate limits, etc.)
}

// StreamResult is one item on the Events channel.
type StreamResult struct {
	Event any   // typed event from EventHandler, or provider-specific from ParseHook
	Err   error // set on protocol-level errors
	Done  bool  // true on terminal event; channel is closed after this item
}

// NewClient creates a new generic client with the given ParserFactory and options.
func NewClient[Req any](parser ParserFactory, opts ...ClientOption[Req]) *Client[Req] {
	if parser == nil {
		panic("apicore: parser factory cannot be nil")
	}
	c := &Client[Req]{
		parser:     parser,
		httpClient: http.DefaultClient,
		headers:    make(http.Header),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Stream sends a streaming HTTP POST and returns a StreamHandle.
//
// Processing order:
//  1. Call HeaderFunc(ctx, req) → dynamic headers
//  2. Apply TransformFunc(req) → serializable value
//  3. Serialize to JSON
//  4. Build http.Request with static + dynamic headers; set GetBody for RetryTransport
//  5. Log request at Info (if logger set): method, url, content_length
//  6. Send via httpClient (retry handled by transport layer)
//  7. Log response at Info (if logger set): status, latency_ms
//  8. Call ResponseHook(req, meta) on ALL responses (before error check)
//  9. Check status: 2xx → stream; non-2xx → read body, return typed error
//
// 10. Create fresh EventHandler from ParserFactory
// 11. Start background goroutine: SSE scan → handler → hook → channel
// 12. Return StreamHandle
func (c *Client[Req]) Stream(ctx context.Context, req *Req) (*StreamHandle, error) {
	// 1. Dynamic headers
	var dynamicHeaders http.Header
	if c.headerFunc != nil {
		h, err := c.headerFunc(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("build request headers: %w", err)
		}
		dynamicHeaders = h
	}

	// 2. Transform
	var body any = req
	if c.transform != nil {
		body = c.transform(req)
	}

	// 3. Serialize
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("serialize request: %w", err)
	}

	// 4. Build HTTP request; set GetBody so RetryTransport can reset the body.
	endpoint := c.baseURL + c.path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("build HTTP request: %w", err)
	}
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	httpReq.ContentLength = int64(len(data))

	httpReq.Header.Set(HeaderContentType, ContentTypeJSON)
	for k, vs := range c.headers {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	for k, vs := range dynamicHeaders {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}

	// 5. Log request
	if c.logger != nil {
		c.logger.InfoContext(ctx, "sending request",
			slog.String("method", http.MethodPost),
			slog.String("url", endpoint),
			slog.Int("content_length", len(data)),
		)
	}

	// 6. Send
	startTime := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	latency := time.Since(startTime)

	// 7. Log response
	if c.logger != nil {
		c.logger.InfoContext(ctx, "response received",
			slog.Int("status", resp.StatusCode),
			slog.Int64("latency_ms", latency.Milliseconds()),
		)
	}

	// 8. ResponseHook (runs on ALL responses, before error check)
	if c.responseHook != nil {
		c.responseHook(req, ResponseMeta{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
		})
	}

	// 9. Error check
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if c.errParser != nil {
			return nil, c.errParser(resp.StatusCode, errBody)
		}
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: errBody}
	}

	// 10. Fresh EventHandler per stream
	handler := c.parser()

	// 11. Background SSE loop
	ch := make(chan StreamResult, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		var eventCount int
		streamStart := time.Now()

		scanErr := forEachDataLine(ctx, resp.Body, func(ev sseEvent) bool {
			result := handler(ev.name, []byte(ev.data))

			if c.logger != nil {
				c.logger.DebugContext(ctx, "SSE event",
					slog.String("event_name", ev.name),
					slog.Int("data_size", len(ev.data)),
				)
			}

			// Only emit non-empty results; handlers return zero-value for unknown events.
			if result.Event != nil || result.Err != nil || result.Done {
				ch <- result
				eventCount++
			}

			// ParseHook runs on non-terminal events only; Done marks the last item.
			if c.parseHook != nil && !result.Done {
				if extra := c.parseHook(req, ev.name, []byte(ev.data)); extra != nil {
					ch <- StreamResult{Event: extra}
				}
			}

			return !result.Done
		})

		if scanErr != nil && ctx.Err() == nil {
			ch <- StreamResult{Err: scanErr}
		}

		if c.logger != nil {
			c.logger.InfoContext(ctx, "stream completed",
				slog.Int("events_count", eventCount),
				slog.Int64("duration_ms", time.Since(streamStart).Milliseconds()),
			)
		}
	}()

	// 12. Return handle with cloned headers so callers get a stable copy.
	return &StreamHandle{
		Events:  ch,
		Request: httpReq,
		Headers: resp.Header.Clone(),
	}, nil
}
