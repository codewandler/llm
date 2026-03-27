package anthropic

import (
	"context"
	"testing"
	"time"
)

// TestParseStream_LargeStreamNoDeadlock verifies that ParseStream drains
// a stream with more events than the publisher's internal buffer without
// deadlocking. 70 thinking_delta chunks exceed the 64-slot channel buffer.
func TestParseStream_LargeStreamNoDeadlock(t *testing.T) {
	events := []any{
		MessageStartEvent{Message: MessageStartPayload{
			ID: "msg_large", Model: "claude-sonnet-4-5",
			Usage: MessageUsage{InputTokens: 1},
		}},
		ContentBlockStartEvent{Index: 0, ContentBlock: ContentBlock{Type: "thinking"}},
	}
	for i := 0; i < 70; i++ {
		events = append(events, ContentBlockDeltaEvent{
			Index: 0,
			Delta: ContentBlockDelta{Type: "thinking_delta", Thinking: "word "},
		})
	}
	events = append(events,
		ContentBlockStopEvent{Index: 0},
		MessageDeltaEvent{
			Delta: MessageDelta{StopReason: "end_turn"},
			Usage: OutputUsage{OutputTokens: 70},
		},
		MessageStopEvent{},
	)

	body := BuildSSEBody(events...)

	done := make(chan struct{})
	go func() {
		ch := ParseStream(context.Background(), body, ParseOpts{
			RequestedModel: "claude-sonnet-4-5",
			ResolvedModel:  "claude-sonnet-4-5",
		})
		for range ch {
		}
		close(done)
	}()

	select {
	case <-done:
		// completed without deadlock
	case <-time.After(3 * time.Second):
		t.Fatal("ParseStream deadlocked: timed out waiting for stream to drain")
	}
}
