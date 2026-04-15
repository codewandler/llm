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

// RetryConfig controls RetryTransport behaviour. Zero values for numeric
// fields select package defaults. Note: MaxRetries=0 means "use default
// (2)", not "no retry" — zero is indistinguishable from unset.
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
