package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
)

// toolBlock accumulates streaming tool-call input fragments.
type toolBlock struct {
	id      string
	name    string
	jsonBuf strings.Builder
}

// streamProcessor owns all mutable state and the per-event dispatch logic for
// an Anthropic-format SSE stream. It is the single authoritative place for the
// rule "when event X arrives, do Y to the publisher".
//
// ParseStream feeds it via dispatch; tests feed it directly via the on* methods.
type streamProcessor struct {
	meta        ParseOpts
	pub         llm.Publisher
	activeTools map[int]*toolBlock
	usage       llm.Usage
	stopReason  llm.StopReason
}

func newStreamProcessor(meta ParseOpts, pub llm.Publisher) *streamProcessor {
	return &streamProcessor{
		meta:        meta,
		pub:         pub,
		activeTools: make(map[int]*toolBlock),
	}
}

// dispatch JSON-decodes one SSE data line and routes to the appropriate on*
// method. Returns false when the stream should stop (message_stop or error).
func (p *streamProcessor) dispatch(data string) bool {
	if data == "" {
		return true
	}
	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(data), &base); err != nil {
		return true
	}
	switch base.Type {
	case "message_start":
		var evt MessageStartEvent
		if err := json.Unmarshal([]byte(data), &evt); err == nil {
			p.onMessageStart(evt)
		}
	case "message_delta":
		var evt MessageDeltaEvent
		if err := json.Unmarshal([]byte(data), &evt); err == nil {
			p.onMessageDelta(evt)
		}
	case "message_stop":
		p.onMessageStop()
		return false
	case "content_block_start":
		var evt ContentBlockStartEvent
		if err := json.Unmarshal([]byte(data), &evt); err == nil {
			p.onContentBlockStart(evt)
		}
	case "content_block_delta":
		var evt ContentBlockDeltaEvent
		if err := json.Unmarshal([]byte(data), &evt); err == nil {
			p.onContentBlockDelta(evt)
		}
	case "content_block_stop":
		var evt ContentBlockStopEvent
		if err := json.Unmarshal([]byte(data), &evt); err == nil {
			p.onContentBlockStop(evt)
		}
	case "error":
		var evt StreamErrorEvent
		if err := json.Unmarshal([]byte(data), &evt); err == nil {
			p.onError(evt)
		}
		return false
	}
	return true
}

func (p *streamProcessor) onMessageStart(evt MessageStartEvent) {
	p.usage.CacheWriteTokens = evt.Message.Usage.CacheCreationInputTokens
	p.usage.CacheReadTokens = evt.Message.Usage.CacheReadInputTokens
	p.usage.InputTokens = evt.Message.Usage.InputTokens +
		p.usage.CacheWriteTokens + p.usage.CacheReadTokens

	p.pub.Started(llm.StreamStartedEvent{
		Model:     evt.Message.Model,
		RequestID: evt.Message.ID,
	})
}

func (p *streamProcessor) onMessageDelta(evt MessageDeltaEvent) {
	p.usage.OutputTokens = evt.Usage.OutputTokens
	p.usage.TotalTokens = p.usage.InputTokens + p.usage.OutputTokens
	if evt.Delta.StopReason != "" {
		p.stopReason = mapAnthropicStopReason(evt.Delta.StopReason)
	}
}

func (p *streamProcessor) onMessageStop() {
	costFn := p.meta.CostFn
	if costFn == nil {
		costFn = FillCost
	}
	costFn(p.meta.ResolvedModel, &p.usage)
	p.pub.Usage(p.usage)
	p.pub.Completed(llm.CompletedEvent{StopReason: p.stopReason})
}

func (p *streamProcessor) onContentBlockStart(evt ContentBlockStartEvent) {
	if evt.ContentBlock.Type == "tool_use" {
		p.activeTools[evt.Index] = &toolBlock{
			id:   evt.ContentBlock.ID,
			name: evt.ContentBlock.Name,
		}
	}
}

func (p *streamProcessor) onContentBlockDelta(evt ContentBlockDeltaEvent) {
	idx := uint32(evt.Index)
	switch evt.Delta.Type {
	case "text_delta":
		d := llm.TextDelta(evt.Delta.Text)
		d.Index = &idx
		p.pub.Delta(d)
	case "thinking_delta":
		d := llm.ReasoningDelta(evt.Delta.Thinking)
		d.Index = &idx
		p.pub.Delta(d)
	case "input_json_delta":
		if tb, ok := p.activeTools[evt.Index]; ok {
			tb.jsonBuf.WriteString(evt.Delta.PartialJSON)
			d := llm.ToolDelta(tb.id, tb.name, evt.Delta.PartialJSON)
			d.Index = &idx
			p.pub.Delta(d)
		}
	}
}

func (p *streamProcessor) onContentBlockStop(evt ContentBlockStopEvent) {
	tb, ok := p.activeTools[evt.Index]
	if !ok {
		return
	}
	var args map[string]any
	if tb.jsonBuf.Len() > 0 {
		_ = json.Unmarshal([]byte(tb.jsonBuf.String()), &args)
	}
	p.pub.ToolCall(tool.NewToolCall(tb.id, tb.name, args))
	delete(p.activeTools, evt.Index)
}

func (p *streamProcessor) onError(evt StreamErrorEvent) {
	p.pub.Error(llm.NewErrProviderMsg(llm.ProviderNameAnthropic, evt.Error.Message))
}

func mapAnthropicStopReason(s string) llm.StopReason {
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
