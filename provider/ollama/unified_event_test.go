package ollama

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/api/unified"
	"github.com/stretchr/testify/assert"
)

func TestUnifiedResponsesEventPipeline_OllamaCompatible(t *testing.T) {
	pub, ch := llm.NewEventPublisher()

	go func() {
		defer pub.Close()

		events := []any{
			&responses.ResponseCreatedEvent{Response: responses.ResponsePayload{ID: "resp_1", Model: "llama3.2"}},
			&responses.OutputTextDeltaEvent{ContentRef: responses.ContentRef{OutputIndex: 0}, Delta: "pong"},
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
			}{Status: responses.StatusCompleted, Usage: &responses.ResponseUsage{InputTokens: 11, OutputTokens: 4}}},
		}

		for _, event := range events {
			uEv, ignored, err := unified.MapResponsesEvent(event)
			if err != nil {
				pub.Error(err)
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
