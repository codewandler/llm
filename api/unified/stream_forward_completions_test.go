package unified_test

import (
	"context"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/completions"
	"github.com/codewandler/llm/api/unified"
	"github.com/stretchr/testify/assert"
)

func TestForwardCompletions_ToolCallAggregation(t *testing.T) {
	chunks := []*completions.Chunk{
		{
			ID:    "chatcmpl-1",
			Model: "openai/gpt-4o",
			Choices: []completions.Choice{{
				Delta: completions.Delta{ToolCalls: []completions.ToolCallDelta{{
					Index:    0,
					ID:       "call_abc",
					Function: completions.FuncCallDelta{Name: "get_weather"},
				}}},
			}},
		},
		{
			Choices: []completions.Choice{{
				Delta: completions.Delta{ToolCalls: []completions.ToolCallDelta{{
					Index:    0,
					Function: completions.FuncCallDelta{Arguments: `{"location"`},
				}}},
			}},
		},
		{
			Choices: []completions.Choice{{
				Delta: completions.Delta{ToolCalls: []completions.ToolCallDelta{{
					Index:    0,
					Function: completions.FuncCallDelta{Arguments: `:"Paris"}`},
				}}},
			}},
		},
		{
			Choices: []completions.Choice{{FinishReason: stringPtr(completions.FinishReasonToolCalls)}},
		},
	}

	events := make(chan apicore.StreamResult, len(chunks))
	for _, chunk := range chunks {
		events <- apicore.StreamResult{Event: chunk}
	}
	close(events)

	handle := &apicore.StreamHandle{Events: events}
	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		unified.ForwardCompletions(context.Background(), handle, pub, unified.StreamContext{Provider: "openrouter", Model: "openai/gpt-4o"})
	}()

	var (
		fragments   []string
		sawToolCall bool
		toolArgs    map[string]any
	)
	for ev := range ch {
		switch ev.Type {
		case llm.StreamEventDelta:
			if de, ok := ev.Data.(*llm.DeltaEvent); ok && de.Kind == llm.DeltaKindTool {
				fragments = append(fragments, de.ToolArgs)
			}
		case llm.StreamEventToolCall:
			call := ev.Data.(*llm.ToolCallEvent).ToolCall
			sawToolCall = true
			toolArgs = call.ToolArgs()
		}
	}

	var nonEmpty []string
	for _, frag := range fragments {
		if frag != "" {
			nonEmpty = append(nonEmpty, frag)
		}
	}
	assert.ElementsMatch(t, []string{`{"location"`, `:"Paris"}`}, nonEmpty)
	assert.True(t, sawToolCall)
	assert.Equal(t, "Paris", toolArgs["location"])
}

func stringPtr(s string) *string { return &s }
