package unified_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/completions"
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

func TestForwardMessages_TokensAndStopReason(t *testing.T) {
	client := newMessagesClientSSE(messagesSSE)
	handle, err := client.Stream(context.Background(), &messages.Request{
		Model: "claude-sonnet-4-6", MaxTokens: 64, Stream: true,
		Messages: []messages.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		unified.ForwardMessages(context.Background(), handle, pub, unified.StreamContext{
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

func TestForwardMessages_ModelResolution(t *testing.T) {
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
		unified.ForwardMessages(context.Background(), handle, pub, unified.StreamContext{
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

func TestForwardMessages_RateLimitsInStarted(t *testing.T) {
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
		unified.ForwardMessages(context.Background(), handle, pub, unified.StreamContext{
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

func TestForwardCompletions_ToolCallAggregation(t *testing.T) {
	chunks := []*completions.Chunk{
		{
			ID:    "chatcmpl-1",
			Model: "openai/gpt-4o",
			Choices: []completions.Choice{{
				Delta: completions.Delta{
					ToolCalls: []completions.ToolCallDelta{{
						Index:    0,
						ID:       "call_abc",
						Function: completions.FuncCallDelta{Name: "get_weather"},
					}},
				},
			}},
		},
		{
			Choices: []completions.Choice{{
				Delta: completions.Delta{
					ToolCalls: []completions.ToolCallDelta{{
						Index:    0,
						Function: completions.FuncCallDelta{Arguments: "{\"location\""},
					}},
				},
			}},
		},
		{
			Choices: []completions.Choice{{
				Delta: completions.Delta{
					ToolCalls: []completions.ToolCallDelta{{
						Index:    0,
						Function: completions.FuncCallDelta{Arguments: ":\"Paris\"}"},
					}},
				},
			}},
		},
		{
			Choices: []completions.Choice{{
				FinishReason: stringPtr(completions.FinishReasonToolCalls),
			}},
		},
	}

	events := make(chan apicore.StreamResult, len(chunks)+1)
	for _, chunk := range chunks {
		events <- apicore.StreamResult{Event: chunk}
	}
	close(events)

	handle := &apicore.StreamHandle{
		Events: events,
	}

	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		unified.ForwardCompletions(context.Background(), handle, pub, unified.StreamContext{
			Provider: "openrouter",
			Model:    "openai/gpt-4o",
		})
	}()

	var (
		fragments   []string
		sawToolCall bool
		toolArgs    map[string]any
	)
	for ev := range ch {
		switch ev.Type {
		case llm.StreamEventDelta:
			if de, ok := ev.Data.(*llm.DeltaEvent); ok && de.Kind == llm.DeltaKindTool {
				fragments = append(fragments, de.ToolArgs)
			}
		case llm.StreamEventToolCall:
			call := ev.Data.(*llm.ToolCallEvent).ToolCall
			sawToolCall = true
			toolArgs = call.ToolArgs()
		}
	}

	var nonEmpty []string
	for _, frag := range fragments {
		if frag != "" {
			nonEmpty = append(nonEmpty, frag)
		}
	}
	assert.ElementsMatch(t, []string{"{\"location\"", ":\"Paris\"}"}, nonEmpty)
	assert.True(t, sawToolCall)
	assert.Equal(t, "Paris", toolArgs["location"])
}

func stringPtr(s string) *string { return &s }

const responsesSSE = "" +
	"event: response.created\n" +
	`data: {"response":{"id":"resp_1","model":"gpt-5.4"}}` + "\n\n" +
	"event: response.output_text.delta\n" +
	`data: {"output_index":0,"delta":"hello"}` + "\n\n" +
	"event: response.completed\n" +
	`data: {"response":{"id":"resp_1","model":"gpt-5.4","status":"completed","usage":{"input_tokens":8,"output_tokens":2}}}` + "\n\n"

func TestForwardResponses_TokensAndProvider(t *testing.T) {
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
		unified.ForwardResponses(context.Background(), handle, pub, unified.StreamContext{
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

func TestForwardResponses_InfersToolUseStopReason(t *testing.T) {
	events := make(chan apicore.StreamResult, 4)
	events <- apicore.StreamResult{Event: &responses.ResponseCreatedEvent{Response: responses.ResponsePayload{ID: "resp_tool", Model: "gpt-5.4"}}}
	events <- apicore.StreamResult{Event: &responses.FunctionCallArgumentsDeltaEvent{OutputRef: responses.OutputRef{OutputIndex: 0, ItemID: "call_1"}, Delta: `{"city":`}}
	events <- apicore.StreamResult{Event: &responses.FunctionCallArgumentsDoneEvent{OutputRef: responses.OutputRef{OutputIndex: 0, ItemID: "call_1"}, Name: "lookup", Arguments: `{"city":"Berlin"}`}}
	events <- apicore.StreamResult{Event: &responses.ResponseCompletedEvent{Response: struct {
		ID                string `json:"id"`
		Model             string `json:"model"`
		Status            string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details,omitempty"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
		Usage *responses.ResponseUsage `json:"usage,omitempty"`
	}{ID: "resp_tool", Model: "gpt-5.4", Status: "completed"}}}
	close(events)

	handle := &apicore.StreamHandle{Events: events}
	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		unified.ForwardResponses(context.Background(), handle, pub, unified.StreamContext{Provider: "openai", Model: "gpt-5.4"})
	}()

	var stopReason llm.StopReason
	for ev := range ch {
		if ev.Type == llm.StreamEventCompleted {
			stopReason = ev.Data.(*llm.CompletedEvent).StopReason
		}
	}

	assert.Equal(t, llm.StopReasonToolUse, stopReason)
}

func TestForwardMessages_CostOverrideAndExtras(t *testing.T) {
	const overrideSSE = "" +
		"event: message_start\n" +
		`data: {"message":{"id":"msg_ovr","model":"claude-sonnet-4-6-20260101","usage":{"input_tokens":4}}}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}` + "\n\n" +
		"event: message_stop\n" +
		`data: {"type":"message_stop"}` + "\n\n"

	client := newMessagesClientSSE(overrideSSE)
	handle, err := client.Stream(context.Background(), &messages.Request{
		Model: "claude-sonnet-4-6", Stream: true,
		Messages: []messages.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	calcCalled := false
	calc := usage.CostCalculatorFunc(func(provider, model string, tokens usage.TokenItems) (usage.Cost, bool) {
		calcCalled = true
		assert.Equal(t, "anthropic", provider)
		assert.Equal(t, "claude-sonnet-4-6-20260101", model)
		assert.Equal(t, 4, tokens.Count(usage.KindInput))
		assert.Equal(t, 2, tokens.Count(usage.KindOutput))
		return usage.Cost{Total: 1.23, Source: "calculated"}, true
	})

	rl := &llm.RateLimits{}
	extra := map[string]any{"foo": "bar"}

	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		unified.ForwardMessages(context.Background(), handle, pub, unified.StreamContext{
			Provider:         "openrouter",
			Model:            "anthropic/claude-sonnet-4-6",
			UpstreamProvider: "anthropic",
			CostCalc:         calc,
			CostProvider:     "anthropic",
			UsageExtras:      extra,
			RateLimits:       rl,
		})
	}()

	var usageEvent *llm.UsageUpdatedEvent
	for ev := range ch {
		if ev.Type == llm.StreamEventUsageUpdated {
			usageEvent = ev.Data.(*llm.UsageUpdatedEvent)
		}
	}

	require.True(t, calcCalled)
	require.NotNil(t, usageEvent)
	assert.Equal(t, "openrouter", usageEvent.Record.Dims.Provider)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", usageEvent.Record.Dims.Model)
	assert.Equal(t, 1.23, usageEvent.Record.Cost.Total)
	assert.Equal(t, "calculated", usageEvent.Record.Cost.Source)
	if assert.NotNil(t, usageEvent.Record.Extras) {
		assert.Equal(t, "bar", usageEvent.Record.Extras["foo"])
		assert.Equal(t, rl, usageEvent.Record.Extras["rate_limits"])
	}
}
