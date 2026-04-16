package cmds

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/cmd/llmcli/store"
	"github.com/codewandler/llm/provider/auto"
	"github.com/codewandler/llm/provider/router"
)

// RootFlags holds flags defined on the root command that are shared across
// all subcommands.
type RootFlags struct {
	// Debug enables provider-level debug logging.
	Debug bool
	// LogHTTP enables HTTP request/response logging at Debug level.
	LogHTTP bool
	// LogHTTPDebug additionally logs request/response headers and bodies.
	// Has no effect unless LogHTTP is also true.
	LogHTTPDebug bool
	// LogHTTPAllHeaders disables the response header allowlist and prints all headers.
	// Has no effect unless LogHTTPDebug is also true.
	LogHTTPAllHeaders bool
	// LogEvents prints each StreamEvent as JSON to stderr as it is consumed
	// from the channel, before any other handling. Useful for debugging the
	// provider event stream at the application layer.
	LogEvents bool
}

// BuildHTTPClient constructs an *http.Client from the root flags.
// Returns nil when no HTTP logging is requested, causing providers to fall
// back to llm.DefaultHttpClient(). The returned handler may be non-nil even
// when the client is nil — callers should check the client first.
func (f *RootFlags) BuildHTTPClient() (*http.Client, *httpLogHandler) {
	if !f.LogHTTP && !f.LogHTTPDebug && !f.LogHTTPAllHeaders {
		return nil, nil
	}
	debug := f.LogHTTPDebug || f.LogHTTPAllHeaders
	handler := newHTTPLogHandler(f.LogHTTPAllHeaders)
	client := llm.NewHttpClient(llm.HttpClientOpts{
		Logger: slog.New(handler),
		Debug:  debug,
	})
	return client, handler
}

// BuildLLMOptions returns llm.Option values for the root flags to be passed
// to providers, including ones that don't use the HTTP transport for logging
// (e.g. Bedrock).
func (f *RootFlags) BuildLLMOptions(handler *httpLogHandler) []llm.Option {
	var logger *slog.Logger
	switch {
	case handler != nil:
		logger = slog.New(handler)
	case f.Debug:
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	default:
		logger = nil
	}
	if logger == nil {
		return nil
	}
	if handler == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
		if f.Debug {
			logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
		}
	}
	return []llm.Option{llm.WithLogger(logger)}
}

// createProvider builds the aggregate provider from available credentials.
// httpClient overrides the default transport (e.g. for logging); pass nil to
// use llm.DefaultHttpClient(). llmOpts are passed to providers that log
// outside the HTTP transport layer (e.g. Bedrock).
func createProvider(ctx context.Context, httpClient *http.Client, llmOpts ...llm.Option) (*router.Provider, error) {
	tokenStore, err := getTokenStore()
	if err != nil {
		return nil, err
	}

	autoOpts := []auto.Option{
		auto.WithName("llmcli"),
		auto.WithClaude(tokenStore),
		auto.WithGlobalAlias("minimax", "minimax/minimax:2.7"),
	}

	if httpClient != nil {
		autoOpts = append(autoOpts, auto.WithHTTPClient(httpClient))
	}
	if len(llmOpts) > 0 {
		autoOpts = append(autoOpts, auto.WithLLMOptions(llmOpts...))
	}

	return auto.New(ctx, autoOpts...)
}

func getTokenStore() (*store.FileTokenStore, error) {
	dir, err := store.DefaultDir()
	if err != nil {
		return nil, err
	}
	return store.NewFileTokenStore(dir)
}
