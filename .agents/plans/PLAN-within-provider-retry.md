# PLAN: Within-Provider Retry with Exponential Backoff

**Design ref:** `.agents/plans/DESIGN-within-provider-retry.md`  
**Estimated total:** ~90 minutes

Tasks are ordered by dependency. Each task must compile + pass `go build ./...` before the next.

---

## Task 1 — Extend `IsRetriableHTTPStatus` in `errors.go`

**Files modified:** `errors.go`  
**Estimated time:** 5 minutes  
**Depends on:** nothing

Add `IsFailoverEligibleHTTPStatus` (500 and 502 are new), keep `IsRetriableHTTPStatus` as a
deprecated variable alias so existing call sites compile unchanged.

> **Semantic change note:** The four providers that call `if llm.IsRetriableHTTPStatus(resp.StatusCode)`
> (anthropic, claude, minimax, openrouter) will now return HTTP 500 and 502 errors from `CreateStream`
> as proper error values rather than publishing them through the event stream. This is the correct
> behaviour — it enables the router to fail over on 500/502 — but it is a behaviour change for
> callers that consume the stream directly. Note it clearly in the commit message.

**Replace** the existing `IsRetriableHTTPStatus` function:

```go
// IsRetriableHTTPStatus reports whether an HTTP status code indicates a
// transient condition where retrying with a different provider may succeed.
// Providers use this to decide whether to surface an HTTP error through the
// event stream (non-retriable) or return it as an error from CreateStream
// (retriable, so the router can fail over to the next target).
func IsRetriableHTTPStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,   // 429 — rate limited
		http.StatusServiceUnavailable, // 503 — temporarily down
		http.StatusPaymentRequired:    // 402 — OpenRouter out of credits
		return true
	}
	return false
}
```

**With:**

```go
// IsFailoverEligibleHTTPStatus reports whether an HTTP status code indicates a
// condition where retrying with a different provider may succeed after the
// within-provider retry budget has been exhausted.
//
// 500 and 502 are included because the RetryTransport will have already made
// multiple attempts against the same provider; by the time this is checked the
// error is genuinely unrecoverable at that provider.
func IsFailoverEligibleHTTPStatus(code int) bool {
	switch code {
	case http.StatusPaymentRequired,    // 402 — out of credits
		http.StatusTooManyRequests,     // 429 — rate limited
		http.StatusInternalServerError, // 500 — server error
		http.StatusBadGateway,          // 502 — gateway error
		http.StatusServiceUnavailable:  // 503 — temporarily down
		return true
	}
	return false
}

// IsRetriableHTTPStatus is deprecated; use IsFailoverEligibleHTTPStatus.
//
// Deprecated: This function is kept for backwards compatibility and will be
// removed in a future version.
var IsRetriableHTTPStatus = IsFailoverEligibleHTTPStatus
```

**Verification:**
```bash
go build ./...
go test ./... -run TestProviderError
```

---

## Task 2 — Add `RetryTransport` to `http.go`

**Files modified:** `http.go`  
**Files created:** `http_retry_test.go`  
**Estimated time:** 25 minutes  
**Depends on:** nothing (self-contained addition)

### 2a — Add imports to `http.go`

Add `"context"`, `"math/rand"`, and `"strconv"` to the import block. `"net/http"` and `"time"` are
already present. `"math"` is **not** needed — `computeDelay` uses an iterative multiply loop, not
`math.Pow`.

The `sleep` helper method uses `context.Context` directly, which requires the `"context"` import.
The current `http.go` does not import it.

### 2b — Append to `http.go` (after the existing `DefaultHttpClient` function)

```go
// ---------------------------------------------------------------------------
// RetryTransport — within-provider retry middleware
// ---------------------------------------------------------------------------

// RetryConfig controls within-provider retry behaviour for RetryTransport.
// A zero value (MaxRetries == 0) means no retries are performed.
// Use DefaultRetryConfig for sensible production defaults.
type RetryConfig struct {
	// MaxRetries is the maximum number of additional attempts after the first
	// failure. 0 disables retries entirely. Default (DefaultRetryConfig): 5.
	MaxRetries int

	// BaseDelay is the starting backoff duration for the first retry.
	// Default: 500ms.
	BaseDelay time.Duration

	// MaxDelay caps the computed exponential backoff. Default: 30s.
	MaxDelay time.Duration

	// Multiplier is the exponential factor applied on each successive attempt.
	// Default: 2.0 (doubles the delay each time).
	Multiplier float64

	// Jitter adds ±25 % random spread to avoid thundering-herd. Default: true.
	Jitter bool

	// MaxRetryAfter is the upper bound on a Retry-After response header that
	// still qualifies for a within-provider retry. When the header value exceeds
	// this (or is absent on a 429), the response is returned immediately so the
	// router can fail over to the next target.
	// Default: 5s — short windows are worth absorbing locally.
	MaxRetryAfter time.Duration

	// ShouldRetry is called with the response and/or transport error from each
	// attempt. Return true to consume the response body and retry; false to
	// return immediately. When nil, DefaultShouldRetry is used.
	ShouldRetry func(resp *http.Response, err error) bool

	// Timer replaces time.NewTimer for unit tests that need instant retry delays.
	// The returned channel must fire exactly once after the given duration (or
	// immediately in tests). Production code must leave this nil.
	Timer func(d time.Duration) <-chan time.Time
}

// DefaultRetryConfig returns a RetryConfig with production-ready defaults:
// 5 retries, 500ms base delay doubling up to 30s, ±25 % jitter, and a 5s
// ceiling on Retry-After headers.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:    5,
		BaseDelay:     500 * time.Millisecond,
		MaxDelay:      30 * time.Second,
		Multiplier:    2.0,
		Jitter:        true,
		MaxRetryAfter: 5 * time.Second,
	}
}

// DefaultShouldRetry is the default predicate for RetryConfig.ShouldRetry.
//
//   - err != nil          — transport/network error; provider never received
//     the request, so retrying is safe.
//   - 429 (rate limited)  — signals "maybe retry" to the transport loop, which
//     then applies the Retry-After guard: retries only if the header is present
//     and ≤ MaxRetryAfter; otherwise returns immediately for router failover.
//   - 500, 502, 503       — transient server errors; always retried with
//     exponential backoff regardless of response headers.
func DefaultShouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests,      // 429
		http.StatusInternalServerError,   // 500
		http.StatusBadGateway,            // 502
		http.StatusServiceUnavailable:    // 503
		return true
	}
	return false
}

// RetryTransport is an http.RoundTripper middleware that retries failed
// requests with exponential backoff before returning the final response to the
// caller. It wraps another RoundTripper and is transparent to the caller: the
// final (*http.Response, error) pair it returns is identical in shape to what
// the caller would have received without retries.
//
// Create with NewRetryTransport; do not use the struct literal directly.
type RetryTransport struct {
	wrapped http.RoundTripper
	cfg     RetryConfig
}

// NewRetryTransport creates a RetryTransport that wraps the given transport.
// Missing fields in cfg are filled with defaults: ShouldRetry ← DefaultShouldRetry,
// BaseDelay ← 500ms, MaxDelay ← 30s, Multiplier ← 2.0, MaxRetryAfter ← 5s.
// When cfg.MaxRetries == 0 the transport is a pass-through.
func NewRetryTransport(cfg RetryConfig, wrapped http.RoundTripper) *RetryTransport {
	if cfg.ShouldRetry == nil {
		cfg.ShouldRetry = DefaultShouldRetry
	}
	if cfg.BaseDelay == 0 {
		cfg.BaseDelay = 500 * time.Millisecond
	}
	if cfg.MaxDelay == 0 {
		cfg.MaxDelay = 30 * time.Second
	}
	if cfg.Multiplier == 0 {
		cfg.Multiplier = 2.0
	}
	if cfg.MaxRetryAfter == 0 {
		cfg.MaxRetryAfter = 5 * time.Second
	}
	return &RetryTransport{wrapped: wrapped, cfg: cfg}
}

// RoundTrip executes the request, retrying on transient failures according to
// the configured RetryConfig. The caller's request body is replayed on each
// retry via req.GetBody; if the body is non-nil and GetBody is nil, the first
// attempt is still made but subsequent retries skip body replay (safe for
// bodyless GET requests, but not for POST — all LLM APIs use POST with
// bytes.NewReader which auto-populates GetBody).
func (rt *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.cfg.MaxRetries == 0 {
		return rt.wrapped.RoundTrip(req)
	}

	var (
		resp  *http.Response
		err   error
		delay time.Duration
	)

	for attempt := 0; attempt <= rt.cfg.MaxRetries; attempt++ {
		// ── wait before every retry (skip on the very first attempt) ───────
		if attempt > 0 {
			if sleepErr := rt.sleep(req.Context(), delay); sleepErr != nil {
				return nil, sleepErr
			}
			// Replay the request body for the next attempt.
			if req.GetBody != nil {
				req.Body, _ = req.GetBody()
			}
		}

		resp, err = rt.wrapped.RoundTrip(req)

		// Not a candidate for retry — return immediately.
		if !rt.cfg.ShouldRetry(resp, err) {
			return resp, err
		}

		// On the last attempt return whatever we have.
		if attempt == rt.cfg.MaxRetries {
			break
		}

		// ── compute delay for the next attempt ──────────────────────────────
		// Parse Retry-After before closing the body.
		var retryAfter time.Duration
		if resp != nil {
			retryAfter = retryAfterFromResponse(resp)
		}

		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			// 429: only retry if the provider told us when — and the wait is short.
			if retryAfter == 0 || retryAfter > rt.cfg.MaxRetryAfter {
				// Unknown or too-long rate-limit window — let the router fail over.
				return resp, err
			}
			delay = retryAfter
		} else if retryAfter > 0 {
			// Non-429 with Retry-After (e.g. some 503 responses).
			if retryAfter > rt.cfg.MaxRetryAfter {
				return resp, err
			}
			delay = retryAfter
		} else {
			// Transport error or 500/502/503 without Retry-After: use our own backoff.
			delay = rt.computeDelay(attempt + 1)
		}

		// Drain and discard the response body before the next attempt.
		if resp != nil {
			resp.Body.Close()
			resp = nil
		}
	}

	return resp, err
}

// sleep pauses for duration d, returning ctx.Err() if the context is cancelled
// first. Uses cfg.Timer when set (for tests), otherwise time.NewTimer to avoid
// the goroutine leak that time.After causes when the context fires first.
func (rt *RetryTransport) sleep(ctx context.Context, d time.Duration) error {
	if rt.cfg.Timer != nil {
		select {
		case <-rt.cfg.Timer(d):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	t := time.NewTimer(d)
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		t.Stop()
		return ctx.Err()
	}
}

// computeDelay returns the exponential backoff duration for the given attempt
// number (1-based), capped at MaxDelay and optionally jittered.
func (rt *RetryTransport) computeDelay(attempt int) time.Duration {
	delay := float64(rt.cfg.BaseDelay)
	for i := 1; i < attempt; i++ {
		delay *= rt.cfg.Multiplier
		if delay >= float64(rt.cfg.MaxDelay) {
			delay = float64(rt.cfg.MaxDelay)
			break
		}
	}
	if rt.cfg.Jitter {
		// ±25 % uniform jitter
		spread := delay * 0.25
		delay += (rand.Float64()*2 - 1) * spread //nolint:gosec // not a security use
	}
	return time.Duration(delay)
}

// retryAfterFromResponse parses the Retry-After response header.
// Supports both integer seconds ("120") and HTTP-date formats.
// Returns 0 when the header is absent or unparseable.
func retryAfterFromResponse(resp *http.Response) time.Duration {
	header := resp.Header.Get("Retry-After")
	if header == "" {
		return 0
	}
	// Integer seconds (most common in LLM APIs).
	if secs, err := strconv.Atoi(header); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	// HTTP-date format (RFC 1123).
	if t, err := http.ParseTime(header); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
```

### 2c — Wire into `NewHttpClient`

In `NewHttpClient`, after the `decompressingTransport` line and before the logger block, add:

```go
	if opts.Retry.MaxRetries > 0 {
		transport = NewRetryTransport(opts.Retry, transport)
	}
```

Also add the `Retry RetryConfig` field to `HttpClientOpts`:

```go
	// Retry configures within-provider retry with exponential backoff.
	// Zero value (MaxRetries == 0) disables retries.
	// Use DefaultRetryConfig() for sensible production defaults.
	Retry RetryConfig
```

### 2d — Write `http_retry_test.go`

```go
package llm_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

// mockTransport records calls and returns configured responses in order.
// After the configured responses are exhausted it returns the last one.
type mockTransport struct {
	responses []*http.Response
	errs      []error
	calls     int
}

func (m *mockTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	i := m.calls
	if i >= len(m.responses) {
		i = len(m.responses) - 1
	}
	m.calls++
	return m.responses[i], m.errs[i]
}

func mockResp(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func mockRespWithHeader(status int, key, value string) *http.Response {
	r := mockResp(status)
	r.Header.Set(key, value)
	return r
}

// fastTimer returns a channel that fires immediately, used to make retry
// sleeps instant in unit tests.
func fastTimer(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}

// fastCfg returns a RetryConfig with tiny delays and an instant timer for
// unit tests. All retry sleeps (including Retry-After waits) complete
// immediately so tests run fast.
func fastCfg(maxRetries int) llm.RetryConfig {
	return llm.RetryConfig{
		MaxRetries:    maxRetries,
		BaseDelay:     time.Millisecond,
		MaxDelay:      10 * time.Millisecond,
		Multiplier:    2.0,
		Jitter:        false,
		MaxRetryAfter: 5 * time.Second,
		Timer:         fastTimer, // instant — no real sleeping in tests
	}
}

func makeResponses(statuses ...int) ([]*http.Response, []error) {
	resps := make([]*http.Response, len(statuses))
	errs := make([]error, len(statuses))
	for i, s := range statuses {
		resps[i] = mockResp(s)
	}
	return resps, errs
}

func TestRetryTransport_NoRetries_PassThrough(t *testing.T) {
	resps, errs := makeResponses(http.StatusOK)
	inner := &mockTransport{responses: resps, errs: errs}
	rt := llm.NewRetryTransport(llm.RetryConfig{MaxRetries: 0}, inner)

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 1, inner.calls)
}

func TestRetryTransport_SuccessOnThirdAttempt(t *testing.T) {
	resps := []*http.Response{mockResp(500), mockResp(500), mockResp(200)}
	errs := []error{nil, nil, nil}
	inner := &mockTransport{responses: resps, errs: errs}
	rt := llm.NewRetryTransport(fastCfg(5), inner)

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 3, inner.calls)
}

func TestRetryTransport_ExhaustRetries_ReturnsFinal500(t *testing.T) {
	resps, errs := makeResponses(500, 500, 500, 500, 500, 500) // 6 = 1 + 5 retries
	inner := &mockTransport{responses: resps, errs: errs}
	rt := llm.NewRetryTransport(fastCfg(5), inner)

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, 6, inner.calls, "should attempt 1 + MaxRetries times")
}

func TestRetryTransport_NonRetriableStatus_NoRetry(t *testing.T) {
	resps, errs := makeResponses(400)
	inner := &mockTransport{responses: resps, errs: errs}
	rt := llm.NewRetryTransport(fastCfg(5), inner)

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, 1, inner.calls, "400 should not be retried")
}

func TestRetryTransport_TransportError_Retried(t *testing.T) {
	netErr := errors.New("connection reset")
	resps := []*http.Response{nil, nil, mockResp(200)}
	errs := []error{netErr, netErr, nil}
	inner := &mockTransport{responses: resps, errs: errs}
	rt := llm.NewRetryTransport(fastCfg(5), inner)

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 3, inner.calls)
}

func TestRetryTransport_429_ShortRetryAfter_Retried(t *testing.T) {
	r429 := mockRespWithHeader(429, "Retry-After", "1") // 1s <= 5s MaxRetryAfter
	resps := []*http.Response{r429, mockResp(200)}
	errs := []error{nil, nil}
	inner := &mockTransport{responses: resps, errs: errs}

	cfg := fastCfg(5)
	cfg.MaxRetryAfter = 5 * time.Second
	rt := llm.NewRetryTransport(cfg, inner)

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 2, inner.calls, "429 with short Retry-After should be retried once")
}

func TestRetryTransport_429_LongRetryAfter_ImmediateReturn(t *testing.T) {
	r429 := mockRespWithHeader(429, "Retry-After", "60") // 60s > 5s MaxRetryAfter
	resps := []*http.Response{r429}
	errs := []error{nil}
	inner := &mockTransport{responses: resps, errs: errs}

	cfg := fastCfg(5)
	cfg.MaxRetryAfter = 5 * time.Second
	rt := llm.NewRetryTransport(cfg, inner)

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, 1, inner.calls, "429 with long Retry-After must not retry")
}

func TestRetryTransport_429_NoRetryAfter_ImmediateReturn(t *testing.T) {
	resps, errs := makeResponses(429)
	inner := &mockTransport{responses: resps, errs: errs}
	rt := llm.NewRetryTransport(fastCfg(5), inner)

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, 1, inner.calls, "429 without Retry-After must not retry")
}

func TestRetryTransport_ContextCancelled_DuringBackoff(t *testing.T) {
	// No Timer set — uses real time so the context timeout (50ms) can fire
	// before the 500ms backoff sleep, exercising the cancellation path.
	resps, errs := makeResponses(500, 500)
	inner := &mockTransport{responses: resps, errs: errs}

	cfg := llm.RetryConfig{
		MaxRetries:    5,
		BaseDelay:     500 * time.Millisecond,
		MaxDelay:      30 * time.Second,
		Multiplier:    2.0,
		MaxRetryAfter: 5 * time.Second,
		// Timer intentionally not set — must use real time for this test
	}
	rt := llm.NewRetryTransport(cfg, inner)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "POST", "http://example.com", nil)
	_, err := rt.RoundTrip(req)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, 1, inner.calls, "should only make one call before context expires")
}

func TestRetryTransport_502_Retried(t *testing.T) {
	resps, errs := makeResponses(502, 200)
	inner := &mockTransport{responses: resps, errs: errs}
	rt := llm.NewRetryTransport(fastCfg(5), inner)

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 2, inner.calls)
}

func TestRetryTransport_503_Retried(t *testing.T) {
	resps, errs := makeResponses(503, 503, 200)
	inner := &mockTransport{responses: resps, errs: errs}
	rt := llm.NewRetryTransport(fastCfg(5), inner)

	req, _ := http.NewRequest("POST", "http://example.com", nil)
	resp, err := rt.RoundTrip(req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 3, inner.calls)
}

func TestDefaultRetryConfig_Defaults(t *testing.T) {
	cfg := llm.DefaultRetryConfig()
	assert.Equal(t, 5, cfg.MaxRetries)
	assert.Equal(t, 500*time.Millisecond, cfg.BaseDelay)
	assert.Equal(t, 30*time.Second, cfg.MaxDelay)
	assert.Equal(t, 2.0, cfg.Multiplier)
	assert.True(t, cfg.Jitter)
	assert.Equal(t, 5*time.Second, cfg.MaxRetryAfter)
}
```

**Verification:**
```bash
go build ./...
go test -run TestRetryTransport ./...
go test -run TestDefaultRetryConfig ./...
```

> **Note:** `TestRetryTransport_ContextCancelled_DuringBackoff` does not set `Timer` and
> intentionally uses a real 500ms delay with a 50ms context deadline. It takes ~50ms to run.
> All other retry tests use `fastCfg` (with `Timer: fastTimer`) and complete instantly.

---

## Task 3 — Add `WithRetry` and `BuildHTTPClient` to `option.go`

**Files modified:** `option.go`  
**Estimated time:** 5 minutes  
**Depends on:** Task 2 (`RetryConfig` must exist)

### 3a — Add `Retry *RetryConfig` field to `Options` struct

In the `Options` struct, after the `Logger` field:

```go
	// Retry configures within-provider retry with exponential backoff for the
	// HTTP client built by BuildHTTPClient. Nil means no retry (use the shared
	// default client). Set via WithRetry().
	Retry *RetryConfig
```

### 3b — Add `WithRetry` option constructor (after `WithAPIKeyFunc`):

```go
// WithRetry configures within-provider retry with exponential backoff.
// The retry transport is inserted into the HTTP client's transport chain
// when BuildHTTPClient is called (i.e. when no explicit HTTPClient was set).
// Use DefaultRetryConfig() as a convenient starting point:
//
//	llm.WithRetry(llm.DefaultRetryConfig())
func WithRetry(cfg RetryConfig) Option {
	return func(o *Options) {
		o.Retry = &cfg
	}
}
```

### 3c — Add `BuildHTTPClient` helper method (after `ResolveAPIKey`):

```go
// BuildHTTPClient returns the HTTP client that providers should use for
// outgoing API requests. Resolution order:
//
//  1. A custom client supplied via WithHTTPClient — returned unchanged.
//  2. A custom retry config supplied via WithRetry — a new client is built
//     with the retry transport wired in.
//  3. The package-level default client (no retry, shared singleton).
func (o *Options) BuildHTTPClient() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	if o.Retry != nil {
		return NewHttpClient(HttpClientOpts{Retry: *o.Retry})
	}
	return DefaultHttpClient()
}
```

**Verification:**
```bash
go build ./...
go test ./... -run TestOption
```

---

## Task 4 — Migrate Category-A providers to `cfg.BuildHTTPClient()`

**Files modified:**
- `provider/anthropic/anthropic.go`
- `provider/ollama/ollama.go`
- `provider/openai/openai.go`
- `provider/openrouter/openrouter.go`

**Estimated time:** 8 minutes  
**Depends on:** Task 3

All four files have the identical pattern. Replace in each:

```go
	client := cfg.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}
```

with:

```go
	client := cfg.BuildHTTPClient()
```

**`provider/anthropic/anthropic.go`** — in `New()`:

Old:
```go
	cfg := llm.Apply(allOpts...)
	client := cfg.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}
	return &Provider{opts: cfg, client: client}
```

New:
```go
	cfg := llm.Apply(allOpts...)
	return &Provider{opts: cfg, client: cfg.BuildHTTPClient()}
```

Repeat the same change for `provider/ollama/ollama.go`, `provider/openai/openai.go`,
and `provider/openrouter/openrouter.go` — each has the identical three-line pattern in their `New()`
function.

**Verification:**
```bash
go build ./provider/anthropic/... ./provider/ollama/... ./provider/openai/... ./provider/openrouter/...
go test ./provider/anthropic/... ./provider/ollama/... ./provider/openai/... ./provider/openrouter/...
```

---

## Task 5 — Migrate Category-B providers (own option types)

**Files modified:**
- `provider/minimax/minimax.go`
- `provider/anthropic/claude/option.go`
- `provider/bedrock/bedrock.go`

**Estimated time:** 10 minutes  
**Depends on:** Task 3

These providers use their own `Option` type and a `WithLLMOptions`/`WithLLMOpts` function
that manually inspects `cfg.HTTPClient`. Extend each to also handle `cfg.Retry`.

### `provider/minimax/minimax.go`

`New()` — same Category-A pattern, replace:
```go
	cfg := llm.Apply(DefaultOptions()...)
	client := cfg.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}
	p := &Provider{opts: cfg, client: client}
```
with:
```go
	cfg := llm.Apply(DefaultOptions()...)
	p := &Provider{opts: cfg, client: cfg.BuildHTTPClient()}
```

`WithLLMOpts` — replace the explicit nil-check:
```go
		// Update HTTP client if provided
		if p.opts.HTTPClient != nil {
			p.client = p.opts.HTTPClient
		}
```
with:
```go
		// Rebuild HTTP client when custom client or retry config is supplied.
		if p.opts.HTTPClient != nil || p.opts.Retry != nil {
			p.client = p.opts.BuildHTTPClient()
		}
```

### `provider/anthropic/claude/option.go`

`WithLLMOptions` — replace:
```go
		if cfg.HTTPClient != nil {
			p.client = cfg.HTTPClient
		}
```
with:
```go
		if cfg.HTTPClient != nil || cfg.Retry != nil {
			p.client = cfg.BuildHTTPClient()
		}
```

### `provider/bedrock/bedrock.go`

`WithLLMOptions` — replace:
```go
		if cfg.HTTPClient != nil {
			p.httpClient = cfg.HTTPClient
		}
```
with:
```go
		if cfg.HTTPClient != nil || cfg.Retry != nil {
			p.httpClient = cfg.BuildHTTPClient()
		}
```

**Verification:**
```bash
go build ./provider/minimax/... ./provider/anthropic/claude/... ./provider/bedrock/...
go test ./provider/minimax/... ./provider/anthropic/claude/... ./provider/bedrock/...
```

---

## Task 6 — Rename `isRetriableError` → `isFailoverEligible` in `provider/router/routing.go`

**Files modified:** `provider/router/routing.go`  
**Estimated time:** 5 minutes  
**Depends on:** Task 1 (`IsFailoverEligibleHTTPStatus` must exist)

Replace the entire `isRetriableError` function:

```go
// isRetriableError checks if an error should trigger failover to the next target.
// Retriable errors: rate limits (429), service unavailable (503), quota exceeded.
func isRetriableError(pe *llm.ProviderError) bool {
	if pe == nil {
		return false
	}

	// Use the structured StatusCode field when available — no string matching needed.
	// 402: payment required / insufficient credits (e.g. OpenRouter out of funds).
	// 429: rate limited. 503: service unavailable.
	if pe.StatusCode == 402 || pe.StatusCode == 429 || pe.StatusCode == 503 {
		return true
	}
	...
}
```

With:

```go
// isFailoverEligible reports whether err should cause the router to abandon the
// current provider and try the next configured target.
//
// It returns true for rate limits, quota exhaustion, payment failures, and
// server errors — conditions where a different provider may succeed while this
// one cannot serve the request right now.
//
// Note: within-provider retries are handled upstream in the RetryTransport HTTP
// middleware. By the time this function is called the provider has already
// exhausted its retry budget.
func isFailoverEligible(pe *llm.ProviderError) bool {
	if pe == nil {
		return false
	}

	// Prefer the structured StatusCode field — no string matching needed.
	// 500 and 502 are included here because the RetryTransport will have already
	// retried them; reaching this point means retries were exhausted.
	if llm.IsFailoverEligibleHTTPStatus(pe.StatusCode) {
		return true
	}

	// Fall back to message heuristics for providers that don't set StatusCode.
	errMsg := strings.ToLower(pe.Error())

	retryPatterns := []string{
		"rate limit",
		"rate_limit",
		"ratelimit",
		"too many requests",
		"quota",
		"quota_exceeded",
		"quota exceeded",
		"service unavailable",
		"service_unavailable",
		"overloaded",
		"capacity",
		"temporarily unavailable",
		"try again",
	}

	for _, pattern := range retryPatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	retryRegexes := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(http\s+)?429\b`),
		regexp.MustCompile(`(?i)(http\s+)?503\b`),
		regexp.MustCompile(`(?i)insufficient.*quota`),
		regexp.MustCompile(`(?i)usage.*limit.*exceeded`),
		regexp.MustCompile(`(?i)request.*limit`),
	}

	for _, re := range retryRegexes {
		if re.MatchString(errMsg) {
			return true
		}
	}

	return false
}
```

**Key change in the status-code check:** Replace the explicit `pe.StatusCode == 402 || ...` with a
call to `llm.IsFailoverEligibleHTTPStatus(pe.StatusCode)`, which now includes 500 and 502.

**Verification:**
```bash
go build ./provider/router/...
```

---

## Task 7 — Update call site in `provider/router/router.go`

**Files modified:** `provider/router/router.go`  
**Estimated time:** 2 minutes  
**Depends on:** Task 6

In `CreateStream`, replace:
```go
		if isRetriableError(pe) {
```
with:
```go
		if isFailoverEligible(pe) {
```

**Verification:**
```bash
go build ./provider/router/...
```

---

## Task 8 — Add `Retry` shortcut to `ProviderInstanceConfig` and wire it in `router.New`

**Files modified:** `provider/router/config.go`, `provider/router/router.go`  
**Estimated time:** 8 minutes  
**Depends on:** Task 3 (`llm.RetryConfig` must exist), Task 7

### `provider/router/config.go`

Replace:
```go
// ProviderInstanceConfig configures a single provider instance.
type ProviderInstanceConfig struct {
	Name         string            // Unique instance name
	Type         string            // Provider type key (passed to factory)
	Options      []llm.Option      // Options passed to factory
	ModelAliases map[string]string // Local aliases: "sonnet" -> "claude-sonnet-4-5"
}
```

With:
```go
// ProviderInstanceConfig configures a single provider instance.
type ProviderInstanceConfig struct {
	Name         string            // Unique instance name
	Type         string            // Provider type key (passed to factory)
	Options      []llm.Option      // Options passed to factory
	ModelAliases map[string]string // Local aliases: "sonnet" -> "claude-sonnet-4-5"

	// Retry is a convenience shortcut for within-provider retry. When non-nil,
	// llm.WithRetry(*Retry) is appended to Options before the factory is called.
	// If Options already contains llm.WithRetry or llm.WithHTTPClient, those
	// take precedence because they are applied after this shortcut.
	Retry *llm.RetryConfig
}
```

### `provider/router/router.go`

In `New()`, in the provider creation loop, replace:
```go
		prov := factory(pcfg.Options...)
```
with:
```go
		opts := pcfg.Options
		if pcfg.Retry != nil {
			opts = append(opts, llm.WithRetry(*pcfg.Retry))
		}
		prov := factory(opts...)
```

**Verification:**
```bash
go build ./provider/router/...
go test ./provider/router/...
```

---

## Task 9 — Update router tests

**Files modified:** `provider/router/router_test.go`  
**Estimated time:** 8 minutes  
**Depends on:** Tasks 6, 7, 8

### 9a — Rename `TestIsRetriableError` → `TestIsFailoverEligible`

Replace the entire test function:
```go
func TestIsRetriableError(t *testing.T) {
	...
	result := isRetriableError(tt.pe)
	...
}
```

With:
```go
func TestIsFailoverEligible(t *testing.T) {
	mkpe := func(msg string, statusCode int) *llm.ProviderError {
		return &llm.ProviderError{
			Sentinel:   llm.ErrAPIError,
			Provider:   "test",
			Message:    msg,
			StatusCode: statusCode,
		}
	}
	tests := []struct {
		name      string
		pe        *llm.ProviderError
		wantTrue  bool
	}{
		// Status-code based — true
		{"429 rate limit",         mkpe("rate limit exceeded", 429), true},
		{"429 too many requests",  mkpe("too many requests", 429),   true},
		{"503 unavailable",        mkpe("service unavailable", 503), true},
		{"402 payment required",   mkpe("payment required", 402),    true},
		{"500 internal error",     mkpe("internal server error", 500), true},  // NEW
		{"502 bad gateway",        mkpe("bad gateway", 502),          true},  // NEW
		// Message heuristics — true (no status code)
		{"quota exceeded msg",     mkpe("quota exceeded", 0),         true},
		{"rate_limit msg",         mkpe("rate_limit", 0),             true},
		{"insufficient quota msg", mkpe("insufficient quota", 0),     true},
		{"usage limit msg",        mkpe("usage limit exceeded", 0),   true},
		// False
		{"401 auth failed",        mkpe("authentication failed", 401), false},
		{"403 forbidden",          mkpe("invalid API key", 403),       false},
		{"404 not found",          mkpe("model not found", 404),       false},
		{"400 bad request",        mkpe("bad request", 400),           false},
		{"nil",                    nil,                                 false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantTrue, isFailoverEligible(tt.pe))
		})
	}
}
```

### 9b — Add `RetryConfig` wiring test

Add a new test after `TestIsFailoverEligible`:

```go
func TestNew_RetryConfigWiredToFactory(t *testing.T) {
	var capturedOpts []llm.Option

	factory := func(opts ...llm.Option) llm.Provider {
		capturedOpts = opts
		return &mockProvider{
			name:   "test",
			models: llm.Models{{ID: "m1", Aliases: []string{"default"}}},
		}
	}

	retryCfg := llm.DefaultRetryConfig()
	_, err := New(Config{
		Providers: []ProviderInstanceConfig{
			{
				Name:  "p1",
				Type:  "test",
				Retry: &retryCfg,
			},
		},
	}, map[string]Factory{"test": factory})
	require.NoError(t, err)

	// Verify that WithRetry was appended to the factory options.
	cfg := llm.Apply(capturedOpts...)
	require.NotNil(t, cfg.Retry, "Retry should be wired through factory options")
	assert.Equal(t, retryCfg.MaxRetries, cfg.Retry.MaxRetries)
}
```

**Verification:**
```bash
go test ./provider/router/... -v -run "TestIsFailoverEligible|TestNew_RetryConfigWiredToFactory"
go test ./provider/router/...
```

---

## Task 10 — Final verification: full build + test suite

**Estimated time:** 3 minutes  
**Depends on:** all prior tasks

```bash
go build ./...
go vet ./...
go test ./...
```

Expected: all tests pass, zero vet warnings.

Also confirm the deprecated alias still compiles:

```bash
# Quick smoke-test that the old name still resolves
grep -r "IsRetriableHTTPStatus" provider/ --include="*.go" | grep -v "_test.go"
```

The four call sites in `anthropic`, `claude`, `minimax`, and `openrouter` should all still compile
unchanged because `IsRetriableHTTPStatus` is now a `var` alias for `IsFailoverEligibleHTTPStatus`.

---

## Summary of changes from draft → refined plan

| Issue | Severity | Fix applied |
|---|---|---|
| `time.After` goroutine leak | Bug | Extracted `sleep()` helper using `time.NewTimer` + `Stop()` |
| `"math"` import not needed | Build error | Removed; only `"context"`, `"math/rand"`, `"strconv"` are new |
| `"context"` import missing | Build error | Added to import list in Task 2a |
| 429+Retry-After test waits 1s | Slow test | Added `Timer func(d time.Duration) <-chan time.Time` to `RetryConfig`; `fastCfg` uses `fastTimer` (instant) |
| Missing 503 test | Coverage gap | Added `TestRetryTransport_503_Retried` |
| Semantic change not flagged | Documentation | Added note in Task 1 about 500/502 now returning from `CreateStream` |

---

## Summary

| # | Task | File(s) | Key change |
|---|---|---|---|
| 1 | `errors.go` | `errors.go` | Add `IsFailoverEligibleHTTPStatus` (500+502); alias `IsRetriableHTTPStatus` |
| 2 | RetryTransport | `http.go`, `http_retry_test.go` | `RetryConfig`, `RetryTransport`, `NewRetryTransport`, wire into `NewHttpClient` |
| 3 | Option integration | `option.go` | `WithRetry`, `BuildHTTPClient()`, `Retry *RetryConfig` in `Options` |
| 4 | Migrate providers A | 4 providers | `cfg.BuildHTTPClient()` replaces nil-check pattern |
| 5 | Migrate providers B | minimax, claude, bedrock | `WithLLMOptions`/`WithLLMOpts` updated for Retry |
| 6 | Router rename | `routing.go` | `isRetriableError` → `isFailoverEligible`, delegate to `IsFailoverEligibleHTTPStatus` |
| 7 | Router call site | `router.go` | One-line rename |
| 8 | Router config | `config.go`, `router.go` | `ProviderInstanceConfig.Retry`, wire via `WithRetry` in `New` |
| 9 | Router tests | `router_test.go` | Rename test, add 500/502 cases, add wiring test |
| 10 | Final verify | — | `go build ./...`, `go vet ./...`, `go test ./...` |
