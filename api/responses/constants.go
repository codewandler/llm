package responses

// HTTP response headers set by OpenAI on every response.
// Ref: https://platform.openai.com/docs/guides/rate-limits#headers
const (
	HeaderRateLimitReqLimit     = "x-ratelimit-limit-requests"
	HeaderRateLimitReqRemaining = "x-ratelimit-remaining-requests"
	HeaderRateLimitReqReset     = "x-ratelimit-reset-requests"
	HeaderRateLimitTokLimit     = "x-ratelimit-limit-tokens"
	HeaderRateLimitTokRemaining = "x-ratelimit-remaining-tokens"
	HeaderRequestID             = "x-request-id"
)

// SSE event names emitted by the Responses API streaming endpoint.
// Ref: https://platform.openai.com/docs/api-reference/responses/streaming
const (
	EventResponseCreated   = "response.created"
	EventOutputItemAdded   = "response.output_item.added"
	EventReasoningDelta    = "response.reasoning_summary_text.delta"
	EventOutputTextDelta   = "response.output_text.delta"
	EventFuncArgsDelta     = "response.function_call_arguments.delta"
	EventOutputItemDone    = "response.output_item.done"
	EventResponseCompleted = "response.completed"
	EventResponseFailed    = "response.failed"
	EventAPIError          = "error"
)

// Response.Status values inside ResponseCompletedEvent.
const (
	StatusCompleted  = "completed"
	StatusIncomplete = "incomplete"
	StatusFailed     = "failed"
)

// IncompleteDetails.Reason values inside ResponseCompletedEvent.
const (
	ReasonMaxOutputTokens = "max_output_tokens"
	ReasonContentFilter   = "content_filter"
)

// Default API path for the Responses endpoint.
const DefaultPath = "/v1/responses"
