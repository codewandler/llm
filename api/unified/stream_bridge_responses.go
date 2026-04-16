package unified

import (
	"encoding/json"
	"fmt"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/usage"
)

// funcCallMeta holds function-call metadata from response.output_item.added that may
// be absent from the later response.function_call_arguments.done event (e.g. Codex).
type funcCallMeta struct {
	name   string
	callID string
}

// ResponsesMapper converts Responses API events to unified StreamEvents.
// It is stateful: it carries function-call name and call_id forward from
// response.output_item.added so that response.function_call_arguments.done
// events from providers that omit those fields still produce correct ToolCall
// events with the right name and ID.
type ResponsesMapper struct {
	pending map[int]funcCallMeta // keyed by output_index
}

// NewResponsesMapper returns an initialised ResponsesMapper for one stream.
func NewResponsesMapper() *ResponsesMapper {
	return &ResponsesMapper{pending: make(map[int]funcCallMeta)}
}

// MapEvent converts one Responses API parser event to a unified StreamEvent.
// It must be called in stream order on the same ResponsesMapper instance.
func (m *ResponsesMapper) MapEvent(ev any) (StreamEvent, bool, error) {
	source := ev
	payload, _, _ := sourceEvent(ev)
	switch e := payload.(type) {
	case *responses.ResponseCreatedEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventStarted, Started: &Started{RequestID: e.Response.ID, Model: e.Response.Model}}, responses.EventResponseCreated), source), false, nil

	case *responses.ResponseQueuedEvent:
		return withRawEventPayload(withProviderExtras(withRawEventName(StreamEvent{Type: StreamEventLifecycle, Lifecycle: &Lifecycle{Scope: LifecycleScopeResponse, State: LifecycleStateQueued, Ref: StreamRef{ResponseID: e.Response.ID}}}, responses.EventResponseQueued), e), source), false, nil

	case *responses.ResponseInProgressEvent:
		return withRawEventPayload(withProviderExtras(withRawEventName(StreamEvent{Type: StreamEventLifecycle, Lifecycle: &Lifecycle{Scope: LifecycleScopeResponse, State: LifecycleStateInProgress, Ref: StreamRef{ResponseID: e.Response.ID}}}, responses.EventResponseInProgress), e), source), false, nil

	case *responses.ResponseFailedEvent:
		out := StreamEvent{Type: StreamEventCompleted, Lifecycle: &Lifecycle{Scope: LifecycleScopeResponse, State: LifecycleStateFailed, Ref: StreamRef{ResponseID: e.Response.ID}}, Completed: &Completed{StopReason: llm.StopReasonError}}
		if u := usageFromResponses(e.Response.Usage); u != nil {
			out.Usage = u
		}
		if e.Response.Error != nil {
			out.Error = &StreamError{Err: fmt.Errorf("responses response failed %s: %s", e.Response.Error.Code, e.Response.Error.Message)}
		}
		return withRawEventPayload(withProviderExtras(withRawEventName(out, responses.EventResponseFailed), e), source), false, nil

	case *responses.ResponseIncompleteEvent:
		out := StreamEvent{Type: StreamEventCompleted, Lifecycle: &Lifecycle{Scope: LifecycleScopeResponse, State: LifecycleStateIncomplete, Ref: StreamRef{ResponseID: e.Response.ID}}, Completed: &Completed{StopReason: mapResponsesIncompleteReason(e.Response.IncompleteDetails)}}
		if u := usageFromResponses(e.Response.Usage); u != nil {
			out.Usage = u
		}
		return withRawEventPayload(withProviderExtras(withRawEventName(out, responses.EventResponseIncomplete), e), source), false, nil

	case *responses.OutputTextDeltaEvent:
		ref := responsesContentRef(e.OutputIndex, e.ItemID, e.ContentIndex)
		out := StreamEvent{Type: StreamEventContentDelta, ContentDelta: &ContentDelta{Ref: ref, Kind: ContentKindText, Variant: ContentVariantPrimary, Encoding: ContentEncodingUTF8, Data: e.Delta}, Delta: &Delta{Kind: llm.DeltaKindText, Index: ref.ItemIndex, Text: e.Delta}}
		if len(e.Logprobs) > 0 {
			out.Extras.Provider = map[string]any{"logprobs": providerMap(e.Logprobs)}
		}
		return withRawEventPayload(withRawEventName(out, responses.EventOutputTextDelta), source), false, nil

	case *responses.OutputTextDoneEvent:
		ref := responsesContentRef(e.OutputIndex, e.ItemID, e.ContentIndex)
		out := StreamEvent{Type: StreamEventContent, StreamContent: &StreamContent{Ref: ref, Kind: ContentKindText, Variant: ContentVariantPrimary, Encoding: ContentEncodingUTF8, Data: e.Text}, Content: &ContentPart{Part: msg.Text(e.Text), Index: e.ContentIndex}}
		if len(e.Logprobs) > 0 {
			out.Extras.Provider = map[string]any{"logprobs": providerMap(e.Logprobs)}
		}
		return withRawEventPayload(withRawEventName(out, responses.EventOutputTextDone), source), false, nil

	case *responses.OutputTextAnnotationAddedEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventAnnotation, Annotation: &Annotation{Ref: responsesAnnotationRef(e.OutputIndex, e.ItemID, e.ContentIndex, e.AnnotationIndex), Type: e.Annotation.Type, Text: e.Annotation.Text, FileID: e.Annotation.FileID, Filename: e.Annotation.Filename, URL: e.Annotation.URL, Title: e.Annotation.Title, ContainerID: e.Annotation.ContainerID, StartIndex: e.Annotation.StartIndex, EndIndex: e.Annotation.EndIndex, Offset: e.Annotation.Offset, Index: e.Annotation.Index}}, responses.EventOutputTextAnnotationAdded), source), false, nil

	case *responses.RefusalDeltaEvent:
		ref := responsesContentRef(e.OutputIndex, e.ItemID, e.ContentIndex)
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventContentDelta, ContentDelta: &ContentDelta{Ref: ref, Kind: ContentKindRefusal, Variant: ContentVariantPrimary, Encoding: ContentEncodingUTF8, Data: e.Delta}}, responses.EventRefusalDelta), source), false, nil

	case *responses.RefusalDoneEvent:
		ref := responsesContentRef(e.OutputIndex, e.ItemID, e.ContentIndex)
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventContent, StreamContent: &StreamContent{Ref: ref, Kind: ContentKindRefusal, Variant: ContentVariantPrimary, Encoding: ContentEncodingUTF8, Data: e.Refusal}}, responses.EventRefusalDone), source), false, nil

	case *responses.ReasoningTextDeltaEvent:
		ref := responsesItemRef(e.OutputIndex, e.ItemID)
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventContentDelta, ContentDelta: &ContentDelta{Ref: ref, Kind: ContentKindReasoning, Variant: ContentVariantRaw, Encoding: ContentEncodingUTF8, Data: e.Delta}, Delta: &Delta{Kind: llm.DeltaKindThinking, Index: ref.ItemIndex, Thinking: e.Delta}}, responses.EventReasoningTextDelta), source), false, nil

	case *responses.ReasoningTextDoneEvent:
		ref := responsesItemRef(e.OutputIndex, e.ItemID)
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventContent, StreamContent: &StreamContent{Ref: ref, Kind: ContentKindReasoning, Variant: ContentVariantRaw, Encoding: ContentEncodingUTF8, Data: e.Text}, Content: &ContentPart{Part: msg.Thinking(e.Text, ""), Index: e.OutputIndex}}, responses.EventReasoningTextDone), source), false, nil

	case *responses.ReasoningSummaryPartAddedEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventLifecycle, Lifecycle: &Lifecycle{Scope: LifecycleScopeSegment, State: LifecycleStateAdded, Ref: responsesSummaryRef(e.OutputIndex, e.ItemID, e.SummaryIndex), Kind: ContentKindReasoning, Variant: ContentVariantSummary}}, responses.EventReasoningSummaryPartAdded), source), false, nil

	case *responses.ReasoningSummaryPartDoneEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventLifecycle, Lifecycle: &Lifecycle{Scope: LifecycleScopeSegment, State: LifecycleStateDone, Ref: responsesSummaryRef(e.OutputIndex, e.ItemID, e.SummaryIndex), Kind: ContentKindReasoning, Variant: ContentVariantSummary}}, responses.EventReasoningSummaryPartDone), source), false, nil

	case *responses.ReasoningSummaryTextDeltaEvent:
		ref := responsesItemRef(e.OutputIndex, e.ItemID)
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventContentDelta, ContentDelta: &ContentDelta{Ref: ref, Kind: ContentKindReasoning, Variant: ContentVariantSummary, Encoding: ContentEncodingUTF8, Data: e.Delta}, Delta: &Delta{Kind: llm.DeltaKindThinking, Index: ref.ItemIndex, Thinking: e.Delta}}, responses.EventReasoningSummaryTextDelta), source), false, nil

	case *responses.ReasoningSummaryTextDoneEvent:
		ref := responsesItemRef(e.OutputIndex, e.ItemID)
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventContent, StreamContent: &StreamContent{Ref: ref, Kind: ContentKindReasoning, Variant: ContentVariantSummary, Encoding: ContentEncodingUTF8, Data: e.Text}, Content: &ContentPart{Part: msg.Thinking(e.Text, ""), Index: e.OutputIndex}}, responses.EventReasoningSummaryTextDone), source), false, nil

	case *responses.FunctionCallArgumentsDeltaEvent:
		ref := responsesItemRef(e.OutputIndex, e.ItemID)
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventToolDelta, ToolDelta: &ToolDelta{Ref: ref, Kind: ToolDeltaKindFunctionArguments, Data: e.Delta}, Delta: &Delta{Kind: llm.DeltaKindTool, Index: ref.ItemIndex, ToolArgs: e.Delta}}, responses.EventFunctionCallArgumentsDelta), source), false, nil

	case *responses.FunctionCallArgumentsDoneEvent:
		var args map[string]any
		_ = json.Unmarshal([]byte(e.Arguments), &args)
		// Resolve name and call_id: providers such as Codex omit these from
		// function_call_arguments.done; carry them from output_item.added.
		name, callID := e.Name, e.ItemID
		if meta, ok := m.pending[e.OutputIndex]; ok {
			if name == "" {
				name = meta.name
			}
			if meta.callID != "" {
				// Always prefer the explicit call_id over item_id:
				// tool result messages reference call_id, not item_id.
				callID = meta.callID
			}
			delete(m.pending, e.OutputIndex)
		}
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventToolCall, StreamToolCall: &StreamToolCall{Ref: responsesItemRef(e.OutputIndex, e.ItemID), ID: callID, Name: name, RawInput: e.Arguments, Args: args}, ToolCall: &ToolCall{ID: callID, Name: name, Args: args}}, responses.EventFunctionCallArgumentsDone), source), false, nil

	case *responses.CustomToolCallInputDeltaEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventToolDelta, ToolDelta: &ToolDelta{Ref: responsesItemRef(e.OutputIndex, e.ItemID), Kind: ToolDeltaKindCustomInput, Data: e.Delta}}, responses.EventCustomToolCallInputDelta), source), false, nil

	case *responses.CustomToolCallInputDoneEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventToolDelta, ToolDelta: &ToolDelta{Ref: responsesItemRef(e.OutputIndex, e.ItemID), Kind: ToolDeltaKindCustomInput, Data: e.Input, Final: true}}, responses.EventCustomToolCallInputDone), source), false, nil

	case *responses.OutputItemAddedEvent:
		if e.Item.Type == "function_call" && (e.Item.Name != "" || e.Item.CallID != "") {
			m.pending[e.OutputIndex] = funcCallMeta{name: e.Item.Name, callID: e.Item.CallID}
		}
		return withRawEventPayload(withProviderExtras(withRawEventName(StreamEvent{Type: StreamEventLifecycle, Lifecycle: &Lifecycle{Scope: LifecycleScopeItem, State: LifecycleStateAdded, Ref: responsesItemRef(e.OutputIndex, e.Item.ID), ItemType: e.Item.Type}}, responses.EventOutputItemAdded), map[string]any{"item": e.Item}), source), false, nil

	case *responses.OutputItemDoneEvent:
		return withRawEventPayload(withProviderExtras(withRawEventName(StreamEvent{Type: StreamEventLifecycle, Lifecycle: &Lifecycle{Scope: LifecycleScopeItem, State: LifecycleStateDone, Ref: responsesItemRef(e.OutputIndex, e.Item.ID), ItemType: e.Item.Type}}, responses.EventOutputItemDone), map[string]any{"item": e.Item}), source), false, nil

	case *responses.ContentPartAddedEvent:
		kind, variant := responsesPartKindVariant(e.Part.Type)
		return withRawEventPayload(withProviderExtras(withRawEventName(StreamEvent{Type: StreamEventLifecycle, Lifecycle: &Lifecycle{Scope: LifecycleScopeSegment, State: LifecycleStateAdded, Ref: responsesContentRef(e.OutputIndex, e.ItemID, e.ContentIndex), Kind: kind, Variant: variant, Mime: responsesPartMime(e.Part.Type)}}, responses.EventContentPartAdded), map[string]any{"part": e.Part}), source), false, nil

	case *responses.ContentPartDoneEvent:
		kind, variant := responsesPartKindVariant(e.Part.Type)
		return withRawEventPayload(withProviderExtras(withRawEventName(StreamEvent{Type: StreamEventLifecycle, Lifecycle: &Lifecycle{Scope: LifecycleScopeSegment, State: LifecycleStateDone, Ref: responsesContentRef(e.OutputIndex, e.ItemID, e.ContentIndex), Kind: kind, Variant: variant, Mime: responsesPartMime(e.Part.Type)}}, responses.EventContentPartDone), map[string]any{"part": e.Part}), source), false, nil

	case *responses.AudioDeltaEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventContentDelta, ContentDelta: &ContentDelta{Ref: StreamRef{ResponseID: e.ResponseID}, Kind: ContentKindMedia, Variant: ContentVariantPrimary, Encoding: ContentEncodingBase64, Data: e.Delta}}, responses.EventAudioDelta), source), false, nil

	case *responses.AudioDoneEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventContentDelta, ContentDelta: &ContentDelta{Ref: StreamRef{ResponseID: e.ResponseID}, Kind: ContentKindMedia, Variant: ContentVariantPrimary, Encoding: ContentEncodingBase64, Final: true}}, responses.EventAudioDone), source), false, nil

	case *responses.AudioTranscriptDeltaEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventContentDelta, ContentDelta: &ContentDelta{Ref: StreamRef{ResponseID: e.ResponseID}, Kind: ContentKindMedia, Variant: ContentVariantTranscript, Encoding: ContentEncodingUTF8, Data: e.Delta}}, responses.EventAudioTranscriptDelta), source), false, nil

	case *responses.AudioTranscriptDoneEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventContentDelta, ContentDelta: &ContentDelta{Ref: StreamRef{ResponseID: e.ResponseID}, Kind: ContentKindMedia, Variant: ContentVariantTranscript, Encoding: ContentEncodingUTF8, Final: true}}, responses.EventAudioTranscriptDone), source), false, nil

	case *responses.ResponseCompletedEvent:
		out := StreamEvent{Type: StreamEventCompleted, Lifecycle: &Lifecycle{Scope: LifecycleScopeResponse, State: LifecycleStateDone, Ref: StreamRef{ResponseID: e.Response.ID}}, Completed: &Completed{StopReason: llm.StopReasonEndTurn}}
		if u := usageFromResponses(e.Response.Usage); u != nil {
			out.Usage = u
		}
		return withRawEventPayload(withProviderExtras(withRawEventName(out, responses.EventResponseCompleted), e), source), false, nil

	case *responses.APIErrorEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventError, Error: &StreamError{Err: e}}, responses.EventAPIError), source), false, nil

	case *responses.FileSearchCallInProgressEvent:
		return unknownResponsesEvent(responses.EventFileSearchCallInProgress, source, e), false, nil
	case *responses.FileSearchCallSearchingEvent:
		return unknownResponsesEvent(responses.EventFileSearchCallSearching, source, e), false, nil
	case *responses.FileSearchCallCompletedEvent:
		return unknownResponsesEvent(responses.EventFileSearchCallCompleted, source, e), false, nil
	case *responses.WebSearchCallInProgressEvent:
		return unknownResponsesEvent(responses.EventWebSearchCallInProgress, source, e), false, nil
	case *responses.WebSearchCallSearchingEvent:
		return unknownResponsesEvent(responses.EventWebSearchCallSearching, source, e), false, nil
	case *responses.WebSearchCallCompletedEvent:
		return unknownResponsesEvent(responses.EventWebSearchCallCompleted, source, e), false, nil
	case *responses.MCPCallArgumentsDeltaEvent:
		return unknownResponsesEvent(responses.EventMCPCallArgumentsDelta, source, e), false, nil
	case *responses.MCPCallArgumentsDoneEvent:
		return unknownResponsesEvent(responses.EventMCPCallArgumentsDone, source, e), false, nil
	case *responses.MCPCallCompletedEvent:
		return unknownResponsesEvent(responses.EventMCPCallCompleted, source, e), false, nil
	case *responses.MCPCallFailedEvent:
		return unknownResponsesEvent(responses.EventMCPCallFailed, source, e), false, nil
	case *responses.MCPCallInProgressEvent:
		return unknownResponsesEvent(responses.EventMCPCallInProgress, source, e), false, nil
	case *responses.MCPListToolsCompletedEvent:
		return unknownResponsesEvent(responses.EventMCPListToolsCompleted, source, e), false, nil
	case *responses.MCPListToolsFailedEvent:
		return unknownResponsesEvent(responses.EventMCPListToolsFailed, source, e), false, nil
	case *responses.MCPListToolsInProgressEvent:
		return unknownResponsesEvent(responses.EventMCPListToolsInProgress, source, e), false, nil
	case *responses.CodeInterpreterCallInProgressEvent:
		return unknownResponsesEvent(responses.EventCodeInterpreterCallInProgress, source, e), false, nil
	case *responses.CodeInterpreterCallInterpretingEvent:
		return unknownResponsesEvent(responses.EventCodeInterpreterCallInterpreting, source, e), false, nil
	case *responses.CodeInterpreterCallCompletedEvent:
		return unknownResponsesEvent(responses.EventCodeInterpreterCallCompleted, source, e), false, nil
	case *responses.CodeInterpreterCallCodeDeltaEvent:
		return unknownResponsesEvent(responses.EventCodeInterpreterCallCodeDelta, source, e), false, nil
	case *responses.CodeInterpreterCallCodeDoneEvent:
		return unknownResponsesEvent(responses.EventCodeInterpreterCallCodeDone, source, e), false, nil
	case *responses.ImageGenerationCallCompletedEvent:
		return unknownResponsesEvent(responses.EventImageGenerationCallCompleted, source, e), false, nil
	case *responses.ImageGenerationCallGeneratingEvent:
		return unknownResponsesEvent(responses.EventImageGenerationCallGenerating, source, e), false, nil
	case *responses.ImageGenerationCallInProgressEvent:
		return unknownResponsesEvent(responses.EventImageGenerationCallInProgress, source, e), false, nil
	case *responses.ImageGenerationCallPartialImageEvent:
		return unknownResponsesEvent(responses.EventImageGenerationCallPartialImage, source, e), false, nil

	default:
		return StreamEvent{Type: StreamEventUnknown}, false, nil
	}
}

func unknownResponsesEvent(name string, source any, provider any) StreamEvent {
	return withRawEventPayload(withProviderExtras(withRawEventName(StreamEvent{Type: StreamEventUnknown}, name), provider), source)
}

func responsesItemRef(outputIndex int, itemID string) StreamRef {
	return StreamRef{ItemIndex: uint32Ptr(outputIndex), ItemID: itemID}
}

func responsesContentRef(outputIndex int, itemID string, contentIndex int) StreamRef {
	return StreamRef{ItemIndex: uint32Ptr(outputIndex), ItemID: itemID, SegmentIndex: uint32Ptr(contentIndex)}
}

func responsesSummaryRef(outputIndex int, itemID string, summaryIndex int) StreamRef {
	return StreamRef{ItemIndex: uint32Ptr(outputIndex), ItemID: itemID, SegmentIndex: uint32Ptr(summaryIndex)}
}

func responsesAnnotationRef(outputIndex int, itemID string, contentIndex int, annotationIndex int) StreamRef {
	return StreamRef{ItemIndex: uint32Ptr(outputIndex), ItemID: itemID, SegmentIndex: uint32Ptr(contentIndex), AnnotationIndex: uint32Ptr(annotationIndex)}
}

func usageFromResponses(u *responses.ResponseUsage) *Usage {
	if u == nil {
		return nil
	}
	tokens := usage.TokenItems{{Kind: usage.KindInput, Count: u.InputTokens}, {Kind: usage.KindOutput, Count: u.OutputTokens}}
	if u.InputTokensDetails != nil && u.InputTokensDetails.CachedTokens > 0 {
		tokens = append(tokens, usage.TokenItem{Kind: usage.KindCacheRead, Count: u.InputTokensDetails.CachedTokens})
	}
	if u.OutputTokensDetails != nil && u.OutputTokensDetails.ReasoningTokens > 0 {
		tokens = append(tokens, usage.TokenItem{Kind: usage.KindReasoning, Count: u.OutputTokensDetails.ReasoningTokens})
	}
	return &Usage{Tokens: tokens.NonZero()}
}

func mapResponsesIncompleteReason(d *responses.IncompleteDetails) llm.StopReason {
	if d == nil {
		return llm.StopReasonEndTurn
	}
	switch d.Reason {
	case responses.ReasonMaxOutputTokens:
		return llm.StopReasonMaxTokens
	case responses.ReasonContentFilter:
		return llm.StopReasonContentFilter
	default:
		return llm.StopReason(d.Reason)
	}
}

func responsesPartKindVariant(partType string) (ContentKind, ContentVariant) {
	switch partType {
	case "output_text":
		return ContentKindText, ContentVariantPrimary
	case "refusal":
		return ContentKindRefusal, ContentVariantPrimary
	case "audio":
		return ContentKindMedia, ContentVariantPrimary
	default:
		return "", ""
	}
}

func responsesPartMime(partType string) string {
	switch partType {
	case "audio":
		return "audio/*"
	default:
		return ""
	}
}

// MapResponsesEvent is a stateless convenience wrapper around ResponsesMapper.MapEvent.
// For streaming use prefer NewResponsesMapper().MapEvent so that state is preserved
// across events in the same stream.
func MapResponsesEvent(ev any) (StreamEvent, bool, error) {
	return NewResponsesMapper().MapEvent(ev)
}