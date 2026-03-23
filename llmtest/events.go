// Package llmtest provides helpers for testing code that consumes
// [llm.StreamEvent] channels, following the convention of packages like
// net/http/httptest.
package llmtest

import (
	"github.com/codewandler/llm"
)

// SendEvents builds a buffered, pre-populated event channel and closes it.
// Use it to construct a fake stream in tests:
//
//	ch := llmtest.SendEvents(
//	    llmtest.TextEvent("hello"),
//	    llmtest.DoneEvent(llm.StopReasonEndTurn, nil),
//	)
func SendEvents(evs ...llm.StreamEvent) <-chan llm.StreamEvent {
	ch := make(chan llm.StreamEvent, len(evs))
	for _, ev := range evs {
		ch <- ev
	}
	close(ch)
	return ch
}

// TextEvent returns a StreamEventDelta carrying a text token.
func TextEvent(s string) llm.StreamEvent {
	return llm.StreamEvent{Type: llm.StreamEventDelta, Delta: llm.TextDelta(nil, s)}
}

// ReasoningEvent returns a StreamEventDelta carrying a reasoning/thinking token.
func ReasoningEvent(s string) llm.StreamEvent {
	return llm.StreamEvent{Type: llm.StreamEventDelta, Delta: llm.ReasoningDelta(nil, s)}
}

// ToolEvent returns a StreamEventToolCall for a completed tool call.
func ToolEvent(id, name string, args map[string]any) llm.StreamEvent {
	tc := llm.ToolCall{ID: id, Name: name, Arguments: args}
	return llm.StreamEvent{Type: llm.StreamEventToolCall, ToolCall: &tc}
}

// DoneEvent returns a StreamEventDone with the given stop reason and optional
// usage statistics.
func DoneEvent(reason llm.StopReason, usage *llm.Usage) llm.StreamEvent {
	return llm.StreamEvent{Type: llm.StreamEventDone, StopReason: reason, Usage: usage}
}

func ErrorEvent(err *llm.ProviderError) llm.StreamEvent {
	return llm.StreamEvent{Type: llm.StreamEventError, Error: err}
}
