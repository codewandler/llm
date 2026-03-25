package llm

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// HttpClientOpts configures the HTTP client created by NewHttpClient.
type HttpClientOpts struct {
	// Logger enables transport-level request/response logging at Debug level.
	// When nil, no logging is performed.
	Logger *slog.Logger

	// Debug extends logging to include request/response headers and bodies.
	// Has no effect when Logger is nil.
	Debug bool
}

// loggingTransport is an http.RoundTripper that logs every request and response.
type loggingTransport struct {
	wrapped http.RoundTripper
	logger  *slog.Logger
	debug   bool
}

// streamLogger is a write-closer that logs each Write call as a debug line,
// then forwards the data to the underlying writer so the eventPub flows through.
type streamLogger struct {
	underlying io.ReadCloser
	logger     *slog.Logger
	method     string
	url        string
}

func (s *streamLogger) Read(p []byte) (int, error) {
	n, err := s.underlying.Read(p)
	if n > 0 {
		s.logger.Debug("http response body",
			"method", s.method,
			"url", s.url,
			"chunk", string(p[:n]),
		)
	}
	return n, err
}

func (s *streamLogger) Close() error {
	return s.underlying.Close()
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	args := []any{
		"method", req.Method,
		"url", req.URL.String(),
	}

	if t.debug {
		// Log request headers
		for k, vs := range req.Header {
			for _, v := range vs {
				args = append(args, fmt.Sprintf("req.header.%s", k), v)
			}
		}
		// Log request body — read, log, then restore so the real transport still sees it.
		// Safe to buffer: the request body is always a complete JSON payload.
		if req.Body != nil {
			body, err := io.ReadAll(req.Body)
			_ = req.Body.Close()
			if err == nil {
				args = append(args, "req.body", string(body))
			}
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
	}

	t.logger.Debug("http request", args...)

	resp, err := t.wrapped.RoundTrip(req)
	if err != nil {
		t.logger.Debug("http error",
			"method", req.Method,
			"url", req.URL.String(),
			"duration", time.Since(start),
			"error", err,
		)
		return nil, err
	}

	respArgs := []any{
		"method", req.Method,
		"url", req.URL.String(),
		"status", resp.StatusCode,
		"duration", time.Since(start),
	}

	if t.debug {
		for k, vs := range resp.Header {
			for _, v := range vs {
				respArgs = append(respArgs, fmt.Sprintf("resp.header.%s", k), v)
			}
		}
	}

	t.logger.Debug("http response", respArgs...)

	if t.debug && resp.Body != nil {
		ct := resp.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "application/vnd.amazon.eventstream") {
			// Binary framing protocol — not human-readable, skip body logging.
			t.logger.Debug("http response body skipped (binary eventstream)",
				"method", req.Method,
				"url", req.URL.String(),
			)
		} else {
			// Wrap the response body in a tee so every chunk is logged as it is
			// read by the caller (SSE parser, JSON decoder, etc.). The eventPub is
			// not buffered — data flows through at the same rate as without logging.
			resp.Body = &streamLogger{
				underlying: resp.Body,
				logger:     t.logger,
				method:     req.Method,
				url:        req.URL.String(),
			}
		}
	}

	return resp, nil
}

// NewHttpClient creates a new *http.Client with sensible defaults for LLM
// provider use. The client has no top-level Timeout — LLM streams can be
// arbitrarily long and are cancelled via context. Transport-level timeouts
// guard against stalled connections at the TCP/TLS layer.
//
// When opts.Logger is non-nil, every request and response is logged at Debug
// level. Set opts.Debug = true to also include headers and bodies. Response
// bodies are tee-logged as they eventPub — no buffering, no broken SSE.
func NewHttpClient(opts HttpClientOpts) *http.Client {
	var transport http.RoundTripper = &http.Transport{
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    false,
	}
	if opts.Logger != nil {
		transport = &loggingTransport{
			wrapped: transport,
			logger:  opts.Logger,
			debug:   opts.Debug,
		}
	}
	return &http.Client{Transport: transport}
}

// defaultHttpClient is the package-level singleton used when no custom client
// is provided via WithHTTPClient.
var defaultHttpClient = NewHttpClient(HttpClientOpts{})

// DefaultHttpClient returns the shared default HTTP client. It is safe for
// concurrent use and is reused across all providers that do not supply their
// own client.
func DefaultHttpClient() *http.Client {
	return defaultHttpClient
}
