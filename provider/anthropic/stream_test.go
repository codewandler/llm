package anthropic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
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

	body := buildSSEBody(events...)

	done := make(chan struct{})
	go func() {
		ch := ParseStream(context.Background(), body, ParseOpts{
			Model: "claude-sonnet-4-5",
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

func TestParseStream_EmitsRequestParams(t *testing.T) {
	req := &llm.Request{
		Model:          "claude-sonnet-4-6-20251120",
		Messages:       llm.Messages{llm.User("hi")},
		ThinkingEffort: llm.ThinkingEffortHigh,
		OutputEffort:   llm.OutputEffortHigh,
	}
	params := map[string]any{
		"model":      "claude-sonnet-4-6-20251120",
		"max_tokens": 32000,
		"thinking":   map[string]any{"type": "adaptive", "budget_tokens": 16000},
	}

	body := buildSSEBody(
		MessageStartEvent{Message: MessageStartPayload{
			ID: "msg_1", Model: "claude-sonnet-4-6-20251120",
			Usage: MessageUsage{InputTokens: 1},
		}},
		ContentBlockStartEvent{Index: 0, ContentBlock: ContentBlock{Type: "text"}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "text_delta", Text: "hi"}},
		ContentBlockStopEvent{Index: 0},
		MessageDeltaEvent{
			Delta: MessageDelta{StopReason: "end_turn"},
			Usage: OutputUsage{OutputTokens: 1},
		},
		MessageStopEvent{},
	)

	ch := ParseStream(context.Background(), body, ParseOpts{
		Model:         "claude-sonnet-4-6-20251120",
		LLMRequest:    req,
		RequestParams: params,
	})

	var foundParams bool
	for env := range ch {
		if env.Type == llm.StreamEventRequestParams {
			foundParams = true
			rpe, ok := env.Data.(*llm.RequestParamsEvent)
			if !ok {
				t.Fatalf("expected *RequestParamsEvent, got %T", env.Data)
			}

			// LLMRequest
			require.NotNil(t, rpe.LLMRequest, "LLMRequest must be set")
			assert.Equal(t, "claude-sonnet-4-6-20251120", rpe.LLMRequest.Model)
			assert.Equal(t, llm.ThinkingEffortHigh, rpe.LLMRequest.ThinkingEffort)
			assert.Equal(t, llm.OutputEffortHigh, rpe.LLMRequest.OutputEffort)

			// ProviderRequestParams
			assert.Equal(t, "claude-sonnet-4-6-20251120", rpe.ProviderRequestParams["model"])
			thinking, _ := rpe.ProviderRequestParams["thinking"].(map[string]any)
			assert.Equal(t, "adaptive", thinking["type"])
			assert.Equal(t, 16000, thinking["budget_tokens"])
		}
	}
	if !foundParams {
		t.Error("expected request_params event but none was emitted")
	}
}

func TestParseStream_NoRequestParamsWhenNil(t *testing.T) {
	body := buildSSEBody(
		MessageStartEvent{Message: MessageStartPayload{
			ID: "msg_1", Model: "claude-sonnet-4-5",
			Usage: MessageUsage{InputTokens: 1},
		}},
		MessageDeltaEvent{
			Delta: MessageDelta{StopReason: "end_turn"},
			Usage: OutputUsage{OutputTokens: 1},
		},
		MessageStopEvent{},
	)

	ch := ParseStream(context.Background(), body, ParseOpts{
		Model: "claude-sonnet-4-5",
		// LLMRequest and RequestParams both nil
	})

	for env := range ch {
		if env.Type == llm.StreamEventRequestParams {
			t.Error("should NOT emit request_params when both fields are nil")
		}
	}
}
