package anthropic

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/usage"
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

	body := io.NopCloser(strings.NewReader(sse))
	ch := ParseStream(context.Background(), body, ParseOpts{
		Model:        "claude-sonnet-4-5",
		ProviderName: providerName,
	})

	var doneRec *usage.Record
	for env := range ch {
		if env.Type == llm.StreamEventUsageUpdated {
			ue := env.Data.(*llm.UsageUpdatedEvent)
			doneRec = &ue.Record
		}
	}

	require.NotNil(t, doneRec, "expected StreamEventUsageUpdated")
	// InputTokens = 10 (non-cache), CacheWrite = 512, CacheRead = 1024
	assert.Equal(t, 10, doneRec.Tokens.Count(usage.KindInput))
	assert.Equal(t, 512, doneRec.Tokens.Count(usage.KindCacheWrite), "cache creation tokens should map to KindCacheWrite")
	assert.Equal(t, 1024, doneRec.Tokens.Count(usage.KindCacheRead), "cache read tokens should map to KindCacheRead")
	assert.Equal(t, 5, doneRec.Tokens.Count(usage.KindOutput))

	assert.InDelta(t, 10.0/1_000_000*3.00, doneRec.Cost.Input, 1e-10, "Input cost")
	assert.InDelta(t, 1024.0/1_000_000*0.30, doneRec.Cost.CacheRead, 1e-10, "CacheRead cost")
	assert.InDelta(t, 512.0/1_000_000*3.75, doneRec.Cost.CacheWrite, 1e-10, "CacheWrite cost")
	assert.InDelta(t, 5.0/1_000_000*15.00, doneRec.Cost.Output, 1e-10, "Output cost")
	assert.InDelta(t, doneRec.Cost.Total, doneRec.Cost.Input+doneRec.Cost.CacheRead+doneRec.Cost.CacheWrite+doneRec.Cost.Output, 1e-10, "breakdown should sum to Total")
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

	body := io.NopCloser(strings.NewReader(sse))
	ch := ParseStream(context.Background(), body, ParseOpts{
		Model:        "claude-haiku-4-5",
		ProviderName: providerName,
	})

	var doneRec *usage.Record
	for env := range ch {
		if env.Type == llm.StreamEventUsageUpdated {
			ue := env.Data.(*llm.UsageUpdatedEvent)
			doneRec = &ue.Record
		}
	}

	require.NotNil(t, doneRec)
	assert.Equal(t, 0, doneRec.Tokens.Count(usage.KindCacheWrite))
	assert.Equal(t, 0, doneRec.Tokens.Count(usage.KindCacheRead))
}
