package responses

import (
	"encoding/json"
	"fmt"

	"github.com/codewandler/llm/api/apicore"
)

type eventTypeSetter interface {
	SetType(string)
}

// NewParser returns a ParserFactory for the OpenAI Responses API.
//
// It handles both named SSE (OpenAI spec: `event: response.created\ndata: ...`)
// and unnamed SSE (OpenRouter style: `data: {"type":"response.created",...}`).
// When the SSE event name is empty, the event type is extracted from the
// JSON payload.
func NewParser() apicore.ParserFactory {
	return func() apicore.EventHandler {
		return func(name string, data []byte) apicore.StreamResult {
			if name == "" {
				var envelope struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(data, &envelope) == nil && envelope.Type != "" {
					name = envelope.Type
				}
			}

			switch name {
			case EventResponseCreated:
				return parseEvent[ResponseCreatedEvent](name, data, false)
			case EventResponseInProgress:
				return parseEvent[ResponseInProgressEvent](name, data, false)
			case EventResponseCompleted:
				return parseEvent[ResponseCompletedEvent](name, data, true)
			case EventResponseFailed:
				return parseEvent[ResponseFailedEvent](name, data, true)
			case EventResponseIncomplete:
				return parseEvent[ResponseIncompleteEvent](name, data, true)
			case EventResponseQueued:
				return parseEvent[ResponseQueuedEvent](name, data, false)

			case EventOutputItemAdded:
				return parseEvent[OutputItemAddedEvent](name, data, false)
			case EventOutputItemDone:
				return parseEvent[OutputItemDoneEvent](name, data, false)
			case EventContentPartAdded:
				return parseEvent[ContentPartAddedEvent](name, data, false)
			case EventContentPartDone:
				return parseEvent[ContentPartDoneEvent](name, data, false)

			case EventOutputTextDelta:
				return parseEvent[OutputTextDeltaEvent](name, data, false)
			case EventOutputTextDone:
				return parseEvent[OutputTextDoneEvent](name, data, false)
			case EventOutputTextAnnotationAdded:
				return parseEvent[OutputTextAnnotationAddedEvent](name, data, false)
			case EventRefusalDelta:
				return parseEvent[RefusalDeltaEvent](name, data, false)
			case EventRefusalDone:
				return parseEvent[RefusalDoneEvent](name, data, false)

			case EventFunctionCallArgumentsDelta:
				return parseEvent[FunctionCallArgumentsDeltaEvent](name, data, false)
			case EventFunctionCallArgumentsDone:
				return parseEvent[FunctionCallArgumentsDoneEvent](name, data, false)

			case EventFileSearchCallInProgress:
				return parseEvent[FileSearchCallInProgressEvent](name, data, false)
			case EventFileSearchCallSearching:
				return parseEvent[FileSearchCallSearchingEvent](name, data, false)
			case EventFileSearchCallCompleted:
				return parseEvent[FileSearchCallCompletedEvent](name, data, false)

			case EventWebSearchCallInProgress:
				return parseEvent[WebSearchCallInProgressEvent](name, data, false)
			case EventWebSearchCallSearching:
				return parseEvent[WebSearchCallSearchingEvent](name, data, false)
			case EventWebSearchCallCompleted:
				return parseEvent[WebSearchCallCompletedEvent](name, data, false)

			case EventReasoningSummaryPartAdded:
				return parseEvent[ReasoningSummaryPartAddedEvent](name, data, false)
			case EventReasoningSummaryPartDone:
				return parseEvent[ReasoningSummaryPartDoneEvent](name, data, false)
			case EventReasoningSummaryTextDelta:
				return parseEvent[ReasoningSummaryTextDeltaEvent](name, data, false)
			case EventReasoningSummaryTextDone:
				return parseEvent[ReasoningSummaryTextDoneEvent](name, data, false)
			case EventReasoningTextDelta:
				return parseEvent[ReasoningTextDeltaEvent](name, data, false)
			case EventReasoningTextDone:
				return parseEvent[ReasoningTextDoneEvent](name, data, false)

			case EventImageGenerationCallCompleted:
				return parseEvent[ImageGenerationCallCompletedEvent](name, data, false)
			case EventImageGenerationCallGenerating:
				return parseEvent[ImageGenerationCallGeneratingEvent](name, data, false)
			case EventImageGenerationCallInProgress:
				return parseEvent[ImageGenerationCallInProgressEvent](name, data, false)
			case EventImageGenerationCallPartialImage:
				return parseEvent[ImageGenerationCallPartialImageEvent](name, data, false)

			case EventMCPCallArgumentsDelta:
				return parseEvent[MCPCallArgumentsDeltaEvent](name, data, false)
			case EventMCPCallArgumentsDone:
				return parseEvent[MCPCallArgumentsDoneEvent](name, data, false)
			case EventMCPCallCompleted:
				return parseEvent[MCPCallCompletedEvent](name, data, false)
			case EventMCPCallFailed:
				return parseEvent[MCPCallFailedEvent](name, data, false)
			case EventMCPCallInProgress:
				return parseEvent[MCPCallInProgressEvent](name, data, false)
			case EventMCPListToolsCompleted:
				return parseEvent[MCPListToolsCompletedEvent](name, data, false)
			case EventMCPListToolsFailed:
				return parseEvent[MCPListToolsFailedEvent](name, data, false)
			case EventMCPListToolsInProgress:
				return parseEvent[MCPListToolsInProgressEvent](name, data, false)

			case EventCodeInterpreterCallInProgress:
				return parseEvent[CodeInterpreterCallInProgressEvent](name, data, false)
			case EventCodeInterpreterCallInterpreting:
				return parseEvent[CodeInterpreterCallInterpretingEvent](name, data, false)
			case EventCodeInterpreterCallCompleted:
				return parseEvent[CodeInterpreterCallCompletedEvent](name, data, false)
			case EventCodeInterpreterCallCodeDelta:
				return parseEvent[CodeInterpreterCallCodeDeltaEvent](name, data, false)
			case EventCodeInterpreterCallCodeDone:
				return parseEvent[CodeInterpreterCallCodeDoneEvent](name, data, false)

			case EventCustomToolCallInputDelta:
				return parseEvent[CustomToolCallInputDeltaEvent](name, data, false)
			case EventCustomToolCallInputDone:
				return parseEvent[CustomToolCallInputDoneEvent](name, data, false)

			case EventAudioTranscriptDone:
				return parseEvent[AudioTranscriptDoneEvent](name, data, false)
			case EventAudioTranscriptDelta:
				return parseEvent[AudioTranscriptDeltaEvent](name, data, false)
			case EventAudioDone:
				return parseEvent[AudioDoneEvent](name, data, false)
			case EventAudioDelta:
				return parseEvent[AudioDeltaEvent](name, data, false)

			case EventAPIError:
				var evt APIErrorEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err), Done: true}
				}
				if s, ok := any(&evt).(eventTypeSetter); ok {
					s.SetType(name)
				}
				return apicore.StreamResult{Err: &evt, Done: true}

			default:
				return apicore.StreamResult{}
			}
		}
	}
}

func parseEvent[T any](name string, data []byte, done bool) apicore.StreamResult {
	var evt T
	if err := json.Unmarshal(data, &evt); err != nil {
		return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err), Done: done}
	}
	if s, ok := any(&evt).(eventTypeSetter); ok {
		s.SetType(name)
	}
	return apicore.StreamResult{Event: &evt, Done: done}
}
