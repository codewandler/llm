package minimax

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/stretchr/testify/require"
)

// TestMinimaxParseStream_ReasoningTokensAreEmitted verifies that a stream
// containing a thinking_delta actually produces ReasoningDelta events on the
// returned channel.
//
// This is the end-to-end proof of the reasoning-display pipeline at the
// provider layer: minimax → anthropic.ParseStream → llm.Publisher → channel.
func TestMinimaxParseStream_ReasoningTokensAreEmitted(t *testing.T) {
	body := anthropic.BuildSSEBody(
		anthropic.MessageStartEvent{Message: anthropic.MessageStartPayload{
			ID: "msg_01", Model: "MiniMax-Text-01",
			Usage: anthropic.MessageUsage{InputTokens: 42},
		}},
		anthropic.ContentBlockStartEvent{Index: 0, ContentBlock: anthropic.ContentBlock{Type: "thinking"}},
		anthropic.ContentBlockDeltaEvent{Index: 0, Delta: anthropic.ContentBlockDelta{
			Type: "thinking_delta", Thinking: "I need to think about this carefully.",
		}},
		anthropic.ContentBlockStopEvent{Index: 0},
		anthropic.ContentBlockStartEvent{Index: 1, ContentBlock: anthropic.ContentBlock{Type: "text"}},
		anthropic.ContentBlockDeltaEvent{Index: 1, Delta: anthropic.ContentBlockDelta{
			Type: "text_delta", Text: "Here is my answer.",
		}},
		anthropic.ContentBlockStopEvent{Index: 1},
		anthropic.MessageDeltaEvent{
			Delta: anthropic.MessageDelta{StopReason: "end_turn"},
			Usage: anthropic.OutputUsage{OutputTokens: 15},
		},
		anthropic.MessageStopEvent{},
	)

	ch := anthropic.ParseStream(context.Background(), body, anthropic.ParseOpts{
		RequestedModel: "minimax-m27",
		ResolvedModel:  "minimax-m27",
		CostFn:         FillCost,
	})

	var reasoningEvents []string
	var textEvents []string

	for env := range ch {
		if env.Type != llm.StreamEventDelta {
			continue
		}
		delta, ok := env.Data.(*llm.DeltaEvent)
		require.True(t, ok)
		if delta.Reasoning != "" {
			reasoningEvents = append(reasoningEvents, delta.Reasoning)
		}
		if delta.Text != "" {
			textEvents = append(textEvents, delta.Text)
		}
	}

	t.Logf("reasoning events: %v", reasoningEvents)
	t.Logf("text events: %v", textEvents)

	require.NotEmpty(t, reasoningEvents,
		"reasoning tokens must appear as ReasoningDelta events; none received")
	require.NotEmpty(t, textEvents,
		"text tokens must appear as TextDelta events; none received")
	require.Equal(t, "I need to think about this carefully.", strings.Join(reasoningEvents, ""))
	require.Equal(t, "Here is my answer.", strings.Join(textEvents, ""))
}

// TestMinimaxParseStream_CostFnApplied verifies that the MiniMax CostFn is
// used (not Anthropic's default) when parsing a stream via anthropic.ParseStream.
func TestMinimaxParseStream_CostFnApplied(t *testing.T) {
	body := anthropic.BuildSSEBody(
		anthropic.MessageStartEvent{Message: anthropic.MessageStartPayload{
			ID: "msg_cost", Model: ModelM27,
			Usage: anthropic.MessageUsage{InputTokens: 1000},
		}},
		anthropic.ContentBlockStartEvent{Index: 0, ContentBlock: anthropic.ContentBlock{Type: "text"}},
		anthropic.ContentBlockDeltaEvent{Index: 0, Delta: anthropic.ContentBlockDelta{
			Type: "text_delta", Text: "Hello",
		}},
		anthropic.ContentBlockStopEvent{Index: 0},
		anthropic.MessageDeltaEvent{
			Delta: anthropic.MessageDelta{StopReason: "end_turn"},
			Usage: anthropic.OutputUsage{OutputTokens: 500},
		},
		anthropic.MessageStopEvent{},
	)

	ch := anthropic.ParseStream(context.Background(), body, anthropic.ParseOpts{
		RequestedModel: ModelM27,
		ResolvedModel:  ModelM27,
		CostFn:         FillCost,
	})

	var usage *llm.Usage
	for env := range ch {
		if env.Type == llm.StreamEventUsageUpdated {
			ue := env.Data.(*llm.UsageUpdatedEvent)
			usage = &ue.Usage
		}
	}

	require.NotNil(t, usage, "expected usage event")
	require.Greater(t, usage.Cost, 0.0, "CostFn should have populated cost")

	// Verify MiniMax pricing was used, not Anthropic's.
	// MiniMax M2.7 input: $0.30/M, output: $1.20/M
	expectedInput := float64(1000) / 1_000_000 * 0.30
	expectedOutput := float64(500) / 1_000_000 * 1.20
	require.InDelta(t, expectedInput, usage.InputCost, 1e-10)
	require.InDelta(t, expectedOutput, usage.OutputCost, 1e-10)
	require.InDelta(t, expectedInput+expectedOutput, usage.Cost, 1e-10)
}

// TestMinimaxParseStream_LargeResponse verifies that ParseStream handles
// large streams without deadlock when a concurrent reader is draining.
func TestMinimaxParseStream_LargeResponse(t *testing.T) {
	// Build a stream with 70 thinking_delta chunks — more than the 64-slot buffer.
	events := []any{
		anthropic.MessageStartEvent{Message: anthropic.MessageStartPayload{
			ID: "msg_01", Model: "minimax",
			Usage: anthropic.MessageUsage{InputTokens: 1},
		}},
		anthropic.ContentBlockStartEvent{Index: 0, ContentBlock: anthropic.ContentBlock{Type: "thinking"}},
	}
	for i := 0; i < 70; i++ {
		events = append(events, anthropic.ContentBlockDeltaEvent{
			Index: 0,
			Delta: anthropic.ContentBlockDelta{Type: "thinking_delta", Thinking: "word "},
		})
	}
	events = append(events,
		anthropic.ContentBlockStopEvent{Index: 0},
		anthropic.MessageDeltaEvent{
			Delta: anthropic.MessageDelta{StopReason: "end_turn"},
			Usage: anthropic.OutputUsage{OutputTokens: 70},
		},
		anthropic.MessageStopEvent{},
	)

	body := anthropic.BuildSSEBody(events...)

	done := make(chan struct{})
	go func() {
		ch := anthropic.ParseStream(context.Background(), body, anthropic.ParseOpts{
			RequestedModel: "minimax",
			ResolvedModel:  "minimax",
			CostFn:         FillCost,
		})
		for range ch {
		}
		close(done)
	}()

	select {
	case <-done:
		// Good — completed without deadlock.
	case <-time.After(3 * time.Second):
		t.Fatal("ParseStream deadlocked: channel buffer full, nobody reading")
	}
}
