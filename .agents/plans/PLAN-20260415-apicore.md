# PLAN: api/apicore — Generic HTTP+SSE Client

> **Design ref**: `.agents/plans/DESIGN-api-extraction.md`
> **Depends on**: nothing (first)
> **Blocks**: `PLAN-20260415-messages.md`, `PLAN-20260415-completions.md`, `PLAN-20260415-responses.md`
> **Estimated total**: ~35 min

---

## Task 1: Scaffold directory and write constants.go

**Files created**: `api/apicore/constants.go`
**Estimated time**: 2 min

```go
// api/apicore/constants.go
package apicore

const (
	HeaderRetryAfter  = "Retry-After"
	HeaderContentType = "Content-Type"

	ContentTypeJSON        = "application/json"
	ContentTypeEventStream = "text/event-stream"
)

// Retryable HTTP status codes.
const (
	StatusTooManyRequests     = 429
	StatusInternalServerError = 500
	StatusBadGateway          = 502
	StatusServiceUnavailable  = 503
	StatusOverloaded          = 529 // Anthropic-specific overload
)

// DefaultRetryableStatuses is the set of HTTP status codes RetryTransport
// retries by default.
var DefaultRetryableStatuses = []int{
	StatusTooManyRequests,
	StatusInternalServerError,
	StatusBadGateway,
	StatusServiceUnavailable,
	StatusOverloaded,
}
```

**Verification**:
```bash
go build ./api/apicore/...
```

---

## Task 2: Write sse.go

**Files created**: `api/apicore/sse.go`
**Estimated time**: 2 min

The SSE scanner is copied from `internal/sse` into this package as **unexported** helpers,
making `api/apicore` self-contained with no dependency on `internal/`. The logic is
identical to `internal/sse/lines.go`; only the names change:

| `internal/sse` | `api/apicore` |
|---|---|
| `Event` (exported struct) | `sseEvent` (unexported struct) |
| `Event.Name` / `Event.Data` | `sseEvent.name` / `sseEvent.data` |
| `ForEachDataLine` (exported func) | `forEachDataLine` (unexported func) |

Do **not** delete `internal/sse` — it is still used by the existing providers until their
own migration phases.

```go
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
				return nil // scanner finished cleanly
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
```

**Verification**:
```bash
go build ./api/apicore/...
```

---

## Task 3: Write client.go (complete)

**Files created**: `api/apicore/client.go`
**Estimated time**: 8 min

> The SSE scanner used here (`forEachDataLine`, `sseEvent`) is defined in
> `api/apicore/sse.go` — same package, no import needed.

```go
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
	url := c.baseURL + c.path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
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
			httpReq.Header.Set(k, v)
		}
	}
	for k, vs := range dynamicHeaders {
		for _, v := range vs {
			httpReq.Header.Set(k, v)
		}
	}

	// 5. Log request
	if c.logger != nil {
		c.logger.InfoContext(ctx, "sending request",
			slog.String("method", http.MethodPost),
			slog.String("url", url),
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

			// ParseHook may inject additional events (e.g. rate-limit metadata).
			if c.parseHook != nil {
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
```

**Verification**:
```bash
go build ./api/apicore/...
```

---

## Task 4: Write options.go

**Files created**: `api/apicore/options.go`
**Estimated time**: 3 min

```go
// api/apicore/options.go
package apicore

import (
	"log/slog"
	"net/http"
)

// ClientOption[Req] configures a Client[Req].
type ClientOption[Req any] func(*Client[Req])

func WithBaseURL[Req any](url string) ClientOption[Req] {
	return func(c *Client[Req]) { c.baseURL = url }
}

func WithPath[Req any](path string) ClientOption[Req] {
	return func(c *Client[Req]) { c.path = path }
}

func WithHTTPClient[Req any](client *http.Client) ClientOption[Req] {
	return func(c *Client[Req]) { c.httpClient = client }
}

// WithHeader sets a static header sent on every request.
func WithHeader[Req any](key, value string) ClientOption[Req] {
	return func(c *Client[Req]) { c.headers.Set(key, value) }
}

func WithHeaderFunc[Req any](fn HeaderFunc[Req]) ClientOption[Req] {
	return func(c *Client[Req]) { c.headerFunc = fn }
}

func WithTransform[Req any](fn TransformFunc[Req]) ClientOption[Req] {
	return func(c *Client[Req]) { c.transform = fn }
}

func WithParseHook[Req any](fn ParseHook[Req]) ClientOption[Req] {
	return func(c *Client[Req]) { c.parseHook = fn }
}

func WithResponseHook[Req any](fn ResponseHook[Req]) ClientOption[Req] {
	return func(c *Client[Req]) { c.responseHook = fn }
}

func WithErrorParser[Req any](fn ErrorParser) ClientOption[Req] {
	return func(c *Client[Req]) { c.errParser = fn }
}

func WithLogger[Req any](logger *slog.Logger) ClientOption[Req] {
	return func(c *Client[Req]) { c.logger = logger }
}
```

**Verification**:
```bash
go build ./api/apicore/...
```

---

## Task 5: Write retry.go

**Files created**: `api/apicore/retry.go`
**Estimated time**: 5 min

> Use `math/rand` (not `math/rand/v2`) for `Int63n`.

```go
// api/apicore/retry.go
package apicore

import (
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// RetryConfig controls RetryTransport behaviour.
type RetryConfig struct {
	MaxRetries        int           // default 2
	RetryableStatuses []int         // default DefaultRetryableStatuses
	InitialBackoff    time.Duration // default 1s
	MaxBackoff        time.Duration // default 60s
	Logger            *slog.Logger  // optional; logs each retry attempt
}

func (rc RetryConfig) withDefaults() RetryConfig {
	if rc.MaxRetries == 0 {
		rc.MaxRetries = 2
	}
	if len(rc.RetryableStatuses) == 0 {
		rc.RetryableStatuses = DefaultRetryableStatuses
	}
	if rc.InitialBackoff == 0 {
		rc.InitialBackoff = time.Second
	}
	if rc.MaxBackoff == 0 {
		rc.MaxBackoff = 60 * time.Second
	}
	return rc
}

type retryTransport struct {
	base http.RoundTripper
	cfg  RetryConfig
}

// NewRetryTransport wraps base with retry-on-retryable-status logic.
// On retryable status codes it:
//  1. Reads Retry-After header (seconds or HTTP-date). Falls back to exponential backoff with jitter.
//  2. Waits, respecting context cancellation.
//  3. Resets the request body via req.GetBody (callers must set GetBody).
//  4. Retries up to MaxRetries times, then returns the last response.
func NewRetryTransport(base http.RoundTripper, cfg RetryConfig) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &retryTransport{base: base, cfg: cfg.withDefaults()}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var (
		resp *http.Response
		err  error
	)
	for attempt := 0; attempt <= t.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := t.backoff(resp, attempt)

			if t.cfg.Logger != nil {
				t.cfg.Logger.InfoContext(req.Context(), "retrying request",
					slog.Int("attempt", attempt),
					slog.Int("status", resp.StatusCode),
					slog.Duration("backoff", backoff),
				)
			}

			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-req.Context().Done():
				timer.Stop()
				return resp, req.Context().Err()
			}

			// Reset body for retry.
			if req.GetBody != nil {
				newBody, berr := req.GetBody()
				if berr != nil {
					return nil, berr
				}
				req.Body = newBody
			}
		}

		resp, err = t.base.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		if !t.isRetryable(resp.StatusCode) || attempt == t.cfg.MaxRetries {
			return resp, nil
		}
		// Drain body before closing so the connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	return resp, nil // err is always nil here; transport errors return early above
}

func (t *retryTransport) isRetryable(status int) bool {
	for _, s := range t.cfg.RetryableStatuses {
		if s == status {
			return true
		}
	}
	return false
}

func (t *retryTransport) backoff(resp *http.Response, attempt int) time.Duration {
	if resp != nil {
		if ra := resp.Header.Get(HeaderRetryAfter); ra != "" {
			if secs, err := strconv.ParseFloat(ra, 64); err == nil && secs > 0 {
				return time.Duration(secs * float64(time.Second))
			}
			if at, err := http.ParseTime(ra); err == nil {
				if d := time.Until(at); d > 0 {
					return d
				}
			}
		}
	}
	// Exponential backoff with 50% jitter.
	exp := math.Pow(2, float64(attempt-1))
	base := time.Duration(exp * float64(t.cfg.InitialBackoff))
	jitter := time.Duration(rand.Int63n(int64(base/2 + 1)))
	d := base + jitter
	if d > t.cfg.MaxBackoff {
		return t.cfg.MaxBackoff
	}
	return d
}
```

**Verification**:
```bash
go build ./api/apicore/...
```

---

## Task 6: Write adapter.go

**Files created**: `api/apicore/adapter.go`
**Estimated time**: 2 min

```go
// api/apicore/adapter.go
package apicore

// AdapterConfig holds identity settings shared by all protocol adapters.
type AdapterConfig struct {
	// ProviderName is used in errors, usage records, and ModelResolvedEvent.Resolver.
	// Example: "anthropic", "openai", "openrouter".
	ProviderName string

	// UpstreamProvider is used in StreamStartedEvent.Provider.
	// Falls back to ProviderName when empty.
	// Relevant for routing providers where billing ≠ upstream backend.
	UpstreamProvider string
}

// Provider returns the effective provider name for errors and records.
func (c AdapterConfig) Provider() string {
	if c.ProviderName != "" {
		return c.ProviderName
	}
	return "unknown"
}

// Upstream returns the effective upstream provider for StreamStartedEvent.
func (c AdapterConfig) Upstream() string {
	if c.UpstreamProvider != "" {
		return c.UpstreamProvider
	}
	return c.Provider()
}

// AdapterOption configures AdapterConfig.
type AdapterOption func(*AdapterConfig)

func WithProviderName(name string) AdapterOption {
	return func(c *AdapterConfig) { c.ProviderName = name }
}

func WithUpstreamProvider(name string) AdapterOption {
	return func(c *AdapterConfig) { c.UpstreamProvider = name }
}

// ApplyAdapterOptions builds an AdapterConfig from options.
func ApplyAdapterOptions(opts ...AdapterOption) AdapterConfig {
	cfg := AdapterConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
```

**Verification**:
```bash
go build ./api/apicore/...
```

---

## Task 7: Write testing.go

**Files created**: `api/apicore/testing.go`
**Estimated time**: 2 min

```go
// api/apicore/testing.go
package apicore

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// RoundTripFunc adapts a plain function to http.RoundTripper.
// Use with WithHTTPClient to intercept HTTP in tests without a real server.
//
//	c := NewClient[Req](factory,
//	    WithHTTPClient[Req](&http.Client{Transport: RoundTripFunc(func(r *http.Request) (*http.Response, error) {
//	        return &http.Response{...}, nil
//	    })}),
//	)
type RoundTripFunc func(*http.Request) (*http.Response, error)

func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// FixedSSEResponse returns a RoundTripFunc that always responds with the given
// status code and SSE body. Use for table-driven stream parser tests.
func FixedSSEResponse(statusCode int, sseBody string) RoundTripFunc {
	return func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: statusCode,
			Header:     http.Header{"Content-Type": {ContentTypeEventStream}},
			Body:       io.NopCloser(strings.NewReader(sseBody)),
		}, nil
	}
}

// NewTestHandle creates a StreamHandle populated with canned StreamResults.
// Use in adapter tests that bypass HTTP entirely.
//
//	handle := NewTestHandle(
//	    StreamResult{Event: &SomeEvent{...}},
//	    StreamResult{Done: true},
//	)
func NewTestHandle(events ...StreamResult) *StreamHandle {
	ch := make(chan StreamResult, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return &StreamHandle{
		Events:  ch,
		Request: httptest.NewRequest(http.MethodPost, "/test", nil),
		Headers: make(http.Header),
	}
}
```

**Verification**:
```bash
go build ./api/apicore/...
```

---

## Task 8: Write client_test.go

**Files created**: `api/apicore/client_test.go`
**Estimated time**: 5 min

```go
// api/apicore/client_test.go
package apicore_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codewandler/llm/api/apicore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testReq struct {
	Model string `json:"model"`
}

func noopParser() apicore.EventHandler {
	return func(name string, data []byte) apicore.StreamResult {
		return apicore.StreamResult{Done: true}
	}
}

func sseBody(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func TestClient_Stream_URLComposition(t *testing.T) {
	var gotURL string
	transport := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	c := apicore.NewClient[testReq](func() apicore.EventHandler { return noopParser() },
		apicore.WithBaseURL[testReq]("https://api.example.com"),
		apicore.WithPath[testReq]("/v1/stream"),
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
	)
	_, err := c.Stream(context.Background(), &testReq{Model: "test"})
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com/v1/stream", gotURL)
}

func TestClient_Stream_SerializesBody(t *testing.T) {
	var gotBody []byte
	transport := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotBody, _ = io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	c := apicore.NewClient[testReq](func() apicore.EventHandler { return noopParser() },
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
	)
	_, err := c.Stream(context.Background(), &testReq{Model: "gpt-4"})
	require.NoError(t, err)
	var parsed testReq
	require.NoError(t, json.Unmarshal(gotBody, &parsed))
	assert.Equal(t, "gpt-4", parsed.Model)
}

func TestClient_Stream_Non2xx_UsesErrorParser(t *testing.T) {
	transport := apicore.FixedSSEResponse(429, `{"error":"rate limited"}`)
	c := apicore.NewClient[testReq](func() apicore.EventHandler { return noopParser() },
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
		apicore.WithErrorParser[testReq](func(status int, body []byte) error {
			return &apicore.HTTPError{StatusCode: status, Body: body}
		}),
	)
	_, err := c.Stream(context.Background(), &testReq{})
	var herr *apicore.HTTPError
	require.ErrorAs(t, err, &herr)
	assert.Equal(t, 429, herr.StatusCode)
}

func TestClient_Stream_Non2xx_DefaultHTTPError(t *testing.T) {
	transport := apicore.FixedSSEResponse(500, `internal error`)
	c := apicore.NewClient[testReq](func() apicore.EventHandler { return noopParser() },
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
	)
	_, err := c.Stream(context.Background(), &testReq{})
	var herr *apicore.HTTPError
	require.ErrorAs(t, err, &herr)
	assert.Equal(t, 500, herr.StatusCode)
}

func TestClient_Stream_EventsDeliveredInOrder(t *testing.T) {
	body := sseBody(
		"data: one",
		"",
		"data: two",
		"",
		"data: [DONE]",
		"",
	)
	transport := apicore.FixedSSEResponse(200, body)
	c := apicore.NewClient[testReq](func() apicore.EventHandler {
		return func(name string, data []byte) apicore.StreamResult {
			s := string(data)
			if s == "[DONE]" {
				return apicore.StreamResult{Done: true}
			}
			return apicore.StreamResult{Event: s}
		}
	}, apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}))

	handle, err := c.Stream(context.Background(), &testReq{})
	require.NoError(t, err)

	var received []string
	for result := range handle.Events {
		if s, ok := result.Event.(string); ok {
			received = append(received, s)
		}
	}
	assert.Equal(t, []string{"one", "two"}, received)
}

func TestClient_Stream_ResponseHookReceivesMeta(t *testing.T) {
	transport := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"X-Request-Id": {"req-123"}, "Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})

	var gotMeta apicore.ResponseMeta
	c := apicore.NewClient[testReq](func() apicore.EventHandler { return noopParser() },
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
		apicore.WithResponseHook[testReq](func(req *testReq, meta apicore.ResponseMeta) {
			gotMeta = meta
		}),
	)
	_, err := c.Stream(context.Background(), &testReq{})
	require.NoError(t, err)
	assert.Equal(t, 200, gotMeta.StatusCode)
	assert.Equal(t, "req-123", gotMeta.Headers.Get("X-Request-Id"))
}

func TestClient_Stream_StaticAndDynamicHeadersMerge(t *testing.T) {
	var gotHeaders http.Header
	transport := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotHeaders = r.Header.Clone()
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	c := apicore.NewClient[testReq](func() apicore.EventHandler { return noopParser() },
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
		apicore.WithHeader[testReq]("X-Static", "static-val"),
		apicore.WithHeaderFunc[testReq](func(ctx context.Context, req *testReq) (http.Header, error) {
			return http.Header{"X-Dynamic": {"dyn-val"}}, nil
		}),
	)
	_, err := c.Stream(context.Background(), &testReq{})
	require.NoError(t, err)
	assert.Equal(t, "static-val", gotHeaders.Get("X-Static"))
	assert.Equal(t, "dyn-val", gotHeaders.Get("X-Dynamic"))
}

func TestClient_Stream_WithLogger_DeliversEvents(t *testing.T) {
	transport := apicore.FixedSSEResponse(200, sseBody("data: hello", "", "data: [DONE]", ""))
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c := apicore.NewClient[testReq](func() apicore.EventHandler {
		return func(name string, data []byte) apicore.StreamResult {
			if string(data) == "[DONE]" {
				return apicore.StreamResult{Done: true}
			}
			return apicore.StreamResult{Event: string(data)}
		}
	},
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
		apicore.WithLogger[testReq](logger),
	)
	handle, err := c.Stream(context.Background(), &testReq{Model: "test"})
	require.NoError(t, err)
	var events []any
	for result := range handle.Events {
		if result.Event != nil {
			events = append(events, result.Event)
		}
	}
	assert.Equal(t, []any{"hello"}, events)
}

func TestClient_Stream_NamedEvents(t *testing.T) {
	body := sseBody(
		"event: message_start",
		"data: {\"id\":\"msg_1\"}",
		"",
		"event: content_block_delta",
		"data: {\"text\":\"hello\"}",
		"",
		"event: message_stop",
		"data: {}",
		"",
	)
	transport := apicore.FixedSSEResponse(200, body)
	type event struct{ name, data string }
	c := apicore.NewClient[testReq](func() apicore.EventHandler {
		return func(name string, data []byte) apicore.StreamResult {
			if name == "message_stop" {
				return apicore.StreamResult{Event: event{name, string(data)}, Done: true}
			}
			if name != "" {
				return apicore.StreamResult{Event: event{name, string(data)}}
			}
			return apicore.StreamResult{}
		}
	}, apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}))

	handle, err := c.Stream(context.Background(), &testReq{})
	require.NoError(t, err)

	var received []event
	for result := range handle.Events {
		if e, ok := result.Event.(event); ok {
			received = append(received, e)
		}
	}
	require.Len(t, received, 3)
	assert.Equal(t, "message_start", received[0].name)
	assert.Equal(t, "content_block_delta", received[1].name)
	assert.Equal(t, "message_stop", received[2].name)
}

func TestClient_Stream_ContextCancelled_ChannelCloses(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	transport := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       pr,
		}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	c := apicore.NewClient[testReq](func() apicore.EventHandler {
		return func(name string, data []byte) apicore.StreamResult {
			return apicore.StreamResult{Event: string(data)}
		}
	}, apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}))

	handle, err := c.Stream(ctx, &testReq{})
	require.NoError(t, err)

	cancel() // signal cancellation; forEachDataLine will close the pipe reader

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range handle.Events {}
	}()

	select {
	case <-done:
		// handle.Events closed cleanly after context cancellation
	case <-time.After(2 * time.Second):
		t.Fatal("handle.Events did not close after context cancellation")
	}
}
```

**Verification**:
```bash
go test ./api/apicore/... -v -count=1
```

---

## Task 9: Write retry_test.go

**Files created**: `api/apicore/retry_test.go`
**Estimated time**: 4 min

> Note: `TestRetryTransport_RespectsRetryAfterHeader` uses a real `time.After` delay
> (50ms). It is intentionally brief. If it becomes flaky in CI, increase the
> `Retry-After` value and the `GreaterOrEqual` threshold together.

```go
// api/apicore/retry_test.go
package apicore_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codewandler/llm/api/apicore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryTransport_NoRetryOnSuccess(t *testing.T) {
	calls := 0
	base := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	})
	tr := apicore.NewRetryTransport(base, apicore.RetryConfig{MaxRetries: 2})
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://x", nil)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 1, calls)
}

func TestRetryTransport_RetriesOn429(t *testing.T) {
	calls := 0
	base := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		status := 429
		if calls > 1 {
			status = 200
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil
	})
	body := []byte(`{"model":"x"}`)
	tr := apicore.NewRetryTransport(base, apicore.RetryConfig{
		MaxRetries:     2,
		InitialBackoff: time.Millisecond,
	})
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://x",
		bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 2, calls)
}

func TestRetryTransport_RespectsRetryAfterHeader(t *testing.T) {
	start := time.Now()
	calls := 0
	base := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		h := make(http.Header)
		if calls == 1 {
			h.Set("Retry-After", "0.05") // 50ms
			return &http.Response{StatusCode: 429, Header: h, Body: io.NopCloser(bytes.NewReader(nil))}, nil
		}
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	})
	body := []byte(`{}`)
	tr := apicore.NewRetryTransport(base, apicore.RetryConfig{MaxRetries: 1})
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://x",
		bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.GreaterOrEqual(t, time.Since(start), 40*time.Millisecond)
}

func TestRetryTransport_ExhaustedReturnsLastResponse(t *testing.T) {
	base := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 529,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil
	})
	body := []byte(`{}`)
	tr := apicore.NewRetryTransport(base, apicore.RetryConfig{
		MaxRetries:     2,
		InitialBackoff: time.Millisecond,
	})
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://x",
		bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, 529, resp.StatusCode) // last response returned, not an error
}

func TestRetryTransport_LogsRetryAttempts(t *testing.T) {
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	calls := 0
	base := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		status := 429
		if calls > 1 {
			status = 200
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil
	})
	body := []byte(`{}`)
	tr := apicore.NewRetryTransport(base, apicore.RetryConfig{
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
		Logger:         logger,
	})
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://x",
		bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, logBuf.String(), "retrying request")
}
```

**Verification**:
```bash
go test ./api/apicore/... -v -count=1 -run TestRetry
go test ./api/apicore/... -race -count=1
```

---

## Phase completion check

```bash
go build ./api/apicore/...
go vet ./api/apicore/...
go test ./api/apicore/... -race -count=1
```

All tests must pass, no race conditions, vet clean before proceeding to the messages plan.
