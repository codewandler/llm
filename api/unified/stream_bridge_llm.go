package unified

import (
	"fmt"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

// PublishToLLM maps a canonical unified StreamEvent to the current llm.Publisher
// surface. Semantic events without a direct llm projection are preserved as
// debug payloads instead of being silently dropped.
func PublishToLLM(pub llm.Publisher, ev StreamEvent) error {
	if pub == nil {
		return fmt.Errorf("publisher is required")
	}
	handled := false

	if ev.Started != nil {
		handled = true
		pub.Started(llm.StreamStartedEvent{
			RequestID: ev.Started.RequestID,
			Model:     ev.Started.Model,
			Provider:  ev.Started.Provider,
			Extra:     ev.Started.Extra,
		})
	}

	if ev.Delta != nil {
		handled = true
		d := &llm.DeltaEvent{Kind: ev.Delta.Kind}
		d.Index = ev.Delta.Index
		d.Text = ev.Delta.Text
		d.Thinking = ev.Delta.Thinking
		d.ToolID = ev.Delta.ToolID
		d.ToolName = ev.Delta.ToolName
		d.ToolArgs = ev.Delta.ToolArgs
		pub.Delta(d)
	}

	if ev.ToolCall != nil {
		handled = true
		pub.ToolCall(tool.NewToolCall(ev.ToolCall.ID, ev.ToolCall.Name, ev.ToolCall.Args))
	}

	if ev.Content != nil {
		handled = true
		pub.ContentBlock(llm.ContentPartEvent{Part: ev.Content.Part, Index: ev.Content.Index})
	}

	if ev.Usage != nil {
		handled = true
		rec := usage.Record{
			Dims: usage.Dims{
				Provider:  ev.Usage.Provider,
				Model:     ev.Usage.Model,
				RequestID: ev.Usage.RequestID,
			},
			Tokens:     ev.Usage.Tokens,
			Cost:       ev.Usage.Cost,
			RecordedAt: ev.Usage.RecordedAt,
			Extras:     ev.Usage.Extras,
		}
		pub.UsageRecord(rec)
	}

	if ev.Completed != nil {
		handled = true
		pub.Completed(llm.CompletedEvent{StopReason: ev.Completed.StopReason})
	}

	if ev.Error != nil && ev.Error.Err != nil {
		handled = true
		pub.Error(ev.Error.Err)
	}

	if hasUnprojectedSemanticPayload(ev) || (!handled && (ev.Extras.RawEventName != "" || len(ev.Extras.RawJSON) > 0)) {
		pub.Debug("unified.stream_event", ev)
	}

	return nil
}

func hasUnprojectedSemanticPayload(ev StreamEvent) bool {
	return ev.Lifecycle != nil || ev.ContentDelta != nil || ev.StreamContent != nil || ev.ToolDelta != nil || ev.StreamToolCall != nil || ev.Annotation != nil || ev.Type == StreamEventUnknown
}
