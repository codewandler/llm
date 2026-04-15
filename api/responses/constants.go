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

// SSE event names emitted by the native OpenAI Responses streaming endpoint.
// This package keeps the OpenAI wire names as the canonical surface.
// Ref: https://developers.openai.com/api/reference/resources/responses/streaming-events
const (
	EventResponseCreated                 = "response.created"
	EventResponseInProgress              = "response.in_progress"
	EventResponseCompleted               = "response.completed"
	EventResponseFailed                  = "response.failed"
	EventResponseIncomplete              = "response.incomplete"
	EventResponseQueued                  = "response.queued"
	EventOutputItemAdded                 = "response.output_item.added"
	EventOutputItemDone                  = "response.output_item.done"
	EventContentPartAdded                = "response.content_part.added"
	EventContentPartDone                 = "response.content_part.done"
	EventOutputTextDelta                 = "response.output_text.delta"
	EventOutputTextDone                  = "response.output_text.done"
	EventOutputTextAnnotationAdded       = "response.output_text.annotation.added"
	EventRefusalDelta                    = "response.refusal.delta"
	EventRefusalDone                     = "response.refusal.done"
	EventFunctionCallArgumentsDelta      = "response.function_call_arguments.delta"
	EventFunctionCallArgumentsDone       = "response.function_call_arguments.done"
	EventFileSearchCallInProgress        = "response.file_search_call.in_progress"
	EventFileSearchCallSearching         = "response.file_search_call.searching"
	EventFileSearchCallCompleted         = "response.file_search_call.completed"
	EventWebSearchCallInProgress         = "response.web_search_call.in_progress"
	EventWebSearchCallSearching          = "response.web_search_call.searching"
	EventWebSearchCallCompleted          = "response.web_search_call.completed"
	EventReasoningSummaryPartAdded       = "response.reasoning_summary_part.added"
	EventReasoningSummaryPartDone        = "response.reasoning_summary_part.done"
	EventReasoningSummaryTextDelta       = "response.reasoning_summary_text.delta"
	EventReasoningSummaryTextDone        = "response.reasoning_summary_text.done"
	EventReasoningTextDelta              = "response.reasoning_text.delta"
	EventReasoningTextDone               = "response.reasoning_text.done"
	EventImageGenerationCallCompleted    = "response.image_generation_call.completed"
	EventImageGenerationCallGenerating   = "response.image_generation_call.generating"
	EventImageGenerationCallInProgress   = "response.image_generation_call.in_progress"
	EventImageGenerationCallPartialImage = "response.image_generation_call.partial_image"
	EventMCPCallArgumentsDelta           = "response.mcp_call_arguments.delta"
	EventMCPCallArgumentsDone            = "response.mcp_call_arguments.done"
	EventMCPCallCompleted                = "response.mcp_call.completed"
	EventMCPCallFailed                   = "response.mcp_call.failed"
	EventMCPCallInProgress               = "response.mcp_call.in_progress"
	EventMCPListToolsCompleted           = "response.mcp_list_tools.completed"
	EventMCPListToolsFailed              = "response.mcp_list_tools.failed"
	EventMCPListToolsInProgress          = "response.mcp_list_tools.in_progress"
	EventCodeInterpreterCallInProgress   = "response.code_interpreter_call.in_progress"
	EventCodeInterpreterCallInterpreting = "response.code_interpreter_call.interpreting"
	EventCodeInterpreterCallCompleted    = "response.code_interpreter_call.completed"
	EventCodeInterpreterCallCodeDelta    = "response.code_interpreter_call_code.delta"
	EventCodeInterpreterCallCodeDone     = "response.code_interpreter_call_code.done"
	EventCustomToolCallInputDelta        = "response.custom_tool_call_input.delta"
	EventCustomToolCallInputDone         = "response.custom_tool_call_input.done"
	EventAPIError                        = "error"
	EventAudioTranscriptDone             = "response.audio.transcript.done"
	EventAudioTranscriptDelta            = "response.audio.transcript.delta"
	EventAudioDone                       = "response.audio.done"
	EventAudioDelta                      = "response.audio.delta"
)

// DocumentedStreamEvents is the exact documented OpenAI event inventory handled
// by this package.
var DocumentedStreamEvents = []string{
	EventResponseCreated,
	EventResponseInProgress,
	EventResponseCompleted,
	EventResponseFailed,
	EventResponseIncomplete,
	EventResponseQueued,
	EventOutputItemAdded,
	EventOutputItemDone,
	EventContentPartAdded,
	EventContentPartDone,
	EventOutputTextDelta,
	EventOutputTextDone,
	EventOutputTextAnnotationAdded,
	EventRefusalDelta,
	EventRefusalDone,
	EventFunctionCallArgumentsDelta,
	EventFunctionCallArgumentsDone,
	EventFileSearchCallInProgress,
	EventFileSearchCallSearching,
	EventFileSearchCallCompleted,
	EventWebSearchCallInProgress,
	EventWebSearchCallSearching,
	EventWebSearchCallCompleted,
	EventReasoningSummaryPartAdded,
	EventReasoningSummaryPartDone,
	EventReasoningSummaryTextDelta,
	EventReasoningSummaryTextDone,
	EventReasoningTextDelta,
	EventReasoningTextDone,
	EventImageGenerationCallCompleted,
	EventImageGenerationCallGenerating,
	EventImageGenerationCallInProgress,
	EventImageGenerationCallPartialImage,
	EventMCPCallArgumentsDelta,
	EventMCPCallArgumentsDone,
	EventMCPCallCompleted,
	EventMCPCallFailed,
	EventMCPCallInProgress,
	EventMCPListToolsCompleted,
	EventMCPListToolsFailed,
	EventMCPListToolsInProgress,
	EventCodeInterpreterCallInProgress,
	EventCodeInterpreterCallInterpreting,
	EventCodeInterpreterCallCompleted,
	EventCodeInterpreterCallCodeDelta,
	EventCodeInterpreterCallCodeDone,
	EventCustomToolCallInputDelta,
	EventCustomToolCallInputDone,
	EventAPIError,
	EventAudioTranscriptDone,
	EventAudioTranscriptDelta,
	EventAudioDone,
	EventAudioDelta,
}

// Response.Status values inside lifecycle response events.
const (
	StatusCompleted  = "completed"
	StatusIncomplete = "incomplete"
	StatusFailed     = "failed"
)

// IncompleteDetails.Reason values inside response lifecycle events.
const (
	ReasonMaxOutputTokens = "max_output_tokens"
	ReasonContentFilter   = "content_filter"
)

// Default API path for the Responses endpoint.
const DefaultPath = "/v1/responses"
