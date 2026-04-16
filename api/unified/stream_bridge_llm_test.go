package unified

import (
	"testing"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishToLLM(t *testing.T) {
	now := time.Now()
	ev := StreamEvent{
		Type:      StreamEventDelta,
		Started:   &Started{RequestID: "req_1", Model: "gpt-4o", Provider: "openai", Extra: map[string]any{"x": 1}},
		Delta:     &Delta{Kind: llm.DeltaKindText, Text: "hello"},
		ToolCall:  &ToolCall{ID: "call_1", Name: "search", Args: map[string]any{"q": "go"}},
		Content:   &ContentPart{Part: msg.Text("full"), Index: 2},
		Usage:     &Usage{Provider: "openai", Model: "gpt-4o", RequestID: "req_1", Tokens: usage.TokenItems{{Kind: usage.KindInput, Count: 10}, {Kind: usage.KindOutput, Count: 4}}, RecordedAt: now},
		Completed: &Completed{StopReason: llm.StopReasonEndTurn},
	}

	pub, ch := llm.NewEventPublisher()
	err := PublishToLLM(pub, ev)
	require.NoError(t, err)
	pub.Close()

	var envelopes []llm.Envelope
	for e := range ch {
		envelopes = append(envelopes, e)
	}

	require.GreaterOrEqual(t, len(envelopes), 7)
	assert.Equal(t, llm.StreamEventCreated, envelopes[0].Type)
	assert.Contains(t, eventTypes(envelopes), llm.StreamEventStarted)
	assert.Contains(t, eventTypes(envelopes), llm.StreamEventDelta)
	assert.Contains(t, eventTypes(envelopes), llm.StreamEventToolCall)
	assert.Contains(t, eventTypes(envelopes), llm.StreamEventContentPart)
	assert.Contains(t, eventTypes(envelopes), llm.StreamEventUsageUpdated)
	assert.Contains(t, eventTypes(envelopes), llm.StreamEventCompleted)
}

func TestPublishToLLM_Error(t *testing.T) {
	pub, ch := llm.NewEventPublisher()
	err := PublishToLLM(pub, StreamEvent{Error: &StreamError{Err: assert.AnError}})
	require.NoError(t, err)
	pub.Close()

	var types []llm.EventType
	for e := range ch {
		types = append(types, e.Type)
	}
	assert.Contains(t, types, llm.StreamEventError)
}

func TestPublishToLLM_SemanticOnlyFallsBackToDebug(t *testing.T) {
	pub, ch := llm.NewEventPublisher()
	err := PublishToLLM(pub, StreamEvent{Type: StreamEventAnnotation, Annotation: &Annotation{Type: "file_citation", FileID: "file_1"}, Extras: EventExtras{RawEventName: responses.EventOutputTextAnnotationAdded}})
	require.NoError(t, err)
	pub.Close()

	var sawDebug bool
	for e := range ch {
		if e.Type == llm.StreamEventDebug {
			sawDebug = true
		}
	}
	assert.True(t, sawDebug)
}

func TestPublishToLLM_MixedEventAlsoFallsBackToDebug(t *testing.T) {
	pub, ch := llm.NewEventPublisher()
	err := PublishToLLM(pub, StreamEvent{Type: StreamEventCompleted, Completed: &Completed{StopReason: llm.StopReasonEndTurn}, Lifecycle: &Lifecycle{Scope: LifecycleScopeResponse, State: LifecycleStateDone}})
	require.NoError(t, err)
	pub.Close()

	var (
		sawCompleted bool
		sawDebug     bool
	)
	for e := range ch {
		switch e.Type {
		case llm.StreamEventCompleted:
			sawCompleted = true
		case llm.StreamEventDebug:
			sawDebug = true
		}
	}
	assert.True(t, sawCompleted)
	assert.True(t, sawDebug)
}

func eventTypes(es []llm.Envelope) []llm.EventType {
	out := make([]llm.EventType, 0, len(es))
	for _, e := range es {
		out = append(out, e.Type)
	}
	return out
}
