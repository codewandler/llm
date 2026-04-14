# DESIGN: Within-Provider Retry with Exponential Backoff

**Date:** 2025-07  
**Status:** Approved

---

## Problem Statement

The router currently fails over to the *next provider* on the first retriable error (429, 503, 402).
For transient errors like HTTP 500 this is wasteful: the same provider will likely recover in
milliseconds, but we're handing the request to a completely different backend.

Two separate concerns are currently conflated:

1. **Within-provider retry** — should we try the same provider again? (e.g. 500, 502, network blip)
2. **Cross-provider failover** — after giving up on one provider, should the router try the next? (e.g. 429, 503, 402)

Additionally, `isRetriableError` in `routing.go` is poorly named. It answers "should the router try
the next target?" — which is really "is this error *failover-eligible*?"

---

## Goals

1. Retry the same provider on transient errors with **exponential backoff + jitter**.
2. Default max retries: **5** (configurable down to 0 to disable).
3. Implement as an **`http.RoundTripper` middleware** — composable, provider-agnostic, zero changes to individual provider stream parsers.
4. Respect the `Retry-After` response header (429 and 503).
5. After retries are exhausted, the final error propagates to the router's failover logic unchanged.
6. Rename `isRetriableError` → `isFailoverEligible` throughout.
7. No per-stream (mid-SSE) retry — only pre-stream response errors (non-2xx before body streaming begins).

## Non-Goals

- Retrying after SSE streaming has already started.
- Circuit breaker (separate concern).
- Per-model retry configuration.
- Automatic retry of `POST` bodies larger than a configured threshold.

---

## Terminology

| Term | Meaning |
|---|---|
| **within-provider retry** | Re-issue the same request to the same provider after a transient failure |
| **failover-eligible** | An error that, after retries, should cause the router to try the next configured provider |
| **retry transport** | An `http.RoundTripper` wrapper that handles within-provider retry logic |

---

## Architecture

```
[Provider.CreateStream]
       │
       │ p.client.Do(req)
       ▼
┌─────────────────────────────────────────┐
│  loggingTransport (optional)            │  ← logs every attempt individually
│  ┌──────────────────────────────────┐   │
│  │  retryTransport (new)            │   │  ← absorbs N-1 transient failures
│  │  ┌───────────────────────────┐   │   │
│  │  │  decompressingTransport   │   │   │
│  │  │  ┌──────────────────────┐ │   │   │
│  │  │  │  http.Transport      │ │   │   │
│  │  │  └──────────────────────┘ │   │   │
│  │  └───────────────────────────┘   │   │
│  └──────────────────────────────────┘   │
└─────────────────────────────────────────┘
       │
       │ Returns final (*http.Response, error) to provider
       ▼
[Provider checks status code, builds ProviderError]
       │
       │ Returns *ProviderError from CreateStream
       ▼
[Router: isFailoverEligible(pe) → try next target or give up]
```

`loggingTransport` wraps `retryTransport` so **each retry attempt is individually logged** at Debug
level with its status code and attempt number.

---

## Detailed Design

### 1. `RetryConfig` and `RetryTransport` — `http.go`

```go
// RetryConfig controls within-provider retry behaviour for the retryTransport.
// A zero value disables retries (MaxRetries == 0).
type RetryConfig struct {
    // MaxRetries is the maximum number of additional attempts after the first
    // failure. 0 disables retries. Default (via DefaultRetryConfig): 5.
    MaxRetries int

    // BaseDelay is the initial backoff duration. Default: 500ms.
    BaseDelay time.Duration

    // MaxDelay caps the computed backoff. Default: 30s.
    MaxDelay time.Duration

    // Multiplier is the exponential factor applied each attempt. Default: 2.0.
    Multiplier float64

    // Jitter adds ±25% random spread to the computed delay to avoid
    // thundering-herd on simultaneous retries. Default: true.
    Jitter bool

    // MaxRetryAfter is the maximum Retry-After duration that still qualifies
    // for a within-provider retry. If the header value exceeds this threshold,
    // or if a 429 carries no Retry-After header at all, the transport returns
    // the response immediately so the router can fail over.
    // Default: 5s. Rationale: short windows (≤5s) are worth absorbing locally;
    // longer windows make failover the better choice.
    MaxRetryAfter time.Duration

    // ShouldRetry is called with the response and/or error from each attempt.
    // Return true to consume the response and retry; false to return to caller.
    // When nil, DefaultShouldRetry is used.
    ShouldRetry func(resp *http.Response, err error) bool
}

// DefaultRetryConfig returns the recommended retry configuration.
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
// It signals which responses are *candidates* for within-provider retry:
//
//   - err != nil  — transport/network error (connection reset, DNS, timeout).
//     Safe to retry because the provider never received a complete response.
//   - 429         — rate limited. ShouldRetry returns true, but the transport
//     loop applies an additional guard: it only retries if Retry-After is
//     present AND ≤ MaxRetryAfter. Without that header (or with a long wait)
//     the response is returned immediately for router failover.
//   - 500, 502, 503 — transient server errors; retry with exponential backoff.
func DefaultShouldRetry(resp *http.Response, err error) bool {
    if err != nil {
        return true // transport error — provider never processed request
    }
    switch resp.StatusCode {
    case 429, 500, 502, 503:
        return true
    }
    return false
}
```

**`RetryTransport.RoundTrip` algorithm:**

```
var delay time.Duration
for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
    // ── sleep before every retry (not the first attempt) ──────────────────
    if attempt > 0 {
        select {
        case <-time.After(delay):
        case <-req.Context().Done():
            return nil, req.Context().Err()
        }
        req.Body, _ = req.GetBody()  // replay body
    }

    resp, err = rt.wrapped.RoundTrip(req)

    if !cfg.ShouldRetry(resp, err) {
        break  // success or a non-retriable error — return immediately
    }

    // ── decide whether and how long to wait before next attempt ───────────
    var retryAfter time.Duration
    if resp != nil {
        retryAfter = parseRetryAfterHeader(resp)  // 0 if header absent
        resp.Body.Close()                         // drain before retrying
    }

    if retryAfter > 0 {
        // Provider told us exactly when to retry.
        if retryAfter > cfg.MaxRetryAfter {
            break  // wait is too long — let the router fail over
        }
        delay = retryAfter
    } else if resp != nil && resp.StatusCode == 429 {
        // Rate-limited but no Retry-After header.
        // We don't know the rate-limit window, so don't guess — fail over.
        break
    } else {
        // Transport error or 500/502/503 without Retry-After.
        // Use our own exponential backoff.
        delay = computeExpBackoff(attempt+1, cfg)  // capped at MaxDelay
    }
}
return resp, err  // caller sees the FINAL response/error, unchanged
```

**429 decision table:**

| `Retry-After` header | Value vs `MaxRetryAfter` (5s default) | Action |
|---|---|---|
| Present | ≤ 5s | Retry after that duration |
| Present | > 5s | Return 429 response → router failover |
| Absent | — | Return 429 response → router failover |

**Key invariant:** `RetryTransport` returns the exact `(*http.Response, error)` pair from the last
attempt, identical to what the provider would have received without retries. No new error types are
introduced; the existing `ProviderError` + status-code path in each provider works as-is.

**Body replay requirement:** The transport requires `req.GetBody` to be set when `req.Body != nil`.
All current providers use `bytes.NewReader(body)` with `http.NewRequestWithContext`, which
automatically populates `GetBody`. No provider changes required for this.

### 2. Integration into `HttpClientOpts` / `NewHttpClient` — `http.go`

```go
type HttpClientOpts struct {
    Logger                *slog.Logger  // existing
    Debug                 bool          // existing
    TLSHandshakeTimeout   time.Duration // existing
    ResponseHeaderTimeout time.Duration // existing

    // Retry configures within-provider retry. Zero value (MaxRetries == 0)
    // disables retries. Use DefaultRetryConfig() for sensible defaults.
    Retry RetryConfig  // NEW
}
```

Transport chain construction in `NewHttpClient`:

```go
var transport http.RoundTripper = &http.Transport{...}
transport = &decompressingTransport{wrapped: transport}
if opts.Retry.MaxRetries > 0 {
    transport = newRetryTransport(opts.Retry, transport)  // NEW: inserted here
}
if opts.Logger != nil {
    transport = &loggingTransport{wrapped: transport, ...}
}
return &http.Client{Transport: transport}
```

`loggingTransport` remains outermost so every individual attempt (including retries) appears in
logs, with each attempt's HTTP status and duration independently visible.

### 3. `llm.Option` integration — `option.go`

```go
// Retry holds within-provider retry configuration. Nil means use the provider
// default (usually no retry unless a custom HTTP client is supplied).
// Set via WithRetry().
Retry *RetryConfig  // added to Options struct

// WithRetry configures within-provider retry with exponential backoff.
// Use llm.DefaultRetryConfig() as a starting point.
func WithRetry(cfg RetryConfig) Option {
    return func(o *Options) { o.Retry = &cfg }
}

// BuildHTTPClient returns the HTTP client to use for provider requests.
// If a custom client was supplied via WithHTTPClient it is returned unchanged.
// If WithRetry was used, a new client is built incorporating the retry transport.
// Otherwise the shared default client is returned.
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

Every provider replaces:
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

This is the only provider-level change required.

### 4. Rename: `isRetriableError` → `isFailoverEligible` — `provider/router/routing.go`

```go
// isFailoverEligible reports whether err should cause the router to abandon the
// current provider and try the next configured target. It is true for rate limits,
// quota exhaustion, service unavailability, and server errors — conditions where
// a different provider may succeed while this one cannot serve the request right now.
//
// Note: within-provider retries are handled upstream in the HTTP transport layer
// (RetryTransport). By the time this function is called, the provider has already
// exhausted its retries.
func isFailoverEligible(pe *llm.ProviderError) bool { ... }
```

Status codes handled (expanded from current):

| Code | Reason | Was before? |
|---|---|---|
| 402 | Payment required / quota | ✅ yes |
| 429 | Rate limited | ✅ yes |
| 500 | Internal server error | 🆕 new |
| 502 | Bad gateway | 🆕 new |
| 503 | Service unavailable | ✅ yes |

The string-heuristic fallback patterns remain unchanged; they are a backstop for providers that
don't set `StatusCode` on the `ProviderError`.

Call site in `router.go` is a one-line rename: `isRetriableError` → `isFailoverEligible`.

### 5. Rename: `IsRetriableHTTPStatus` → `IsFailoverEligibleHTTPStatus` — `errors.go`

```go
// IsFailoverEligibleHTTPStatus reports whether an HTTP status code indicates a
// condition where retrying with a different provider may succeed after the
// within-provider retry budget has been exhausted.
func IsFailoverEligibleHTTPStatus(code int) bool {
    switch code {
    case http.StatusPaymentRequired,    // 402 — out of credits
        http.StatusTooManyRequests,     // 429 — rate limited
        http.StatusInternalServerError, // 500 — server error (retried upstream)
        http.StatusBadGateway,          // 502 — gateway error
        http.StatusServiceUnavailable:  // 503 — temporarily down
        return true
    }
    return false
}

// IsRetriableHTTPStatus is deprecated; use IsFailoverEligibleHTTPStatus.
// It will be removed in a future version.
var IsRetriableHTTPStatus = IsFailoverEligibleHTTPStatus
```

All four provider call sites (`anthropic`, `claude`, `minimax`, `openrouter`) continue to compile
without change via the alias; they can be migrated to the new name at leisure.

### 6. Router `ProviderInstanceConfig` — `provider/router/config.go`

```go
type ProviderInstanceConfig struct {
    Name         string
    Type         string
    Options      []llm.Option
    ModelAliases map[string]string

    // Retry is a convenience shortcut. When non-nil, llm.WithRetry(*Retry) is
    // appended to Options before the factory is called. Options takes precedence:
    // if Options already contains a WithRetry or WithHTTPClient, this field is
    // ignored.
    Retry *llm.RetryConfig  // NEW
}
```

In `router.New`, before calling the factory:
```go
opts := pcfg.Options
if pcfg.Retry != nil {
    opts = append(opts, llm.WithRetry(*pcfg.Retry))
}
prov := factory(opts...)
```

This lets callers opt in per-provider without constructing an HTTP client manually:

```go
router.Config{
    Providers: []router.ProviderInstanceConfig{
        {
            Name:  "anthropic",
            Type:  "anthropic",
            Retry: llm.Ptr(llm.DefaultRetryConfig()),  // 5 retries, exp backoff
        },
        {
            Name: "openrouter-fallback",
            Type: "openrouter",
            // no retry — fail over immediately
        },
    },
}
```

---

## Error Flow: End-to-End Example

**Scenario:** Anthropic returns HTTP 500 twice, then succeeds on the 3rd attempt.

```
attempt 1:  POST /v1/messages → 500  (retryTransport: retry in 500ms)
attempt 2:  POST /v1/messages → 500  (retryTransport: retry in 1000ms)
attempt 3:  POST /v1/messages → 200  ✓
Provider.CreateStream returns (stream, nil)
Router forwards stream to caller — no failover event emitted
```

**Scenario:** Anthropic returns HTTP 500 on all 6 attempts (max retries = 5).

```
attempt 1–6: POST /v1/messages → 500 each time
retryTransport returns final (*Response{500}, nil) to provider
Provider: IsFailoverEligibleHTTPStatus(500) == true
Provider: pub.Close(); return nil, &ProviderError{StatusCode: 500, Sentinel: ErrAPIError}
Router: isFailoverEligible(pe) == true → try next target
If next target succeeds: ProviderFailoverEvent emitted, stream returned
If all targets fail: NewErrAllProvidersFailed → error returned to caller
```

---

## Files Changed

| File | Change |
|---|---|
| `http.go` | Add `RetryConfig`, `DefaultRetryConfig`, `DefaultShouldRetry`, `retryTransport`; update `HttpClientOpts`, `NewHttpClient` |
| `option.go` | Add `Retry *RetryConfig` to `Options`; add `WithRetry`; add `BuildHTTPClient()` helper |
| `errors.go` | Add `IsFailoverEligibleHTTPStatus` (expanded); keep `IsRetriableHTTPStatus` as deprecated alias |
| `provider/router/routing.go` | Rename `isRetriableError` → `isFailoverEligible`; add 500, 502 to status checks |
| `provider/router/router.go` | Call site rename |
| `provider/router/router_test.go` | Rename test helper call; add 500/502 test cases |
| `provider/router/config.go` | Add `Retry *llm.RetryConfig` to `ProviderInstanceConfig` |
| `provider/anthropic/anthropic.go` | Use `cfg.BuildHTTPClient()` |
| `provider/anthropic/claude/provider.go` | Use `cfg.BuildHTTPClient()` |
| `provider/minimax/minimax.go` | Use `cfg.BuildHTTPClient()` |
| `provider/openrouter/openrouter.go` | Use `cfg.BuildHTTPClient()` |
| *(other providers with HTTP client pattern)* | Use `cfg.BuildHTTPClient()` |

---

## Decisions (resolved)

| # | Question | Decision |
|---|---|---|
| 1 | 429 retry policy | Retry only when `Retry-After` is present **and** ≤ `MaxRetryAfter` (default 5s). No header → failover immediately. Header too long → failover immediately. |
| 2 | Transport errors (`err != nil`) | **Yes**, retry — the provider never processed the request so retrying is safe. |
| 3 | Export `NewRetryTransport` | **Yes** — expose `NewRetryTransport(cfg RetryConfig, wrapped http.RoundTripper) *RetryTransport` for custom client composition. |

---

## Acceptance Criteria

- [ ] `RetryTransport` retries up to `MaxRetries` times on configured status codes.
- [ ] Exponential backoff with jitter is applied between attempts.
- [ ] 429 with `Retry-After` ≤ `MaxRetryAfter`: retried after that duration.
- [ ] 429 with `Retry-After` > `MaxRetryAfter`: returned immediately for router failover.
- [ ] 429 with no `Retry-After` header: returned immediately for router failover.
- [ ] 500/502/503: retried with exponential backoff regardless of headers.
- [ ] Context cancellation aborts retry sleep immediately.
- [ ] After max retries, the final response is returned unchanged to the provider.
- [ ] `isFailoverEligible` (renamed) returns true for 500 and 502 in addition to existing codes.
- [ ] `ProviderInstanceConfig.Retry` wires retry config through the factory options.
- [ ] All existing router failover tests pass.
- [ ] New unit tests for `RetryTransport` (happy path, exhaustion, Retry-After, ctx cancel).
- [ ] `IsRetriableHTTPStatus` alias compiles and is marked deprecated.
