package unified

import (
	"testing"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/completions"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventFromMessages_StartAndNoOps(t *testing.T) {
	ev, ignored, err := EventFromMessages(&messages.MessageStartEvent{
		Message: messages.MessageStartPayload{
			ID:    "msg_1",
			Model: "claude-sonnet-4-6",
			Usage: messages.MessageUsage{InputTokens: 42, CacheReadInputTokens: 8, CacheCreationInputTokens: 4},
		},
	})
	require.NoError(t, err)
	require.False(t, ignored)
	// started
	require.NotNil(t, ev.Started)
	assert.Equal(t, "msg_1", ev.Started.RequestID)
	assert.Equal(t, "claude-sonnet-4-6", ev.Started.Model)
	// input-phase usage is emitted at message_start
	require.NotNil(t, ev.Usage)
	assert.Equal(t, 42, ev.Usage.Tokens.Count(usage.KindInput))
	assert.Equal(t, 8, ev.Usage.Tokens.Count(usage.KindCacheRead))
	assert.Equal(t, 4, ev.Usage.Tokens.Count(usage.KindCacheWrite))

	_, ignored, err = EventFromMessages(&messages.PingEvent{})
	require.NoError(t, err)
	assert.True(t, ignored)

	_, ignored, err = EventFromMessages(&messages.MessageStopEvent{})
	require.NoError(t, err)
	assert.True(t, ignored)
}

func TestEventFromMessages_DeltasContentUsageToolAndError(t *testing.T) {
	ev, ignored, err := EventFromMessages(&messages.ContentBlockDeltaEvent{
		Index: 3,
		Delta: messages.Delta{Type: messages.DeltaTypeText, Text: "hello"},
	})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	assert.Equal(t, llm.DeltaKindText, ev.Delta.Kind)
	require.NotNil(t, ev.Delta.Index)
	assert.Equal(t, uint32(3), *ev.Delta.Index)
	assert.Equal(t, "hello", ev.Delta.Text)

	ev, ignored, err = EventFromMessages(&messages.ContentBlockDeltaEvent{
		Index: 1,
		Delta: messages.Delta{Type: messages.DeltaTypeThinking, Thinking: "hmm"},
	})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	assert.Equal(t, llm.DeltaKindThinking, ev.Delta.Kind)
	assert.Equal(t, "hmm", ev.Delta.Thinking)

	ev, ignored, err = EventFromMessages(&messages.ContentBlockDeltaEvent{
		Index: 2,
		Delta: messages.Delta{Type: messages.DeltaTypeInputJSON, PartialJSON: "{\"q\":"},
	})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	assert.Equal(t, llm.DeltaKindTool, ev.Delta.Kind)
	assert.Equal(t, "{\"q\":", ev.Delta.ToolArgs)

	ev, ignored, err = EventFromMessages(&messages.TextCompleteEvent{Index: 0, Text: "full text"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Content)
	assert.Equal(t, msg.PartTypeText, ev.Content.Part.Type)
	assert.Equal(t, "full text", ev.Content.Part.Text)
	assert.Equal(t, 0, ev.Content.Index)

	ev, ignored, err = EventFromMessages(&messages.ThinkingCompleteEvent{Index: 4, Thinking: "thought", Signature: "sig"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Content)
	assert.Equal(t, msg.PartTypeThinking, ev.Content.Part.Type)
	require.NotNil(t, ev.Content.Part.Thinking)
	assert.Equal(t, "thought", ev.Content.Part.Thinking.Text)
	assert.Equal(t, "sig", ev.Content.Part.Thinking.Signature)

	ev, ignored, err = EventFromMessages(&messages.ToolCompleteEvent{ID: "call_1", Name: "search", Args: map[string]any{"q": "golang"}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ToolCall)
	assert.Equal(t, "call_1", ev.ToolCall.ID)
	assert.Equal(t, "search", ev.ToolCall.Name)
	assert.Equal(t, map[string]any{"q": "golang"}, ev.ToolCall.Args)

	ev, ignored, err = EventFromMessages(&messages.MessageDeltaEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	// message_delta carries output tokens and stop reason
	require.NotNil(t, ev.Completed)
	assert.Equal(t, llm.StopReason(""), ev.Completed.StopReason) // empty stop_reason maps to empty
	require.NotNil(t, ev.Usage)

	ev, ignored, err = EventFromMessages(&messages.MessageDeltaEvent{
		Delta: struct {
			StopReason string `json:"stop_reason"`
		}{StopReason: "end_turn"},
		Usage: struct {
			OutputTokens int `json:"output_tokens"`
		}{OutputTokens: 15},
	})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed)
	assert.Equal(t, llm.StopReasonEndTurn, ev.Completed.StopReason)
	require.NotNil(t, ev.Usage)
	assert.Equal(t, 15, ev.Usage.Tokens.Count(usage.KindOutput))

	ev, ignored, err = EventFromMessages(&messages.MessageDeltaEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed) // always present from message_delta
	require.NotNil(t, ev.Usage)

	ev, ignored, err = EventFromMessages(&messages.StreamErrorEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Error)
}

func TestEventFromCompletions(t *testing.T) {
	ev, ignored, err := EventFromCompletions(&completions.Chunk{
		ID:    "chatcmpl_1",
		Model: "gpt-4o",
		Choices: []completions.Choice{{
			Delta: completions.Delta{Content: "hello"},
		}},
	})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Started)
	require.NotNil(t, ev.Delta)
	assert.Equal(t, llm.DeltaKindText, ev.Delta.Kind)
	assert.Equal(t, "hello", ev.Delta.Text)

	ev, ignored, err = EventFromCompletions(&completions.Chunk{
		Choices: []completions.Choice{
			{
				Delta: completions.Delta{
					ToolCalls: []completions.ToolCallDelta{
						{
							Index: 2,
							ID:    "call_1",
							Function: completions.FuncCallDelta{
								Name:      "search",
								Arguments: "{\"q\":",
							},
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	assert.Equal(t, llm.DeltaKindTool, ev.Delta.Kind)
	require.NotNil(t, ev.Delta.Index)
	assert.Equal(t, uint32(2), *ev.Delta.Index)
	assert.Equal(t, "call_1", ev.Delta.ToolID)
	assert.Equal(t, "search", ev.Delta.ToolName)

	finish := completions.FinishReasonToolCalls
	ev, ignored, err = EventFromCompletions(&completions.Chunk{
		Choices: []completions.Choice{{FinishReason: &finish}},
		Usage:   &completions.Usage{PromptTokens: 12, CompletionTokens: 5},
	})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed)
	assert.Equal(t, llm.StopReasonToolUse, ev.Completed.StopReason)
	require.NotNil(t, ev.Usage)
	assert.Equal(t, 12, ev.Usage.Tokens.Count(usage.KindInput))
	assert.Equal(t, 5, ev.Usage.Tokens.Count(usage.KindOutput))
}

func TestEventFromResponses(t *testing.T) {
	ev, ignored, err := EventFromResponses(&responses.ResponseCreatedEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Started)

	ev, ignored, err = EventFromResponses(&responses.TextDeltaEvent{OutputIndex: 4, Delta: "pong"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	assert.Equal(t, llm.DeltaKindText, ev.Delta.Kind)
	require.NotNil(t, ev.Delta.Index)
	assert.Equal(t, uint32(4), *ev.Delta.Index)

	ev, ignored, err = EventFromResponses(&responses.ReasoningDeltaEvent{OutputIndex: 1, Delta: "reason"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	assert.Equal(t, llm.DeltaKindThinking, ev.Delta.Kind)

	ev, ignored, err = EventFromResponses(&responses.FuncArgsDeltaEvent{OutputIndex: 3, Delta: "{}"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	assert.Equal(t, llm.DeltaKindTool, ev.Delta.Kind)

	ev, ignored, err = EventFromResponses(&responses.ToolCompleteEvent{ID: "call_1", Name: "lookup", Args: map[string]any{"a": 1}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ToolCall)
	assert.Equal(t, "call_1", ev.ToolCall.ID)

	ev, ignored, err = EventFromResponses(&responses.ResponseCompletedEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed)

	ev, ignored, err = EventFromResponses(&responses.APIErrorEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Error)

	_, ignored, err = EventFromResponses(&responses.OutputItemAddedEvent{})
	require.NoError(t, err)
	assert.True(t, ignored)
}

func TestPublish(t *testing.T) {
	now := time.Now()
	ev := StreamEvent{
		Type: StreamEventDelta,
		Started: &Started{
			RequestID: "req_1",
			Model:     "gpt-4o",
			Provider:  "openai",
			Extra:     map[string]any{"x": 1},
		},
		Delta: &Delta{
			Kind: llm.DeltaKindText,
			Text: "hello",
		},
		ToolCall: &ToolCall{ID: "call_1", Name: "search", Args: map[string]any{"q": "go"}},
		Content:  &ContentPart{Part: msg.Text("full"), Index: 2},
		Usage: &Usage{
			Provider:   "openai",
			Model:      "gpt-4o",
			RequestID:  "req_1",
			Tokens:     usage.TokenItems{{Kind: usage.KindInput, Count: 10}, {Kind: usage.KindOutput, Count: 4}},
			RecordedAt: now,
		},
		Completed: &Completed{StopReason: llm.StopReasonEndTurn},
	}

	pub, ch := llm.NewEventPublisher()
	err := Publish(pub, ev)
	require.NoError(t, err)
	pub.Close()

	var envelopes []llm.Envelope
	for e := range ch {
		envelopes = append(envelopes, e)
	}

	// created + started + delta + tool_call + content_part + usage + completed
	require.GreaterOrEqual(t, len(envelopes), 7)
	assert.Equal(t, llm.StreamEventCreated, envelopes[0].Type)
	assert.Contains(t, eventTypes(envelopes), llm.StreamEventStarted)
	assert.Contains(t, eventTypes(envelopes), llm.StreamEventDelta)
	assert.Contains(t, eventTypes(envelopes), llm.StreamEventToolCall)
	assert.Contains(t, eventTypes(envelopes), llm.StreamEventContentPart)
	assert.Contains(t, eventTypes(envelopes), llm.StreamEventUsageUpdated)
	assert.Contains(t, eventTypes(envelopes), llm.StreamEventCompleted)
}

func TestPublish_Error(t *testing.T) {
	pub, ch := llm.NewEventPublisher()
	err := Publish(pub, StreamEvent{Error: &StreamError{Err: assert.AnError}})
	require.NoError(t, err)
	pub.Close()

	var types []llm.EventType
	for e := range ch {
		types = append(types, e.Type)
	}
	assert.Contains(t, types, llm.StreamEventError)
}

func eventTypes(es []llm.Envelope) []llm.EventType {
	out := make([]llm.EventType, 0, len(es))
	for _, e := range es {
		out = append(out, e.Type)
	}
	return out
}
