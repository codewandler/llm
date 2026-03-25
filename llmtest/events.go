// Package llmtest provides helpers for testing code that consumes
// [llm.Stream] channels, following the convention of packages like
// net/http/httptest.
package llmtest

import (
	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
)

// SendEvents builds a buffered, pre-populated Envelope channel and closes it.
// Use it to construct a fake stream in tests:
//
//	ch := llmtest.SendEvents(
//	    llmtest.TextEvent("hello"),
//	    llmtest.CompletedEvent(llm.StopReasonEndTurn, nil),
//	)
func SendEvents(evs ...llm.Event) <-chan llm.Envelope {
	ch := make(chan llm.Envelope, len(evs))
	for _, ev := range evs {
		ch <- llm.Envelope{Type: ev.Type(), Data: ev}
	}
	close(ch)
	return ch
}
func TextEvent(s string) llm.Event                   { return llm.TextDelta(s) }
func ReasoningEvent(s string) llm.Event              { return llm.ReasoningDelta(s) }
func CompletedEvent(reason llm.StopReason) llm.Event { return &llm.CompletedEvent{StopReason: reason} }
func ErrorEvent(err *llm.ProviderError) llm.Event    { return &llm.ErrorEvent{Error: err} }
func UsageEvent(u llm.Usage) llm.Event               { return &llm.UsageUpdatedEvent{Usage: u} }
func ToolEvent(id, name string, args map[string]any) llm.Event {
	return &llm.ToolCallEvent{
		ToolCall: tool.NewToolCall(id, name, args),
	}
}
