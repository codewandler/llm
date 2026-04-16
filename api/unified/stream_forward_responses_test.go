package unified_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/api/unified"
	"github.com/codewandler/llm/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(responsesSSE))}, nil
		})}),
	)

	handle, err := client.Stream(context.Background(), &responses.Request{Model: "gpt-5.4", Stream: true, Input: []responses.Input{{Role: "user", Content: "hi"}}})
	require.NoError(t, err)

	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		unified.ForwardResponses(context.Background(), handle, pub, unified.StreamContext{Provider: "openai", Model: "gpt-5.4", CostCalc: usage.Default()})
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

func TestForwardResponses_RespectsIncompleteAndFailedReasons(t *testing.T) {
	t.Run("incomplete maps to max tokens", func(t *testing.T) {
		events := make(chan apicore.StreamResult, 1)
		events <- apicore.StreamResult{Event: &responses.ResponseIncompleteEvent{Response: responses.ResponsePayload{ID: "resp_inc", IncompleteDetails: &responses.IncompleteDetails{Reason: responses.ReasonMaxOutputTokens}}}}
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

		assert.Equal(t, llm.StopReasonMaxTokens, stopReason)
	})

	t.Run("failed maps to error", func(t *testing.T) {
		events := make(chan apicore.StreamResult, 1)
		events <- apicore.StreamResult{Event: &responses.ResponseFailedEvent{Response: responses.ResponsePayload{ID: "resp_fail", Error: &responses.ResponseError{Code: "server_error", Message: "boom"}}}}
		close(events)

		handle := &apicore.StreamHandle{Events: events}
		pub, ch := llm.NewEventPublisher()
		go func() {
			defer pub.Close()
			unified.ForwardResponses(context.Background(), handle, pub, unified.StreamContext{Provider: "openai", Model: "gpt-5.4"})
		}()

		var (
			stopReason llm.StopReason
			sawError   bool
		)
		for ev := range ch {
			switch ev.Type {
			case llm.StreamEventCompleted:
				stopReason = ev.Data.(*llm.CompletedEvent).StopReason
			case llm.StreamEventError:
				sawError = true
			}
		}

		assert.Equal(t, llm.StopReasonError, stopReason)
		assert.True(t, sawError)
	})
}
