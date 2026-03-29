package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
)

// streamingToolBlock accumulates streaming tool-call input fragments.
type streamingToolBlock struct {
	id      string
	name    string
	jsonBuf strings.Builder
}

// streamingTextBlock accumulates streaming text content for a single content block.
type streamingTextBlock struct {
	text strings.Builder
}

// streamingThinkingBlock accumulates streaming ThinkingConfig content and its signature.
type streamingThinkingBlock struct {
	thinking  strings.Builder
	signature strings.Builder
}

// streamProcessor owns all mutable state and the per-event dispatch logic for
// an Anthropic-format SSE stream. It is the single authoritative place for the
// rule "when event X arrives, do Y to the publisher".
//
// ParseStream feeds it via dispatch; tests feed it directly via the on* methods.
type streamProcessor struct {
	meta           ParseOpts
	pub            llm.Publisher
	activeTools    map[int]*streamingToolBlock
	activeText     map[int]*streamingTextBlock
	activeThinking map[int]*streamingThinkingBlock
	usage          llm.Usage
	stopReason     llm.StopReason
	rateLimits     *llm.RateLimits
}

func newStreamProcessor(meta ParseOpts, pub llm.Publisher) *streamProcessor {
	rl := llm.ParseRateLimits(meta.ResponseHeaders)
	return &streamProcessor{
		meta:           meta,
		pub:            pub,
		activeTools:    make(map[int]*streamingToolBlock),
		activeText:     make(map[int]*streamingTextBlock),
		activeThinking: make(map[int]*streamingThinkingBlock),
		rateLimits:     rl,
	}
}

// dispatch JSON-decodes one SSE data line and routes to the appropriate on*
// method. Returns false when the stream should stop (message_stop or error).
// When returning false after "error", the error has already been published by
// onError — no second error event will be emitted.
func (p *streamProcessor) dispatch(data string) bool {
	if data == "" {
		return true
	}
	b := []byte(data)
	var base struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &base); err != nil {
		return true
	}
	switch base.Type {
	case "message_start":
		var evt MessageStartEvent
		if err := json.Unmarshal(b, &evt); err == nil {
			p.onMessageStart(evt)
		}
	case "message_delta":
		var evt MessageDeltaEvent
		if err := json.Unmarshal(b, &evt); err == nil {
			p.onMessageDelta(evt)
		}
	case "message_stop":
		p.onMessageStop()
		return false
	case "content_block_start":
		var evt ContentBlockStartEvent
		if err := json.Unmarshal(b, &evt); err == nil {
			p.onContentBlockStart(evt)
		}
	case "content_block_delta":
		var evt ContentBlockDeltaEvent
		if err := json.Unmarshal(b, &evt); err == nil {
			p.onContentBlockDelta(evt)
		}
	case "content_block_stop":
		var evt ContentBlockStopEvent
		if err := json.Unmarshal(b, &evt); err == nil {
			p.onContentBlockStop(evt)
		}
	case "error":
		// error was published by onError below; return false to stop the loop
		// without emitting a second error event.
		var evt StreamErrorEvent
		if err := json.Unmarshal(b, &evt); err == nil {
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

	// Build Extra map with rate limits if available
	var extra map[string]any
	if p.rateLimits != nil {
		extra = map[string]any{
			"rate_limits": p.rateLimits,
		}
	}

	p.pub.Started(llm.StreamStartedEvent{
		Model:     evt.Message.Model,
		RequestID: evt.Message.ID,
		Extra:     extra,
	})
}

func (p *streamProcessor) onMessageDelta(evt MessageDeltaEvent) {
	p.usage.OutputTokens = evt.Usage.OutputTokens
	p.usage.TotalTokens = p.usage.InputTokens + p.usage.OutputTokens
	p.stopReason = mapAnthropicStopReason(evt.Delta.StopReason)
}

func (p *streamProcessor) onMessageStop() {
	costFn := p.meta.CostFn
	if costFn == nil {
		costFn = FillCost
	}
	costFn(p.meta.Model, &p.usage)
	p.pub.Usage(p.usage)
	p.pub.Completed(llm.CompletedEvent{StopReason: p.stopReason})
}

func (p *streamProcessor) onContentBlockStart(evt ContentBlockStartEvent) {
	switch evt.ContentBlock.Type {
	case "tool_use":
		p.activeTools[evt.Index] = &streamingToolBlock{
			id:   evt.ContentBlock.ID,
			name: evt.ContentBlock.Name,
		}
	case "text":
		p.activeText[evt.Index] = &streamingTextBlock{}
	case "thinking":
		p.activeThinking[evt.Index] = &streamingThinkingBlock{}
	}
}

func (p *streamProcessor) onContentBlockDelta(evt ContentBlockDeltaEvent) {
	idx := uint32(evt.Index)
	switch evt.Delta.Type {
	case "text_delta":
		// Accumulate for block-level tracking
		if tb, ok := p.activeText[evt.Index]; ok {
			tb.text.WriteString(evt.Delta.Text)
		}
		// Also stream per-token delta for TUI display
		d := llm.TextDelta(evt.Delta.Text)
		d.Index = &idx
		p.pub.Delta(d)
	case "thinking_delta":
		// Accumulate for block-level tracking
		if tb, ok := p.activeThinking[evt.Index]; ok {
			tb.thinking.WriteString(evt.Delta.Thinking)
			tb.signature.WriteString(evt.Delta.Signature)
		}
		// Also stream per-token delta for TUI display
		d := llm.ThinkingDelta(evt.Delta.Thinking)
		d.Index = &idx
		p.pub.Delta(d)
	case "signature_delta":
		// Capture the cryptographic signature for the ThinkingConfig block.
		// It arrives as a single event (not streamed char-by-char).
		if tb, ok := p.activeThinking[evt.Index]; ok {
			tb.signature.WriteString(evt.Delta.Signature)
		}
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
	// Thought blocks: emit ContentBlockEvent with the accumulated ThinkingConfig
	// text and the signature required for tool-use loop re-submission.
	if tb, ok := p.activeThinking[evt.Index]; ok {
		p.pub.ContentBlock(llm.ContentPartEvent{
			Part:  msg.Thinking(tb.thinking.String(), tb.signature.String()),
			Index: evt.Index,
		})
		delete(p.activeThinking, evt.Index)
	}

	// Text blocks: emit ContentBlockEvent so the caller can store the full
	// block and preserve its index position relative to other blocks.
	if tb, ok := p.activeText[evt.Index]; ok {
		p.pub.ContentBlock(llm.ContentPartEvent{
			Part:  msg.Text(tb.text.String()),
			Index: evt.Index,
		})
		delete(p.activeText, evt.Index)
		return
	}

	// Tool blocks: emit ToolCall event (existing behaviour, unchanged).
	if tb, ok := p.activeTools[evt.Index]; ok {
		var args map[string]any
		if tb.jsonBuf.Len() > 0 {
			_ = json.Unmarshal([]byte(tb.jsonBuf.String()), &args)
		}
		p.pub.ToolCall(tool.NewToolCall(tb.id, tb.name, args))
		delete(p.activeTools, evt.Index)
		return
	}

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
