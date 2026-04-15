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

func TestMapMessagesEvent_StartAndNoOps(t *testing.T) {
	ev, ignored, err := MapMessagesEvent(&messages.MessageStartEvent{
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
	assert.Equal(t, messages.EventMessageStart, ev.Extras.RawEventName)

	ev, ignored, err = MapMessagesEvent(&messages.ContentBlockStartEvent{
		Index:        2,
		ContentBlock: []byte(`{"type":"text"}`),
	})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Lifecycle)
	assert.Equal(t, LifecycleScopeSegment, ev.Lifecycle.Scope)
	assert.Equal(t, LifecycleStateAdded, ev.Lifecycle.State)
	assert.Equal(t, ContentKindText, ev.Lifecycle.Kind)
	assert.Equal(t, messages.EventContentBlockStart, ev.Extras.RawEventName)

	_, ignored, err = MapMessagesEvent(&messages.PingEvent{})
	require.NoError(t, err)
	assert.True(t, ignored)

	_, ignored, err = MapMessagesEvent(&messages.MessageStopEvent{})
	require.NoError(t, err)
	assert.True(t, ignored)
}

func TestMapMessagesEvent_DeltasContentUsageToolAndError(t *testing.T) {
	ev, ignored, err := MapMessagesEvent(&messages.ContentBlockDeltaEvent{
		Index: 3,
		Delta: messages.Delta{Type: messages.DeltaTypeText, Text: "hello"},
	})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindText, ev.ContentDelta.Kind)
	assert.Equal(t, llm.DeltaKindText, ev.Delta.Kind)
	require.NotNil(t, ev.Delta.Index)
	assert.Equal(t, uint32(3), *ev.Delta.Index)
	assert.Equal(t, "hello", ev.Delta.Text)

	ev, ignored, err = MapMessagesEvent(&messages.ContentBlockDeltaEvent{
		Index: 1,
		Delta: messages.Delta{Type: messages.DeltaTypeThinking, Thinking: "hmm"},
	})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindReasoning, ev.ContentDelta.Kind)
	assert.Equal(t, llm.DeltaKindThinking, ev.Delta.Kind)
	assert.Equal(t, "hmm", ev.Delta.Thinking)

	ev, ignored, err = MapMessagesEvent(&messages.ContentBlockDeltaEvent{
		Index: 2,
		Delta: messages.Delta{Type: messages.DeltaTypeInputJSON, PartialJSON: "{\"q\":"},
	})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ToolDelta)
	assert.Equal(t, ToolDeltaKindFunctionArguments, ev.ToolDelta.Kind)
	assert.Equal(t, llm.DeltaKindTool, ev.Delta.Kind)
	assert.Equal(t, "{\"q\":", ev.Delta.ToolArgs)

	ev, ignored, err = MapMessagesEvent(&messages.ContentBlockDeltaEvent{
		Index: 2,
		Delta: messages.Delta{Type: messages.DeltaTypeSignature, Signature: "sig-part"},
	})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindReasoning, ev.ContentDelta.Kind)
	assert.Equal(t, "sig-part", ev.ContentDelta.Signature)

	ev, ignored, err = MapMessagesEvent(&messages.TextCompleteEvent{Index: 0, Text: "full text"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Content)
	require.NotNil(t, ev.StreamContent)
	require.NotNil(t, ev.Lifecycle)
	assert.Equal(t, LifecycleStateDone, ev.Lifecycle.State)
	assert.Equal(t, ContentKindText, ev.StreamContent.Kind)
	assert.Equal(t, msg.PartTypeText, ev.Content.Part.Type)
	assert.Equal(t, "full text", ev.Content.Part.Text)
	assert.Equal(t, 0, ev.Content.Index)

	ev, ignored, err = MapMessagesEvent(&messages.ThinkingCompleteEvent{Index: 4, Thinking: "thought", Signature: "sig"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Content)
	require.NotNil(t, ev.StreamContent)
	assert.Equal(t, ContentKindReasoning, ev.StreamContent.Kind)
	assert.Equal(t, "sig", ev.StreamContent.Signature)
	assert.Equal(t, msg.PartTypeThinking, ev.Content.Part.Type)
	require.NotNil(t, ev.Content.Part.Thinking)
	assert.Equal(t, "thought", ev.Content.Part.Thinking.Text)
	assert.Equal(t, "sig", ev.Content.Part.Thinking.Signature)

	ev, ignored, err = MapMessagesEvent(&messages.ToolCompleteEvent{ID: "call_1", Name: "search", RawInput: `{"q":"golang"}`, Args: map[string]any{"q": "golang"}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ToolCall)
	require.NotNil(t, ev.StreamToolCall)
	assert.Equal(t, `{"q":"golang"}`, ev.StreamToolCall.RawInput)
	assert.Equal(t, "call_1", ev.ToolCall.ID)
	assert.Equal(t, "search", ev.ToolCall.Name)
	assert.Equal(t, map[string]any{"q": "golang"}, ev.ToolCall.Args)

	ev, ignored, err = MapMessagesEvent(&messages.MessageDeltaEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	// message_delta carries output tokens and stop reason
	require.NotNil(t, ev.Completed)
	assert.Equal(t, llm.StopReason(""), ev.Completed.StopReason) // empty stop_reason maps to empty
	require.NotNil(t, ev.Usage)

	ev, ignored, err = MapMessagesEvent(&messages.MessageDeltaEvent{
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

	ev, ignored, err = MapMessagesEvent(&messages.MessageDeltaEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed) // always present from message_delta
	require.NotNil(t, ev.Usage)

	ev, ignored, err = MapMessagesEvent(&messages.StreamErrorEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Error)
	assert.Equal(t, messages.EventError, ev.Extras.RawEventName)
}

func TestMapCompletionsEvent(t *testing.T) {
	ev, ignored, err := MapCompletionsEvent(&completions.Chunk{
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
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindText, ev.ContentDelta.Kind)
	assert.Equal(t, llm.DeltaKindText, ev.Delta.Kind)
	assert.Equal(t, "hello", ev.Delta.Text)

	ev, ignored, err = MapCompletionsEvent(&completions.Chunk{
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
	require.NotNil(t, ev.ToolDelta)
	assert.Equal(t, ToolDeltaKindFunctionArguments, ev.ToolDelta.Kind)
	assert.Equal(t, llm.DeltaKindTool, ev.Delta.Kind)
	require.NotNil(t, ev.Delta.Index)
	assert.Equal(t, uint32(2), *ev.Delta.Index)
	assert.Equal(t, "call_1", ev.Delta.ToolID)
	assert.Equal(t, "search", ev.Delta.ToolName)

	finish := completions.FinishReasonToolCalls
	ev, ignored, err = MapCompletionsEvent(&completions.Chunk{
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

func TestMapResponsesEvent(t *testing.T) {
	ev, ignored, err := MapResponsesEvent(&responses.ResponseCreatedEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Started)
	assert.Equal(t, responses.EventResponseCreated, ev.Extras.RawEventName)

	ev, ignored, err = MapResponsesEvent(&responses.ResponseQueuedEvent{Response: responses.ResponsePayload{ID: "resp_1"}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Lifecycle)
	assert.Equal(t, LifecycleScopeResponse, ev.Lifecycle.Scope)
	assert.Equal(t, LifecycleStateQueued, ev.Lifecycle.State)

	ev, ignored, err = MapResponsesEvent(&responses.OutputTextDeltaEvent{ContentRef: responses.ContentRef{OutputIndex: 4}, Delta: "pong"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindText, ev.ContentDelta.Kind)
	assert.Equal(t, ContentVariantPrimary, ev.ContentDelta.Variant)
	assert.Equal(t, llm.DeltaKindText, ev.Delta.Kind)
	require.NotNil(t, ev.Delta.Index)
	assert.Equal(t, uint32(4), *ev.Delta.Index)

	ev, ignored, err = MapResponsesEvent(&responses.ReasoningTextDeltaEvent{OutputRef: responses.OutputRef{OutputIndex: 1}, Delta: "reason"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentVariantRaw, ev.ContentDelta.Variant)
	assert.Equal(t, llm.DeltaKindThinking, ev.Delta.Kind)

	ev, ignored, err = MapResponsesEvent(&responses.ReasoningSummaryTextDeltaEvent{OutputRef: responses.OutputRef{OutputIndex: 2}, Delta: "summary"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindReasoning, ev.ContentDelta.Kind)
	assert.Equal(t, ContentVariantSummary, ev.ContentDelta.Variant)

	ev, ignored, err = MapResponsesEvent(&responses.RefusalDoneEvent{ContentRef: responses.ContentRef{OutputIndex: 0, ContentIndex: 1}, Refusal: "no"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.StreamContent)
	assert.Equal(t, ContentKindRefusal, ev.StreamContent.Kind)

	ev, ignored, err = MapResponsesEvent(&responses.OutputTextAnnotationAddedEvent{ContentRef: responses.ContentRef{OutputIndex: 0, ContentIndex: 1}, AnnotationIndex: 3, Annotation: responses.OutputTextAnnotation{Type: "file_citation", FileID: "file_1"}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Annotation)
	assert.Equal(t, "file_citation", ev.Annotation.Type)
	require.NotNil(t, ev.Annotation.Ref.AnnotationIndex)
	assert.Equal(t, uint32(3), *ev.Annotation.Ref.AnnotationIndex)

	ev, ignored, err = MapResponsesEvent(&responses.FunctionCallArgumentsDeltaEvent{OutputRef: responses.OutputRef{OutputIndex: 3}, Delta: "{}"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ToolDelta)
	assert.Equal(t, llm.DeltaKindTool, ev.Delta.Kind)

	ev, ignored, err = MapResponsesEvent(&responses.FunctionCallArgumentsDoneEvent{OutputRef: responses.OutputRef{ItemID: "call_1"}, Name: "lookup", Arguments: `{"a":1}`})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ToolCall)
	require.NotNil(t, ev.StreamToolCall)
	assert.Equal(t, "call_1", ev.ToolCall.ID)
	assert.Equal(t, `{"a":1}`, ev.StreamToolCall.RawInput)

	ev, ignored, err = MapResponsesEvent(&responses.CustomToolCallInputDoneEvent{OutputRef: responses.OutputRef{OutputIndex: 7, ItemID: "cust_1"}, Input: "raw-input"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ToolDelta)
	assert.True(t, ev.ToolDelta.Final)
	assert.Equal(t, ToolDeltaKindCustomInput, ev.ToolDelta.Kind)

	ev, ignored, err = MapResponsesEvent(&responses.AudioTranscriptDeltaEvent{ResponseRef: responses.ResponseRef{ResponseID: "resp_1"}, Delta: "hello"})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindMedia, ev.ContentDelta.Kind)
	assert.Equal(t, ContentVariantTranscript, ev.ContentDelta.Variant)

	ev, ignored, err = MapResponsesEvent(&responses.ResponseCompletedEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed)
	require.NotNil(t, ev.Lifecycle)
	assert.Equal(t, LifecycleStateDone, ev.Lifecycle.State)

	ev, ignored, err = MapResponsesEvent(&responses.ResponseFailedEvent{Response: responses.ResponsePayload{ID: "resp_1", Error: &responses.ResponseError{Code: "server_error", Message: "boom"}}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed)
	require.NotNil(t, ev.Error)
	require.NotNil(t, ev.Lifecycle)
	assert.Equal(t, LifecycleStateFailed, ev.Lifecycle.State)

	ev, ignored, err = MapResponsesEvent(&responses.APIErrorEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Error)

	ev, ignored, err = MapResponsesEvent(&responses.OutputItemAddedEvent{OutputIndex: 1, Item: responses.ResponseOutputItem{ID: "it_1", Type: "message"}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Lifecycle)
	assert.Equal(t, LifecycleScopeItem, ev.Lifecycle.Scope)
	assert.Equal(t, LifecycleStateAdded, ev.Lifecycle.State)

	ev, ignored, err = MapResponsesEvent(&responses.WebSearchCallInProgressEvent{OutputRef: responses.OutputRef{OutputIndex: 9, ItemID: "ws_1"}})
	require.NoError(t, err)
	require.False(t, ignored)
	assert.Equal(t, StreamEventUnknown, ev.Type)
	assert.Equal(t, responses.EventWebSearchCallInProgress, ev.Extras.RawEventName)
	assert.NotEmpty(t, ev.Extras.RawJSON)
}

func TestPublishToLLM(t *testing.T) {
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
	err := PublishToLLM(pub, ev)
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
	err := PublishToLLM(pub, StreamEvent{
		Type:       StreamEventAnnotation,
		Annotation: &Annotation{Type: "file_citation", FileID: "file_1"},
		Extras:     EventExtras{RawEventName: responses.EventOutputTextAnnotationAdded},
	})
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

func eventTypes(es []llm.Envelope) []llm.EventType {
	out := make([]llm.EventType, 0, len(es))
	for _, e := range es {
		out = append(out, e.Type)
	}
	return out
}
