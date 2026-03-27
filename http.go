package llm

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
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
		// Disable automatic decompression — we handle gzip, deflate, br, and
		// zstd ourselves in decompressingTransport so we can support brotli
		// and zstd which the standard Transport doesn't handle.
		DisableCompression: true,
	}
	// Decompressing transport handles gzip, deflate, brotli (br), and zstd.
	transport = &decompressingTransport{wrapped: transport}
	if opts.Logger != nil {
		transport = &loggingTransport{
			wrapped: transport,
			logger:  opts.Logger,
			debug:   opts.Debug,
		}
	}
	return &http.Client{Transport: transport}
}

// decompressingTransport wraps an http.RoundTripper and automatically
// decompresses response bodies based on Content-Encoding.
// Go's standard Transport handles gzip and deflate, but not brotli (br)
// or zstd. This transport adds support for those.
type decompressingTransport struct {
	wrapped http.RoundTripper
}

func (t *decompressingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.wrapped.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}

	// Remove Content-Length since decompressed body size is unknown
	resp.Header.Del("Content-Length")

	encoding := resp.Header.Get("Content-Encoding")
	switch encoding {
	case "br":
		// Brotli decompression
		resp.Body = &brotliReadCloser{underlying: resp.Body}
	case "zstd":
		// Zstd decompression
		decoder, err := zstd.NewReader(resp.Body)
		if err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("zstd decompression: %w", err)
		}
		resp.Body = &zstdReadCloser{decoder: decoder, underlying: resp.Body}
	case "gzip":
		// We handle gzip ourselves since DisableCompression is true.
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("gzip decompression: %w", err)
		}
		resp.Body = &gzipReadCloser{Reader: gz, underlying: resp.Body}
	case "deflate":
		// We handle deflate ourselves since DisableCompression is true.
		resp.Body = &flateReadCloser{underlying: resp.Body}
	}

	return resp, nil
}

// brotliReadCloser wraps brotli decompression around a ReadCloser.
type brotliReadCloser struct {
	underlying io.ReadCloser
	reader     *brotli.Reader
}

func (b *brotliReadCloser) Read(p []byte) (int, error) {
	if b.reader == nil {
		b.reader = brotli.NewReader(b.underlying)
	}
	return b.reader.Read(p)
}

func (b *brotliReadCloser) Close() error {
	b.reader = nil
	return b.underlying.Close()
}

// zstdReadCloser wraps zstd decompression around a ReadCloser.
type zstdReadCloser struct {
	decoder   *zstd.Decoder
	underlying io.ReadCloser
}

func (z *zstdReadCloser) Read(p []byte) (int, error) {
	return z.decoder.Read(p)
}

func (z *zstdReadCloser) Close() error {
	z.decoder.Close()
	return z.underlying.Close()
}

// gzipReadCloser wraps gzip decompression and tracks the underlying body for closing.
type gzipReadCloser struct {
	*gzip.Reader
	underlying io.ReadCloser
}

func (g *gzipReadCloser) Close() error {
	g.Reader.Close()
	return g.underlying.Close()
}

// flateReadCloser wraps flate decompression around a ReadCloser.
type flateReadCloser struct {
	underlying io.ReadCloser
	reader     io.ReadCloser
}

func (f *flateReadCloser) Read(p []byte) (int, error) {
	if f.reader == nil {
		f.reader = flate.NewReader(f.underlying)
	}
	return f.reader.Read(p)
}

func (f *flateReadCloser) Close() error {
	f.reader = nil
	return f.underlying.Close()
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
