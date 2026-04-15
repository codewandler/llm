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
