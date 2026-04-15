package completions

// HTTP response headers set by OpenAI.
// Ref: https://platform.openai.com/docs/guides/rate-limits#headers
const (
	HeaderRateLimitReqLimit     = "x-ratelimit-limit-requests"
	HeaderRateLimitReqRemaining = "x-ratelimit-remaining-requests"
	HeaderRateLimitReqReset     = "x-ratelimit-reset-requests"
	HeaderRateLimitTokLimit     = "x-ratelimit-limit-tokens"
	HeaderRateLimitTokRemaining = "x-ratelimit-remaining-tokens"
	HeaderRequestID             = "x-request-id"
)

// StreamDone is the SSE sentinel that terminates a Chat Completions stream.
const StreamDone = "[DONE]"

// finish_reason values from the API.
const (
	FinishReasonStop          = "stop"
	FinishReasonToolCalls     = "tool_calls"
	FinishReasonLength        = "length"
	FinishReasonContentFilter = "content_filter"
)

// Default API path.
const DefaultPath = "/v1/chat/completions"
