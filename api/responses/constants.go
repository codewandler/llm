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

// Explicit known no-op event names (documented by the API but currently
// not required by the adapt layer). Parsers should keep explicit case labels
// for these to make coverage intentional and visible.
const (
	EventResponseInProgress   = "response.in_progress"
	EventContentPartAdded     = "response.content_part.added"
	EventContentPartDone      = "response.content_part.done"
	EventOutputTextDone       = "response.output_text.done"
	EventOutputTextAnnotation = "response.output_text.annotation.added"
	EventFuncArgsDone         = "response.function_call_arguments.done"
	EventReasoningDeltaRaw    = "response.reasoning.delta"
	EventReasoningDone        = "response.reasoning.done"
	EventReasoningSummaryDone = "response.reasoning_summary_text.done"
	EventReasoningTextDelta   = "response.reasoning_text.delta"  // actual thinking tokens (e.g. Claude via OpenRouter)
	EventReasoningTextDone    = "response.reasoning_text.done"   // no-op: complete thinking text
	EventResponseQueued       = "response.queued"
	EventRateLimitsUpdated    = "rate_limits.updated"
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
