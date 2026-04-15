// api/apicore/retry_test.go
package apicore_test

import (
	"bytes"
	"context"
	"fmt"
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
	// Note: uses a real 50ms delay. If flaky in CI, increase "0.05" and 40ms together.
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

func TestRetryTransport_ContextCancelledDuringBackoff(t *testing.T) {
	calls := 0
	base := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: 429,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	body := []byte(`{}`)
	tr := apicore.NewRetryTransport(base, apicore.RetryConfig{
		MaxRetries:     2,
		InitialBackoff: 500 * time.Millisecond,
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://x", bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }

	cancel() // cancel before retry wait
	resp, err := tr.RoundTrip(req)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotNil(t, resp) // first response is returned alongside cancellation error
	assert.Equal(t, 1, calls)
}

func TestRetryTransport_TransportErrorReturned(t *testing.T) {
	tErr := fmt.Errorf("dial tcp: connection refused")
	base := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, tErr
	})
	tr := apicore.NewRetryTransport(base, apicore.RetryConfig{MaxRetries: 2})
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://x", nil)
	resp, err := tr.RoundTrip(req)
	require.Error(t, err)
	assert.ErrorIs(t, err, tErr)
	assert.Nil(t, resp)
}
