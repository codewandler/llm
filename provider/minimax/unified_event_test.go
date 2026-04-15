package minimax

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/api/unified"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnifiedMessagesEventPipeline_MiniMaxThinkingStillWorks(t *testing.T) {
	sseBody := "event: message_start\ndata: {\"message\":{\"id\":\"msg_1\",\"model\":\"MiniMax-M2.7\",\"usage\":{\"input_tokens\":10}}}\n\n" +
		"event: content_block_start\ndata: {\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n" +
		"event: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"hmm\"}}\n\n" +
		"event: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig1\"}}\n\n" +
		"event: content_block_stop\ndata: {\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	client := messages.NewClient(
		messages.WithBaseURL("https://fake.api"),
		messages.WithHTTPClient(&http.Client{Transport: minimaxRT(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(sseBody)),
			}, nil
		})}),
	)

	handle, err := client.Stream(context.Background(), &messages.Request{
		Model:     "MiniMax-M2.7",
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
			uEv, ignored, convErr := unified.MapMessagesEvent(r.Event)
			if convErr != nil {
				pub.Error(convErr)
				return
			}
			if ignored {
				continue
			}
			if err := unified.PublishToLLM(pub, uEv); err != nil {
				pub.Error(err)
				return
			}
		}
	}()

	var sawThinking bool
	for ev := range ch {
		if ev.Type != llm.StreamEventContentPart {
			continue
		}
		cp := ev.Data.(*llm.ContentPartEvent)
		if cp.Part.Type == "thinking" && cp.Part.Thinking != nil && cp.Part.Thinking.Text == "hmm" {
			sawThinking = true
		}
	}

	assert.True(t, sawThinking)
}

type minimaxRT func(*http.Request) (*http.Response, error)

func (f minimaxRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
