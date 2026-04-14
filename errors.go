package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Provider name constants used in ProviderError.Provider.
const (
	ProviderNameAnthropic  = "anthropic"
	ProviderNameClaude     = "claude"
	ProviderNameBedrock    = "bedrock"
	ProviderNameChatGPT    = "chatgpt"
	ProviderNameOllama     = "ollama"
	ProviderNameOpenAI     = "openai"
	ProviderNameOpenRouter = "openrouter"
	ProviderNameRouter     = "router"
)

// Sentinel errors for use with errors.Is. Each ProviderError wraps one of
// these so callers can inspect the error kind without string matching.
var (
	// ErrContextCancelled is returned when the caller's context is cancelled
	// while a eventPub is in progress.
	ErrContextCancelled = errors.New("context cancelled")

	// ErrRequestFailed is returned when the HTTP transport fails before a
	// response is received (e.g. network error, DNS failure).
	ErrRequestFailed = errors.New("request failed")

	// ErrAPIError is returned when the provider API responds with a non-2xx
	// HTTP status. The ProviderError carries StatusCode and Body.
	ErrAPIError = errors.New("API error")

	// ErrStreamRead is returned when reading or scanning the response eventPub
	// fails at the I/O level (e.g. scanner error, connection reset).
	ErrStreamRead = errors.New("eventPub read/decode error")

	// ErrStreamDecode is returned when a eventPub chunk cannot be decoded
	// (e.g. malformed JSON in an SSE data line).
	ErrStreamDecode = errors.New("eventPub read/decode error")

	// ErrProviderError is returned when the provider sends an explicit
	// error inside the eventPub (e.g. Anthropic error event, OpenRouter
	// chunk-level error).
	ErrProviderError = errors.New("provider error")

	// ErrMissingAPIKey is returned when a provider requires an API key
	// but none has been configured.
	ErrMissingAPIKey = errors.New("missing API key")

	// ErrBuildRequest is returned when serialising the outgoing request
	// fails before it is sent.
	ErrBuildRequest = errors.New("build request error")

	// ErrUnknownModel is returned when a model ToolCallID or alias cannot be resolved.
	ErrUnknownModel = errors.New("unknown model")

	// ErrNoProviders is returned when no providers are configured or all
	// failover targets have been exhausted.
	ErrNoProviders = errors.New("no providers configured")

	// ErrUnknown is used to wrap any error that is not already a ProviderError.
	// Callers can test for it with errors.Is(err, llm.ErrUnknown).
	ErrUnknown = errors.New("unknown error")
)

// ProviderError is a structured error emitted by any provider. It wraps a
// sentinel so errors.Is works, carries the provider name for identification,
// and optionally holds an HTTP status code and body for API errors.
type ProviderError struct {
	// Sentinel is one of the Err* vars above. errors.Is matches against it.
	Sentinel error `json:"-"`

	// Provider is the name of the provider that produced this error.
	// Use the ProviderName* constants.
	Provider string `json:"provider"`

	// Message is a human-readable description of the error.
	Message string `json:"message"`

	// Cause is the underlying error that triggered this one, if any.
	Cause error `json:"-"`

	// RequestBody is the raw HTTP request body. Only set for ErrBuildRequest.
	RequestBody string `json:"request_body,omitempty"`

	// StatusCode is the HTTP response status code. Only set for ErrAPIError.
	StatusCode int `json:"status_code,omitempty"`

	// Body is the raw HTTP response body. Only set for ErrAPIError.
	ResponseBody string `json:"response_body,omitempty"`
}

func (e *ProviderError) WithRequestBody(body string) *ProviderError {
	e.RequestBody = body
	return e
}

// Error returns a human-readable error string in the form:
// "<provider>: <sentinel>" or "<provider>: <sentinel>: <message>" (with optional ": <cause>" suffix).
func (e *ProviderError) Error() string {
	var base string
	if e.Message == "" {
		base = fmt.Sprintf("%s: %s", e.Provider, e.Sentinel.Error())
	} else {
		base = fmt.Sprintf("%s: %s: %s", e.Provider, e.Sentinel.Error(), e.Message)
	}
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

// MarshalJSON serialises ProviderError to JSON. Sentinel and Cause are
// rendered as strings so the full error is machine-readable.
func (e *ProviderError) MarshalJSON() ([]byte, error) {
	type wire struct {
		Sentinel   string `json:"sentinel"`
		Provider   string `json:"provider"`
		Message    string `json:"message"`
		Cause      string `json:"cause,omitempty"`
		StatusCode int    `json:"status_code,omitempty"`
		Body       string `json:"body,omitempty"`
	}
	w := wire{
		Provider:   e.Provider,
		Message:    e.Message,
		StatusCode: e.StatusCode,
		Body:       e.ResponseBody,
	}
	if e.Sentinel != nil {
		w.Sentinel = e.Sentinel.Error()
	}
	if e.Cause != nil {
		w.Cause = e.Cause.Error()
	}
	return json.Marshal(w)
}

// --- Constructors ---

// NewErrContextCancelled wraps a context cancellation for a provider eventPub.
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

// NewErrAPIErrorWithRequest wraps a non-2xx HTTP response from a provider API.
func NewErrAPIErrorWithRequest(provider string, requestBody string, statusCode int, responseBody string) *ProviderError {
	return &ProviderError{
		Sentinel:     ErrAPIError,
		Provider:     provider,
		Message:      fmt.Sprintf("HTTP %d response %s", statusCode, responseBody),
		StatusCode:   statusCode,
		RequestBody:  requestBody,
		ResponseBody: responseBody,
	}
}

// NewErrAPIError wraps a non-2xx HTTP response from a provider API.
func NewErrAPIError(provider string, statusCode int, responseBody string) *ProviderError {
	return &ProviderError{
		Sentinel:     ErrAPIError,
		Provider:     provider,
		Message:      fmt.Sprintf("HTTP %d\nRESPONSE: %s", statusCode, responseBody),
		StatusCode:   statusCode,
		ResponseBody: responseBody,
	}
}

// NewErrStreamRead wraps an I/O or scanner error that occurred while reading
// the response eventPub.
func NewErrStreamRead(provider string, cause error) *ProviderError {
	return &ProviderError{
		Sentinel: ErrStreamRead,
		Provider: provider,
		Message:  "eventPub read/decode error",
		Cause:    cause,
	}
}

// NewErrStreamDecode wraps a JSON or protocol decode failure mid-eventPub.
func NewErrStreamDecode(provider string, cause error) *ProviderError {
	return &ProviderError{
		Sentinel: ErrStreamDecode,
		Provider: provider,
		Message:  "eventPub read/decode error",
		Cause:    cause,
	}
}

// NewErrProviderMsg wraps an explicit error message sent by the provider
// inside the eventPub (e.g. an Anthropic error event or OpenRouter chunk error).
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

// NewErrUnknownModel returns an error for a model ToolCallID or alias that cannot
// be resolved by the provider.
func NewErrUnknownModel(provider string, modelID string) *ProviderError {
	return &ProviderError{
		Sentinel: ErrUnknownModel,
		Provider: provider,
		Message:  fmt.Sprintf("unknown model %q", modelID),
	}
}

// NewErrAllProvidersFailed returns an error when every failover target has been
// tried and all returned retriable errors. The original per-provider errors are
// preserved as the Cause via errors.Join so callers can inspect them with
// errors.Is / errors.As without losing the HTTP status, body, or message.
func NewErrAllProvidersFailed(provider string, errs []error) *ProviderError {
	return &ProviderError{
		Sentinel: ErrNoProviders,
		Provider: provider,
		Message:  fmt.Sprintf("all %d provider(s) exhausted", len(errs)),
		Cause:    errors.Join(errs...),
	}
}

// IsRetriableHTTPStatus reports whether an HTTP status code indicates a
// transient condition where retrying with a different provider may succeed.
// Providers use this to decide whether to surface an HTTP error through the
// event stream (non-retriable) or return it as an error from CreateStream
// (retriable, so the router can fail over to the next target).
func IsRetriableHTTPStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, // 429 — rate limited
		http.StatusServiceUnavailable, // 503 — temporarily down
		http.StatusPaymentRequired:    // 402 — OpenRouter out of credits
		return true
	}
	return false
}

// NewErrNoProviders returns an error when no providers are available or all
// failover targets have been exhausted.
func NewErrNoProviders(provider string) *ProviderError {
	return &ProviderError{
		Sentinel: ErrNoProviders,
		Provider: provider,
	}
}

// AsProviderError ensures err is a *ProviderError. If it already is one,
// it is returned as-is. Otherwise it is wrapped in a new ProviderError
// with ErrUnknown as the sentinel. This guarantees that every error
// surface from CreateStream and EventStream.Error() is a *ProviderError.
func AsProviderError(provider string, err error) *ProviderError {
	if err == nil {
		return nil
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe
	}
	return &ProviderError{
		Sentinel: ErrUnknown,
		Provider: provider,
		Message:  "unexpected error",
		Cause:    err,
	}
}
