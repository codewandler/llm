package anthropic

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestParseStream_CacheTokens(t *testing.T) {
	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_01","model":"claude-sonnet-4-5","usage":{"input_tokens":10,"cache_creation_input_tokens":512,"cache_read_input_tokens":1024}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	pub, ch := llm.NewEventPublisher()
	body := io.NopCloser(strings.NewReader(sse))
	go ParseStream(context.Background(), body, pub, StreamMeta{
		RequestedModel: "claude-sonnet-4-5",
		ResolvedModel:  "claude-sonnet-4-5",
	})

	var doneUsage *llm.Usage
	for env := range ch {
		if env.Type == llm.StreamEventCompleted {
			ce := env.Data.(*llm.CompletedEvent)
			if ce.StopReason == llm.StopReasonEndTurn {
				doneUsage = &llm.Usage{}
			}
		}
		if env.Type == llm.StreamEventUsageUpdated {
			ue := env.Data.(*llm.UsageUpdatedEvent)
			doneUsage = &ue.Usage
		}
	}

	require.NotNil(t, doneUsage, "expected StreamEventDone with usage")
	assert.Equal(t, 1546, doneUsage.InputTokens)
	assert.Equal(t, 512, doneUsage.CacheWriteTokens, "cache creation tokens should map to CacheWriteTokens")
	assert.Equal(t, 1024, doneUsage.CacheReadTokens, "cache read tokens should map to CacheReadTokens")
	assert.Equal(t, 5, doneUsage.OutputTokens)

	assert.InDelta(t, 10.0/1_000_000*3.00, doneUsage.InputCost, 1e-10, "InputCost")
	assert.InDelta(t, 1024.0/1_000_000*0.30, doneUsage.CacheReadCost, 1e-10, "CacheReadCost")
	assert.InDelta(t, 512.0/1_000_000*3.75, doneUsage.CacheWriteCost, 1e-10, "CacheWriteCost")
	assert.InDelta(t, 5.0/1_000_000*15.00, doneUsage.OutputCost, 1e-10, "OutputCost")
	assert.InDelta(t, doneUsage.Cost, doneUsage.InputCost+doneUsage.CacheReadCost+doneUsage.CacheWriteCost+doneUsage.OutputCost, 1e-10, "breakdown should sum to Cost")
}

func TestParseStream_NoCacheTokens(t *testing.T) {
	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_02","model":"claude-haiku-4-5","usage":{"input_tokens":8}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	pub, ch := llm.NewEventPublisher()
	body := io.NopCloser(strings.NewReader(sse))
	go ParseStream(context.Background(), body, pub, StreamMeta{
		RequestedModel: "claude-haiku-4-5",
	})

	var doneUsage *llm.Usage
	for env := range ch {
		if env.Type == llm.StreamEventUsageUpdated {
			ue := env.Data.(*llm.UsageUpdatedEvent)
			doneUsage = &ue.Usage
		}
	}

	require.NotNil(t, doneUsage)
	assert.Equal(t, 0, doneUsage.CacheWriteTokens)
	assert.Equal(t, 0, doneUsage.CacheReadTokens)
}
