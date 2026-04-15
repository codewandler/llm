package anthropic

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/api/unified"
	"github.com/codewandler/llm/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnifiedMessagesEventPipeline_Parity(t *testing.T) {
	sseBody := "event: message_start\ndata: {\"message\":{\"id\":\"msg_1\",\"model\":\"claude-sonnet-4-6\",\"usage\":{\"input_tokens\":10,\"cache_read_input_tokens\":5,\"cache_creation_input_tokens\":2}}}\n\n" +
		"event: content_block_start\ndata: {\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n" +
		"event: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
		"event: content_block_stop\ndata: {\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	client := messages.NewClient(
		messages.WithBaseURL("https://fake.api"),
		messages.WithHTTPClient(&http.Client{Transport: messagesRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(sseBody)),
			}, nil
		})}),
	)

	handle, err := client.Stream(context.Background(), &messages.Request{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 64,
		Stream:    true,
		Messages:  []messages.Message{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)

	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		for r := range handle.Events {
			if r.Err != nil {
				pub.Error(r.Err)
				return
			}
			uEv, ignored, convErr := unified.EventFromMessages(r.Event)
			if convErr != nil {
				pub.Error(convErr)
				return
			}
			if ignored {
				continue
			}
			if err := unified.Publish(pub, uEv); err != nil {
				pub.Error(err)
				return
			}
		}
	}()

	var (
		sawStarted   bool
		sawDelta     bool
		sawContent   bool
		inputTokens  int
		outputTokens int
		cacheRead    int
		cacheWrite   int
		sawDone      bool
		stopReason   llm.StopReason
	)
	for ev := range ch {
		switch ev.Type {
		case llm.StreamEventStarted:
			sawStarted = true
		case llm.StreamEventDelta:
			sawDelta = true
		case llm.StreamEventContentPart:
			sawContent = true
		case llm.StreamEventUsageUpdated:
			ue := ev.Data.(*llm.UsageUpdatedEvent)
			inputTokens += ue.Record.Tokens.Count(usage.KindInput)
			outputTokens += ue.Record.Tokens.Count(usage.KindOutput)
			cacheRead += ue.Record.Tokens.Count(usage.KindCacheRead)
			cacheWrite += ue.Record.Tokens.Count(usage.KindCacheWrite)
		case llm.StreamEventCompleted:
			sawDone = true
			stopReason = ev.Data.(*llm.CompletedEvent).StopReason
		}
	}

	assert.True(t, sawStarted)
	assert.True(t, sawDelta)
	assert.True(t, sawContent)
	assert.True(t, sawDone)
	assert.Equal(t, llm.StopReasonEndTurn, stopReason)
	// input tokens from message_start, output from message_delta
	assert.Equal(t, 10, inputTokens)
	assert.Equal(t, 3, outputTokens)
	assert.Equal(t, 5, cacheRead)
	assert.Equal(t, 2, cacheWrite)
}

type messagesRoundTripFunc func(*http.Request) (*http.Response, error)

func (f messagesRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
