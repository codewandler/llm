package unified

import (
	"fmt"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

// Publish maps a canonical unified StreamEvent to existing llm.Publisher methods.
func Publish(pub llm.Publisher, ev StreamEvent) error {
	if pub == nil {
		return fmt.Errorf("publisher is required")
	}

	if ev.Started != nil {
		pub.Started(llm.StreamStartedEvent{
			RequestID: ev.Started.RequestID,
			Model:     ev.Started.Model,
			Provider:  ev.Started.Provider,
			Extra:     ev.Started.Extra,
		})
	}

	if ev.Delta != nil {
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
		pub.ToolCall(tool.NewToolCall(ev.ToolCall.ID, ev.ToolCall.Name, ev.ToolCall.Args))
	}

	if ev.Content != nil {
		pub.ContentBlock(llm.ContentPartEvent{Part: ev.Content.Part, Index: ev.Content.Index})
	}

	if ev.Usage != nil {
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
		pub.Completed(llm.CompletedEvent{StopReason: ev.Completed.StopReason})
	}

	if ev.Error != nil && ev.Error.Err != nil {
		pub.Error(ev.Error.Err)
	}

	return nil
}
