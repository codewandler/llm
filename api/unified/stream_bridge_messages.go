package unified

import (
	"encoding/json"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/usage"
)

// MapMessagesEvent converts a Messages native parser event into a unified StreamEvent.
// Returns ignored=true for explicit no-op events.
func MapMessagesEvent(ev any) (StreamEvent, bool, error) {
	switch e := ev.(type) {
	case *messages.MessageStartEvent:
		tokens := usage.TokenItems{
			{Kind: usage.KindInput, Count: e.Message.Usage.InputTokens},
			{Kind: usage.KindCacheWrite, Count: e.Message.Usage.CacheCreationInputTokens},
			{Kind: usage.KindCacheRead, Count: e.Message.Usage.CacheReadInputTokens},
		}.NonZero()
		return withRawEventPayload(withRawEventName(StreamEvent{
			Type:    StreamEventStarted,
			Started: &Started{RequestID: e.Message.ID, Model: e.Message.Model},
			Usage:   &Usage{Tokens: tokens},
		}, messages.EventMessageStart), e), false, nil

	case *messages.ContentBlockStartEvent:
		out := StreamEvent{
			Type: StreamEventLifecycle,
			Lifecycle: &Lifecycle{
				Scope: LifecycleScopeSegment,
				State: LifecycleStateAdded,
				Ref:   StreamRef{ItemIndex: uint32Ptr(e.Index)},
			},
		}
		var block messages.StartBlockView
		if err := json.Unmarshal(e.ContentBlock, &block); err == nil {
			out.Lifecycle.ItemType = block.Type
			out.Lifecycle.Kind, out.Lifecycle.Variant = messagesBlockKindVariant(block.Type)
			out.Extras.Provider = map[string]any{"content_block": providerMap(block)}
		} else {
			out.Extras.Provider = map[string]any{"content_block": string(e.ContentBlock)}
		}
		return withRawEventPayload(withRawEventName(out, messages.EventContentBlockStart), e), false, nil

	case *messages.ContentBlockDeltaEvent:
		ref := StreamRef{ItemIndex: uint32Ptr(e.Index)}
		switch e.Delta.Type {
		case messages.DeltaTypeText:
			return withRawEventPayload(withRawEventName(StreamEvent{
				Type: StreamEventContentDelta,
				ContentDelta: &ContentDelta{
					Ref:      ref,
					Kind:     ContentKindText,
					Variant:  ContentVariantPrimary,
					Encoding: ContentEncodingUTF8,
					Data:     e.Delta.Text,
				},
				Delta: &Delta{Kind: llm.DeltaKindText, Index: ref.ItemIndex, Text: e.Delta.Text},
			}, messages.EventContentBlockDelta), e), false, nil
		case messages.DeltaTypeThinking:
			return withRawEventPayload(withRawEventName(StreamEvent{
				Type: StreamEventContentDelta,
				ContentDelta: &ContentDelta{
					Ref:      ref,
					Kind:     ContentKindReasoning,
					Encoding: ContentEncodingUTF8,
					Data:     e.Delta.Thinking,
				},
				Delta: &Delta{Kind: llm.DeltaKindThinking, Index: ref.ItemIndex, Thinking: e.Delta.Thinking},
			}, messages.EventContentBlockDelta), e), false, nil
		case messages.DeltaTypeInputJSON:
			return withRawEventPayload(withRawEventName(StreamEvent{
				Type: StreamEventToolDelta,
				ToolDelta: &ToolDelta{
					Ref:  ref,
					Kind: ToolDeltaKindFunctionArguments,
					Data: e.Delta.PartialJSON,
				},
				Delta: &Delta{Kind: llm.DeltaKindTool, Index: ref.ItemIndex, ToolArgs: e.Delta.PartialJSON},
			}, messages.EventContentBlockDelta), e), false, nil
		case messages.DeltaTypeSignature:
			return withRawEventPayload(withRawEventName(StreamEvent{
				Type: StreamEventContentDelta,
				ContentDelta: &ContentDelta{
					Ref:       ref,
					Kind:      ContentKindReasoning,
					Signature: e.Delta.Signature,
				},
			}, messages.EventContentBlockDelta), e), false, nil
		default:
			return withRawEventPayload(withProviderExtras(withRawEventName(StreamEvent{Type: StreamEventUnknown}, messages.EventContentBlockDelta), e), e), false, nil
		}

	case *messages.TextCompleteEvent:
		ref := StreamRef{ItemIndex: uint32Ptr(e.Index)}
		return withRawEventPayload(withRawEventName(StreamEvent{
			Type: StreamEventContent,
			Lifecycle: &Lifecycle{
				Scope:   LifecycleScopeSegment,
				State:   LifecycleStateDone,
				Ref:     ref,
				Kind:    ContentKindText,
				Variant: ContentVariantPrimary,
			},
			StreamContent: &StreamContent{
				Ref:      ref,
				Kind:     ContentKindText,
				Variant:  ContentVariantPrimary,
				Encoding: ContentEncodingUTF8,
				Data:     e.Text,
			},
			Content: &ContentPart{Part: msg.Text(e.Text), Index: e.Index},
		}, messages.EventContentBlockStop), e), false, nil

	case *messages.ThinkingCompleteEvent:
		ref := StreamRef{ItemIndex: uint32Ptr(e.Index)}
		return withRawEventPayload(withRawEventName(StreamEvent{
			Type: StreamEventContent,
			Lifecycle: &Lifecycle{
				Scope: LifecycleScopeSegment,
				State: LifecycleStateDone,
				Ref:   ref,
				Kind:  ContentKindReasoning,
			},
			StreamContent: &StreamContent{
				Ref:       ref,
				Kind:      ContentKindReasoning,
				Encoding:  ContentEncodingUTF8,
				Data:      e.Thinking,
				Signature: e.Signature,
			},
			Content: &ContentPart{Part: msg.Thinking(e.Thinking, e.Signature), Index: e.Index},
		}, messages.EventContentBlockStop), e), false, nil

	case *messages.ToolCompleteEvent:
		ref := StreamRef{ItemIndex: uint32Ptr(e.Index)}
		return withRawEventPayload(withRawEventName(StreamEvent{
			Type: StreamEventToolCall,
			Lifecycle: &Lifecycle{
				Scope:    LifecycleScopeSegment,
				State:    LifecycleStateDone,
				Ref:      ref,
				ItemType: messages.BlockTypeToolUse,
			},
			StreamToolCall: &StreamToolCall{Ref: ref, ID: e.ID, Name: e.Name, RawInput: e.RawInput, Args: e.Args},
			ToolCall:       &ToolCall{ID: e.ID, Name: e.Name, Args: e.Args},
		}, messages.EventContentBlockStop), e), false, nil

	case *messages.MessageDeltaEvent:
		tokens := usage.TokenItems{{Kind: usage.KindOutput, Count: e.Usage.OutputTokens}}.NonZero()
		return withRawEventPayload(withRawEventName(StreamEvent{
			Type:      StreamEventCompleted,
			Completed: &Completed{StopReason: mapMessagesStopReason(e.Delta.StopReason)},
			Usage:     &Usage{Tokens: tokens},
		}, messages.EventMessageDelta), e), false, nil

	case *messages.StreamErrorEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{Type: StreamEventError, Error: &StreamError{Err: e}}, messages.EventError), e), false, nil

	case *messages.PingEvent, *messages.MessageStopEvent:
		return StreamEvent{}, true, nil

	case *messages.ContentBlockStopEvent:
		return withRawEventPayload(withRawEventName(StreamEvent{
			Type: StreamEventLifecycle,
			Lifecycle: &Lifecycle{
				Scope: LifecycleScopeSegment,
				State: LifecycleStateDone,
				Ref:   StreamRef{ItemIndex: uint32Ptr(e.Index)},
			},
		}, messages.EventContentBlockStop), e), false, nil

	default:
		return StreamEvent{Type: StreamEventUnknown}, false, nil
	}
}

func messagesBlockKindVariant(blockType string) (ContentKind, ContentVariant) {
	switch blockType {
	case messages.BlockTypeText:
		return ContentKindText, ContentVariantPrimary
	case messages.BlockTypeThinking, messages.BlockTypeRedactedThinking:
		return ContentKindReasoning, ""
	default:
		return "", ""
	}
}

func mapMessagesStopReason(s string) llm.StopReason {
	switch s {
	case "end_turn":
		return llm.StopReasonEndTurn
	case "tool_use":
		return llm.StopReasonToolUse
	case "max_tokens":
		return llm.StopReasonMaxTokens
	default:
		return llm.StopReason(s)
	}
}
