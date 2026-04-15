package unified_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/api/unified"
	"github.com/codewandler/llm/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const messagesSSE = "" +
	"event: message_start\n" +
	`data: {"message":{"id":"msg_1","model":"claude-sonnet-4-6","usage":{"input_tokens":10,"cache_read_input_tokens":5,"cache_creation_input_tokens":2}}}` + "\n\n" +
	"event: content_block_start\n" +
	`data: {"index":0,"content_block":{"type":"text"}}` + "\n\n" +
	"event: content_block_delta\n" +
	`data: {"index":0,"delta":{"type":"text_delta","text":"hello world"}}` + "\n\n" +
	"event: content_block_stop\n" +
	`data: {"index":0}` + "\n\n" +
	"event: message_delta\n" +
	`data: {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}` + "\n\n" +
	"event: message_stop\n" +
	`data: {"type":"message_stop"}` + "\n\n"

func newMessagesClientSSE(sseBody string) *messages.Client {
	return messages.NewClient(
		messages.WithBaseURL("https://fake.api"),
		messages.WithHTTPClient(&http.Client{Transport: apicore.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(sseBody)),
			}, nil
		})}),
	)
}

func TestStreamMessages_TokensAndStopReason(t *testing.T) {
	client := newMessagesClientSSE(messagesSSE)
	handle, err := client.Stream(context.Background(), &messages.Request{
		Model: "claude-sonnet-4-6", MaxTokens: 64, Stream: true,
		Messages: []messages.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		unified.StreamMessages(context.Background(), handle, pub, unified.StreamContext{
			Provider: "anthropic",
			Model:    "claude-sonnet-4-6",
			CostCalc: usage.Default(),
		})
	}()

	var (
		sawStarted   bool
		sawDelta     bool
		sawContent   bool
		sawUsage     bool
		sawCompleted bool
		inputTok     int
		outputTok    int
		cacheRead    int
		cacheWrite   int
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
			sawUsage = true
			ue := ev.Data.(*llm.UsageUpdatedEvent)
			inputTok = ue.Record.Tokens.Count(usage.KindInput)
			outputTok = ue.Record.Tokens.Count(usage.KindOutput)
			cacheRead = ue.Record.Tokens.Count(usage.KindCacheRead)
			cacheWrite = ue.Record.Tokens.Count(usage.KindCacheWrite)
			assert.Equal(t, "anthropic", ue.Record.Dims.Provider)
			assert.Equal(t, "msg_1", ue.Record.Dims.RequestID)
		case llm.StreamEventCompleted:
			sawCompleted = true
			stopReason = ev.Data.(*llm.CompletedEvent).StopReason
		}
	}

	assert.True(t, sawStarted)
	assert.True(t, sawDelta)
	assert.True(t, sawContent)
	assert.True(t, sawUsage, "combined usage record must be emitted")
	assert.True(t, sawCompleted)
	assert.Equal(t, llm.StopReasonEndTurn, stopReason)
	assert.Equal(t, 10, inputTok)
	assert.Equal(t, 3, outputTok)
	assert.Equal(t, 5, cacheRead)
	assert.Equal(t, 2, cacheWrite)
}

func TestStreamMessages_ModelResolution(t *testing.T) {
	const resolvedSSE = "" +
		"event: message_start\n" +
		`data: {"message":{"id":"msg_2","model":"claude-sonnet-4-6-20251201","usage":{"input_tokens":5}}}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}` + "\n\n" +
		"event: message_stop\n" +
		`data: {"type":"message_stop"}` + "\n\n"

	client := newMessagesClientSSE(resolvedSSE)
	handle, err := client.Stream(context.Background(), &messages.Request{
		Model: "claude-sonnet-4-6", MaxTokens: 64, Stream: true,
		Messages: []messages.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		unified.StreamMessages(context.Background(), handle, pub, unified.StreamContext{
			Provider: "anthropic",
			Model:    "claude-sonnet-4-6",
		})
	}()

	var sawModelResolved bool
	for ev := range ch {
		if ev.Type == llm.StreamEventModelResolved {
			sawModelResolved = true
			mr := ev.Data.(*llm.ModelResolvedEvent)
			// ModelResolvedEvent.Resolver = provider, .Name = requested, .Resolved = API-returned
			assert.Equal(t, "anthropic", mr.Resolver)
			assert.Equal(t, "claude-sonnet-4-6", mr.Name)
			assert.Equal(t, "claude-sonnet-4-6-20251201", mr.Resolved)
		}
	}
	assert.True(t, sawModelResolved)
}

func TestStreamMessages_RateLimitsInStarted(t *testing.T) {
	client := newMessagesClientSSE(messagesSSE)
	handle, err := client.Stream(context.Background(), &messages.Request{
		Model: "claude-sonnet-4-6", MaxTokens: 64, Stream: true,
		Messages: []messages.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	rl := &llm.RateLimits{}

	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		unified.StreamMessages(context.Background(), handle, pub, unified.StreamContext{
			Provider:   "anthropic",
			Model:      "claude-sonnet-4-6",
			RateLimits: rl,
		})
	}()

	var startedExtra map[string]any
	for ev := range ch {
		if ev.Type == llm.StreamEventStarted {
			se := ev.Data.(*llm.StreamStartedEvent)
			startedExtra = se.Extra
		}
	}
	require.NotNil(t, startedExtra)
	assert.Equal(t, rl, startedExtra["rate_limits"])
}

const responsesSSE = "" +
	"event: response.created\n" +
	`data: {"response":{"id":"resp_1","model":"gpt-5.4"}}` + "\n\n" +
	"event: response.output_text.delta\n" +
	`data: {"output_index":0,"delta":"hello"}` + "\n\n" +
	"event: response.completed\n" +
	`data: {"response":{"id":"resp_1","model":"gpt-5.4","status":"completed","usage":{"input_tokens":8,"output_tokens":2}}}` + "\n\n"

func TestStreamResponses_TokensAndProvider(t *testing.T) {
	client := responses.NewClient(
		responses.WithBaseURL("https://fake.api"),
		responses.WithHTTPClient(&http.Client{Transport: apicore.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(responsesSSE)),
			}, nil
		})}),
	)

	handle, err := client.Stream(context.Background(), &responses.Request{
		Model: "gpt-5.4", Stream: true,
		Input: []responses.Input{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		unified.StreamResponses(context.Background(), handle, pub, unified.StreamContext{
			Provider: "openai",
			Model:    "gpt-5.4",
			CostCalc: usage.Default(),
		})
	}()

	var (
		sawUsage     bool
		sawCompleted bool
		inputTok     int
		outputTok    int
	)
	for ev := range ch {
		switch ev.Type {
		case llm.StreamEventUsageUpdated:
			sawUsage = true
			ue := ev.Data.(*llm.UsageUpdatedEvent)
			inputTok = ue.Record.Tokens.Count(usage.KindInput)
			outputTok = ue.Record.Tokens.Count(usage.KindOutput)
			assert.Equal(t, "openai", ue.Record.Dims.Provider)
			assert.Equal(t, "resp_1", ue.Record.Dims.RequestID)
		case llm.StreamEventCompleted:
			sawCompleted = true
			assert.Equal(t, llm.StopReasonEndTurn, ev.Data.(*llm.CompletedEvent).StopReason)
		}
	}

	assert.True(t, sawUsage)
	assert.True(t, sawCompleted)
	assert.Equal(t, 8, inputTok)
	assert.Equal(t, 2, outputTok)
}
