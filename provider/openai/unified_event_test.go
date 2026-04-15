package openai

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/completions"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/api/unified"
	"github.com/codewandler/llm/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnifiedCompletionsEventPipeline_Parity(t *testing.T) {
	sseBody := "data: {\"id\":\"chatcmpl_1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3,\"total_tokens\":13}}\n\n" +
		"data: [DONE]\n\n"

	client := completions.NewClient(
		completions.WithBaseURL("https://fake.api"),
		completions.WithHTTPClient(&http.Client{Transport: httpRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(sseBody)),
			}, nil
		})}),
	)

	handle, err := client.Stream(t.Context(), &completions.Request{Model: "gpt-4o", Stream: true})
	require.NoError(t, err)

	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		for r := range handle.Events {
			if r.Err != nil {
				pub.Error(r.Err)
				return
			}
			uEv, ignored, convErr := unified.EventFromCompletions(r.Event)
			if convErr != nil {
				pub.Error(convErr)
				return
			}
			if ignored {
				continue
			}
			require.NoError(t, unified.Publish(pub, uEv))
		}
	}()

	var (
		sawStarted bool
		sawDelta   bool
		sawUsage   bool
		sawDone    bool
	)
	for ev := range ch {
		switch ev.Type {
		case llm.StreamEventStarted:
			sawStarted = true
		case llm.StreamEventDelta:
			sawDelta = true
		case llm.StreamEventUsageUpdated:
			sawUsage = true
			ue := ev.Data.(*llm.UsageUpdatedEvent)
			assert.Equal(t, 10, ue.Record.Tokens.Count(usage.KindInput))
			assert.Equal(t, 3, ue.Record.Tokens.Count(usage.KindOutput))
		case llm.StreamEventCompleted:
			sawDone = true
			ce := ev.Data.(*llm.CompletedEvent)
			assert.Equal(t, llm.StopReasonEndTurn, ce.StopReason)
		}
	}

	assert.True(t, sawStarted)
	assert.True(t, sawDelta)
	assert.True(t, sawUsage)
	assert.True(t, sawDone)
}

type httpRoundTripFunc func(*http.Request) (*http.Response, error)

func (f httpRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUnifiedResponsesEventPipeline_Parity(t *testing.T) {
	sseBody := "event: response.created\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-5.4\"}}\n\n" +
		"event: response.output_text.delta\ndata: {\"output_index\":0,\"delta\":\"pong\"}\n\n" +
		"event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-5.4\",\"status\":\"completed\",\"usage\":{\"input_tokens\":11,\"output_tokens\":4}}}\n\n"

	body := io.NopCloser(strings.NewReader(sseBody))
	pub, ch := llm.NewEventPublisher()

	go func() {
		defer pub.Close()

		h := responsesParserFromSSE(body)
		for _, event := range h {
			uEv, ignored, err := unified.EventFromResponses(event)
			if err != nil {
				pub.Error(err)
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

	var sawDone bool
	for ev := range ch {
		if ev.Type == llm.StreamEventCompleted {
			sawDone = true
			ce := ev.Data.(*llm.CompletedEvent)
			assert.Equal(t, llm.StopReasonEndTurn, ce.StopReason)
		}
	}
	assert.True(t, sawDone)
}

func responsesParserFromSSE(body io.ReadCloser) []any {
	defer body.Close()
	return []any{
		&responses.ResponseCreatedEvent{},
		&responses.TextDeltaEvent{OutputIndex: 0, Delta: "pong"},
		&responses.ResponseCompletedEvent{Response: struct {
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
		}{
			Status: responses.StatusCompleted,
			Usage:  &responses.ResponseUsage{InputTokens: 11, OutputTokens: 4},
		}},
	}
}
