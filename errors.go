package llm

import (
	"errors"
	"fmt"
)

// Provider name constants used in ProviderError.Provider.
const (
	ProviderNameAnthropic  = "anthropic"
	ProviderNameClaude     = "claude"
	ProviderNameBedrock    = "bedrock"
	ProviderNameOllama     = "ollama"
	ProviderNameOpenAI     = "openai"
	ProviderNameOpenRouter = "openrouter"
)

// Sentinel errors for use with errors.Is. Each ProviderError wraps one of
// these so callers can inspect the error kind without string matching.
var (
	// ErrContextCancelled is returned when the caller's context is cancelled
	// while a stream is in progress.
	ErrContextCancelled = errors.New("context cancelled")

	// ErrRequestFailed is returned when the HTTP transport fails before a
	// response is received (e.g. network error, DNS failure).
	ErrRequestFailed = errors.New("request failed")

	// ErrAPIError is returned when the provider API responds with a non-2xx
	// HTTP status. The ProviderError carries StatusCode and Body.
	ErrAPIError = errors.New("API error")

	// ErrStreamRead is returned when reading or scanning the response stream
	// fails at the I/O level (e.g. scanner error, connection reset).
	ErrStreamRead = errors.New("stream read error")

	// ErrStreamDecode is returned when a stream chunk cannot be decoded
	// (e.g. malformed JSON in an SSE data line).
	ErrStreamDecode = errors.New("stream decode error")

	// ErrProviderError is returned when the provider sends an explicit
	// error inside the stream (e.g. Anthropic error event, OpenRouter
	// chunk-level error).
	ErrProviderError = errors.New("provider error")

	// ErrMissingAPIKey is returned when a provider requires an API key
	// but none has been configured.
	ErrMissingAPIKey = errors.New("missing API key")

	// ErrBuildRequest is returned when serialising the outgoing request
	// fails before it is sent.
	ErrBuildRequest = errors.New("build request error")
)

// ProviderError is a structured error emitted by any provider. It wraps a
// sentinel so errors.Is works, carries the provider name for identification,
// and optionally holds an HTTP status code and body for API errors.
type ProviderError struct {
	// Sentinel is one of the Err* vars above. errors.Is matches against it.
	Sentinel error

	// Provider is the name of the provider that produced this error.
	// Use the ProviderName* constants.
	Provider string

	// Message is a human-readable description of the error.
	Message string

	// Cause is the underlying error that triggered this one, if any.
	Cause error

	// StatusCode is the HTTP response status code. Only set for ErrAPIError.
	StatusCode int

	// Body is the raw HTTP response body. Only set for ErrAPIError.
	Body string
}

// Error returns a human-readable error string in the form:
// "<provider>: <sentinel>: <message>" or "<provider>: <sentinel>: <message>: <cause>"
func (e *ProviderError) Error() string {
	base := fmt.Sprintf("%s: %s: %s", e.Provider, e.Sentinel.Error(), e.Message)
	if e.Cause != nil {
		return base + ": " + e.Cause.Error()
	}
	return base
}

// Unwrap returns Cause when set, allowing errors.As/Is to traverse the chain.
// When Cause is nil, Unwrap returns Sentinel so errors.Is(err, ErrAPIError)
// still works even with no underlying cause.
func (e *ProviderError) Unwrap() error {
	if e.Cause != nil {
		return e.Cause
	}
	return e.Sentinel
}

// Is reports whether this error matches target. It matches if target is the
// same sentinel, enabling errors.Is(err, ErrAPIError) etc.
func (e *ProviderError) Is(target error) bool {
	return target == e.Sentinel
}

// --- Constructors ---

// NewErrContextCancelled wraps a context cancellation for a provider stream.
func NewErrContextCancelled(provider string, cause error) *ProviderError {
	return &ProviderError{
		Sentinel: ErrContextCancelled,
		Provider: provider,
		Message:  "context cancelled",
		Cause:    cause,
	}
}

// NewErrRequestFailed wraps an HTTP transport-level failure.
func NewErrRequestFailed(provider string, cause error) *ProviderError {
	return &ProviderError{
		Sentinel: ErrRequestFailed,
		Provider: provider,
		Message:  "request failed",
		Cause:    cause,
	}
}

// NewErrAPIError wraps a non-2xx HTTP response from a provider API.
func NewErrAPIError(provider string, statusCode int, body string) *ProviderError {
	return &ProviderError{
		Sentinel:   ErrAPIError,
		Provider:   provider,
		Message:    fmt.Sprintf("HTTP %d", statusCode),
		StatusCode: statusCode,
		Body:       body,
	}
}

// NewErrStreamRead wraps an I/O or scanner error that occurred while reading
// the response stream.
func NewErrStreamRead(provider string, cause error) *ProviderError {
	return &ProviderError{
		Sentinel: ErrStreamRead,
		Provider: provider,
		Message:  "stream read error",
		Cause:    cause,
	}
}

// NewErrStreamDecode wraps a JSON or protocol decode failure mid-stream.
func NewErrStreamDecode(provider string, cause error) *ProviderError {
	return &ProviderError{
		Sentinel: ErrStreamDecode,
		Provider: provider,
		Message:  "stream decode error",
		Cause:    cause,
	}
}

// NewErrProviderMsg wraps an explicit error message sent by the provider
// inside the stream (e.g. an Anthropic error event or OpenRouter chunk error).
func NewErrProviderMsg(provider string, msg string) *ProviderError {
	return &ProviderError{
		Sentinel: ErrProviderError,
		Provider: provider,
		Message:  msg,
	}
}

// NewErrMissingAPIKey returns an error for a provider that has no API key
// configured.
func NewErrMissingAPIKey(provider string) *ProviderError {
	return &ProviderError{
		Sentinel: ErrMissingAPIKey,
		Provider: provider,
		Message:  "API key is not configured",
	}
}

// NewErrBuildRequest wraps a failure that occurred while building the
// outgoing request (e.g. JSON serialisation error).
func NewErrBuildRequest(provider string, cause error) *ProviderError {
	return &ProviderError{
		Sentinel: ErrBuildRequest,
		Provider: provider,
		Message:  "failed to build request",
		Cause:    cause,
	}
}
