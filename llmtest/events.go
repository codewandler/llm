// Package llmtest provides helpers for testing code that consumes
// [llm.Stream] channels, following the convention of packages like
// net/http/httptest.
package llmtest

import (
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

// SendEvents builds a buffered, pre-populated Envelope channel and closes it.
// Use it to construct a fake stream in tests:
//
//	ch := llmtest.SendEvents(
//	    llmtest.TextEvent("hello"),
//	    llmtest.CompletedEvent(llm.StopReasonEndTurn),
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
func ReasoningEvent(s string) llm.Event              { return llm.ThinkingDelta(s) }
func CompletedEvent(reason llm.StopReason) llm.Event { return &llm.CompletedEvent{StopReason: reason} }
func ErrorEvent(err *llm.ProviderError) llm.Event    { return &llm.ErrorEvent{Error: err} }

// UsageEvent builds a UsageUpdatedEvent from a usage.Record.
// For simple tests that only need token counts, use UsageTokenEvent.
func UsageEvent(rec usage.Record) llm.Event { return &llm.UsageUpdatedEvent{Record: rec} }

// UsageTokenEvent builds a minimal UsageUpdatedEvent with the given token counts.
// provider and model are optional; pass empty strings to omit them.
func UsageTokenEvent(provider, model string, inputTokens, outputTokens int) llm.Event {
	return &llm.UsageUpdatedEvent{
		Record: usage.Record{
			Dims:       usage.Dims{Provider: provider, Model: model},
			Tokens:     usage.TokenItems{{Kind: usage.KindInput, Count: inputTokens}, {Kind: usage.KindOutput, Count: outputTokens}},
			RecordedAt: time.Now(),
		},
	}
}

func ToolEvent(id, name string, args map[string]any) llm.Event {
	return &llm.ToolCallEvent{
		ToolCall: tool.NewToolCall(id, name, args),
	}
}
