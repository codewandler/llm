package ollama

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func testMeta(model string) streamMeta {
	return streamMeta{
		RequestedModel: model,
		ResolvedModel:  model,
		StartTime:      time.Now(),
	}
}

func TestParseStream_StopReasonEndTurn(t *testing.T) {
	ndjson := `{"message":{"role":"assistant","content":"Hello"},"done":false}
{"message":{"role":"assistant","content":""},"done":true,"done_reason":"stop"}
`
	pub, ch := llm.NewEventPublisher()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(ndjson)), pub, testMeta("llama3"))

	var stopReason llm.StopReason
	for event := range ch {
		if event.Type == llm.StreamEventCompleted {
			ce, ok := event.Data.(*llm.CompletedEvent)
			require.True(t, ok, "expected CompletedEvent")
			stopReason = ce.StopReason
		}
	}
	assert.Equal(t, llm.StopReasonEndTurn, stopReason)
}

func TestParseStream_StopReasonMaxTokens(t *testing.T) {
	ndjson := `{"message":{"role":"assistant","content":"Hello"},"done":false}
{"message":{"role":"assistant","content":""},"done":true,"done_reason":"length"}
`
	pub, ch := llm.NewEventPublisher()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(ndjson)), pub, testMeta("llama3"))

	var stopReason llm.StopReason
	for event := range ch {
		if event.Type == llm.StreamEventCompleted {
			ce, ok := event.Data.(*llm.CompletedEvent)
			require.True(t, ok, "expected CompletedEvent")
			stopReason = ce.StopReason
		}
	}
	assert.Equal(t, llm.StopReasonMaxTokens, stopReason)
}
