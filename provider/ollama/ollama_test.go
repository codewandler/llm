package ollama

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
	events := llm.NewEventStream()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(ndjson)), events, testMeta("llama3"))

	var stopReason llm.StopReason
	for event := range events.C() {
		if event.Type == llm.StreamEventDone {
			stopReason = event.StopReason
		}
	}
	assert.Equal(t, llm.StopReasonEndTurn, stopReason)
}

func TestParseStream_StopReasonMaxTokens(t *testing.T) {
	ndjson := `{"message":{"role":"assistant","content":"Hello"},"done":false}
{"message":{"role":"assistant","content":""},"done":true,"done_reason":"length"}
`
	events := llm.NewEventStream()
	go parseStream(context.Background(), io.NopCloser(strings.NewReader(ndjson)), events, testMeta("llama3"))

	var stopReason llm.StopReason
	for event := range events.C() {
		if event.Type == llm.StreamEventDone {
			stopReason = event.StopReason
		}
	}
	assert.Equal(t, llm.StopReasonMaxTokens, stopReason)
}
