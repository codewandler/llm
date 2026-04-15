package messages

// Request headers.
// Ref: https://docs.anthropic.com/en/api/getting-started#headers
const (
	HeaderAPIKey           = "x-api-key"
	HeaderAnthropicVersion = "anthropic-version"
	HeaderAnthropicBeta    = "anthropic-beta"
)

// Anthropic API version sent in every request.
const APIVersion = "2023-06-01"

// Beta feature values for anthropic-beta header.
// Ref: https://docs.anthropic.com/en/api/beta-headers
const (
	BetaInterleavedThinking = "interleaved-thinking-2025-05-14"
)

// HTTP response headers for rate-limit tracking.
// Ref: https://docs.anthropic.com/en/api/rate-limits#response-headers
const (
	HeaderRateLimitReqLimit        = "anthropic-ratelimit-requests-limit"
	HeaderRateLimitReqRemaining    = "anthropic-ratelimit-requests-remaining"
	HeaderRateLimitReqReset        = "anthropic-ratelimit-requests-reset"
	HeaderRateLimitTokLimit        = "anthropic-ratelimit-tokens-limit"
	HeaderRateLimitTokRemaining    = "anthropic-ratelimit-tokens-remaining"
	HeaderRateLimitTokReset        = "anthropic-ratelimit-tokens-reset"
	HeaderRateLimitInTokLimit      = "anthropic-ratelimit-input-tokens-limit"
	HeaderRateLimitInTokRemaining  = "anthropic-ratelimit-input-tokens-remaining"
	HeaderRateLimitInTokReset      = "anthropic-ratelimit-input-tokens-reset"
	HeaderRateLimitOutTokLimit     = "anthropic-ratelimit-output-tokens-limit"
	HeaderRateLimitOutTokRemaining = "anthropic-ratelimit-output-tokens-remaining"
	HeaderRateLimitOutTokReset     = "anthropic-ratelimit-output-tokens-reset"
	HeaderRequestID                = "request-id"
)

// SSE event names emitted by the Anthropic Messages streaming API.
// Ref: https://docs.anthropic.com/en/api/messages-streaming#event-types
const (
	EventMessageStart      = "message_start"
	EventContentBlockStart = "content_block_start"
	EventContentBlockDelta = "content_block_delta"
	EventContentBlockStop  = "content_block_stop"
	EventMessageDelta      = "message_delta"
	EventMessageStop       = "message_stop"
	EventError             = "error"
	EventPing              = "ping"
)

// Content block types (content_block_start.content_block.type).
const (
	BlockTypeText                = "text"
	BlockTypeToolUse             = "tool_use"
	BlockTypeThinking            = "thinking"
	BlockTypeServerToolUse       = "server_tool_use"
	BlockTypeWebSearchToolResult = "web_search_tool_result"
)

// Delta types (content_block_delta.delta.type).
const (
	DeltaTypeText      = "text_delta"
	DeltaTypeInputJSON = "input_json_delta"
	DeltaTypeThinking  = "thinking_delta"
	DeltaTypeSignature = "signature_delta"
)

// Stop reasons (message_delta.delta.stop_reason).
const (
	StopReasonEndTurn = "end_turn"
	StopReasonToolUse = "tool_use"
	StopReasonMaxTok  = "max_tokens"
)

// Default path for the Messages API.
const DefaultPath = "/v1/messages"

// ThinkingMode controls extended thinking configuration.
type ThinkingMode int

const (
	ThinkingDisabled ThinkingMode = iota
	ThinkingEnabled
	ThinkingAdaptive
)
