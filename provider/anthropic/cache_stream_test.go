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
	// Minimal Anthropic SSE stream with cache token counts in message_start.
	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_01","model":"claude-sonnet-4-6","usage":{"input_tokens":10,"cache_creation_input_tokens":512,"cache_read_input_tokens":1024}}}`,
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

	events := make(chan llm.StreamEvent, 64)
	body := io.NopCloser(strings.NewReader(sse))
	go ParseStream(context.Background(), body, events, StreamMeta{
		RequestedModel: "claude-sonnet-4-6",
	})

	var doneUsage *llm.Usage
	for ev := range events {
		if ev.Type == llm.StreamEventDone {
			doneUsage = ev.Usage
		}
	}

	require.NotNil(t, doneUsage, "expected StreamEventDone with usage")
	assert.Equal(t, 10, doneUsage.InputTokens)
	assert.Equal(t, 512, doneUsage.CacheWriteTokens, "cache creation tokens should map to CacheWriteTokens")
	assert.Equal(t, 1024, doneUsage.CachedTokens, "cache read tokens should map to CachedTokens")
	assert.Equal(t, 5, doneUsage.OutputTokens)
}

func TestParseStream_NoCacheTokens(t *testing.T) {
	// Stream without cache fields — both fields should remain 0.
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

	events := make(chan llm.StreamEvent, 64)
	body := io.NopCloser(strings.NewReader(sse))
	go ParseStream(context.Background(), body, events, StreamMeta{
		RequestedModel: "claude-haiku-4-5",
	})

	var doneUsage *llm.Usage
	for ev := range events {
		if ev.Type == llm.StreamEventDone {
			doneUsage = ev.Usage
		}
	}

	require.NotNil(t, doneUsage)
	assert.Equal(t, 0, doneUsage.CacheWriteTokens)
	assert.Equal(t, 0, doneUsage.CachedTokens)
}
