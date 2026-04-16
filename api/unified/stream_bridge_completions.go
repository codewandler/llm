package unified

import (
	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/completions"
	"github.com/codewandler/llm/usage"
)

// MapCompletionsEvent converts a Chat Completions native parser event into a unified StreamEvent.
// Returns ignored=true for explicit no-op events.
func MapCompletionsEvent(ev any) (StreamEvent, bool, error) {
	source := ev
	payload, _, _ := sourceEvent(ev)
	chunk, ok := payload.(*completions.Chunk)
	if !ok {
		return StreamEvent{Type: StreamEventUnknown}, false, nil
	}

	out := withRawEventPayload(withProviderExtras(StreamEvent{}, chunk), source)
	if chunk.ID != "" || chunk.Model != "" {
		out.Type = StreamEventStarted
		out.Started = &Started{RequestID: chunk.ID, Model: chunk.Model}
	}

	if chunk.Usage != nil {
		tokens := usage.TokenItems{{Kind: usage.KindInput, Count: chunk.Usage.PromptTokens}, {Kind: usage.KindOutput, Count: chunk.Usage.CompletionTokens}}.NonZero()
		out.Type = StreamEventUsage
		out.Usage = &Usage{Tokens: tokens}
	}

	if len(chunk.Choices) > 0 {
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			out.Type = StreamEventContentDelta
			out.ContentDelta = &ContentDelta{Kind: ContentKindText, Variant: ContentVariantPrimary, Encoding: ContentEncodingUTF8, Data: choice.Delta.Content}
			out.Delta = &Delta{Kind: llm.DeltaKindText, Text: choice.Delta.Content}
		}
		if len(choice.Delta.ToolCalls) > 0 {
			tc := choice.Delta.ToolCalls[0]
			ref := StreamRef{ItemIndex: uint32Ptr(tc.Index)}
			out.Type = StreamEventToolDelta
			out.ToolDelta = &ToolDelta{Ref: ref, Kind: ToolDeltaKindFunctionArguments, ToolID: tc.ID, ToolName: tc.Function.Name, Data: tc.Function.Arguments}
			out.Delta = &Delta{Kind: llm.DeltaKindTool, Index: ref.ItemIndex, ToolID: tc.ID, ToolName: tc.Function.Name, ToolArgs: tc.Function.Arguments}
		}
		if choice.FinishReason != nil {
			out.Type = StreamEventCompleted
			out.Completed = &Completed{StopReason: mapOpenAIFinishReason(*choice.FinishReason)}
		}
	}

	if out.Started == nil && out.Delta == nil && out.Usage == nil && out.Completed == nil && out.ContentDelta == nil && out.ToolDelta == nil {
		return StreamEvent{}, true, nil
	}
	return out, false, nil
}

func mapOpenAIFinishReason(s string) llm.StopReason {
	switch s {
	case completions.FinishReasonStop:
		return llm.StopReasonEndTurn
	case completions.FinishReasonToolCalls:
		return llm.StopReasonToolUse
	case completions.FinishReasonLength:
		return llm.StopReasonMaxTokens
	case completions.FinishReasonContentFilter:
		return llm.StopReasonContentFilter
	default:
		return llm.StopReason(s)
	}
}
