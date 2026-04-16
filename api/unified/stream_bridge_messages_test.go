package unified

import (
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/messages"
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
	require.NotNil(t, ev.Started)
	assert.Equal(t, "msg_1", ev.Started.RequestID)
	assert.Equal(t, "claude-sonnet-4-6", ev.Started.Model)
	require.NotNil(t, ev.Usage)
	assert.Equal(t, 42, ev.Usage.Tokens.Count(usage.KindInput))
	assert.Equal(t, 8, ev.Usage.Tokens.Count(usage.KindCacheRead))
	assert.Equal(t, 4, ev.Usage.Tokens.Count(usage.KindCacheWrite))
	assert.Equal(t, messages.EventMessageStart, ev.Extras.RawEventName)

	ev, ignored, err = MapMessagesEvent(&messages.ContentBlockStartEvent{Index: 2, ContentBlock: []byte(`{"type":"text"}`)})
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

func TestMapMessagesEvent_PreservesSourceRawPayload(t *testing.T) {
	result := apicore.StreamResult{
		Event:        &messages.ContentBlockDeltaEvent{Index: 3, Delta: messages.Delta{Type: messages.DeltaTypeText, Text: "hello"}},
		RawEventName: messages.EventContentBlockDelta,
		RawJSON:      []byte(`{"index":3,"delta":{"type":"text_delta","text":"hello"}}`),
	}

	ev, ignored, err := MapMessagesEvent(result)
	require.NoError(t, err)
	require.False(t, ignored)
	assert.Equal(t, messages.EventContentBlockDelta, ev.Extras.RawEventName)
	assert.JSONEq(t, string(result.RawJSON), string(ev.Extras.RawJSON))
}

func TestMapMessagesEvent_DeltasContentUsageToolAndError(t *testing.T) {
	ev, ignored, err := MapMessagesEvent(&messages.ContentBlockDeltaEvent{Index: 3, Delta: messages.Delta{Type: messages.DeltaTypeText, Text: "hello"}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindText, ev.ContentDelta.Kind)
	assert.Equal(t, llm.DeltaKindText, ev.Delta.Kind)
	require.NotNil(t, ev.Delta.Index)
	assert.Equal(t, uint32(3), *ev.Delta.Index)
	assert.Equal(t, "hello", ev.Delta.Text)

	ev, ignored, err = MapMessagesEvent(&messages.ContentBlockDeltaEvent{Index: 1, Delta: messages.Delta{Type: messages.DeltaTypeThinking, Thinking: "hmm"}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ContentDelta)
	assert.Equal(t, ContentKindReasoning, ev.ContentDelta.Kind)
	assert.Equal(t, llm.DeltaKindThinking, ev.Delta.Kind)
	assert.Equal(t, "hmm", ev.Delta.Thinking)

	ev, ignored, err = MapMessagesEvent(&messages.ContentBlockDeltaEvent{Index: 2, Delta: messages.Delta{Type: messages.DeltaTypeInputJSON, PartialJSON: `{"q":`}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Delta)
	require.NotNil(t, ev.ToolDelta)
	assert.Equal(t, ToolDeltaKindFunctionArguments, ev.ToolDelta.Kind)
	assert.Equal(t, llm.DeltaKindTool, ev.Delta.Kind)
	assert.Equal(t, `{"q":`, ev.Delta.ToolArgs)

	ev, ignored, err = MapMessagesEvent(&messages.ContentBlockDeltaEvent{Index: 2, Delta: messages.Delta{Type: messages.DeltaTypeSignature, Signature: "sig-part"}})
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
	require.NotNil(t, ev.Completed)
	assert.Equal(t, llm.StopReason(""), ev.Completed.StopReason)
	require.NotNil(t, ev.Usage)

	ev, ignored, err = MapMessagesEvent(&messages.MessageDeltaEvent{Delta: struct {
		StopReason string `json:"stop_reason"`
	}{StopReason: "end_turn"}, Usage: struct {
		OutputTokens int `json:"output_tokens"`
	}{OutputTokens: 15}})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed)
	assert.Equal(t, llm.StopReasonEndTurn, ev.Completed.StopReason)
	require.NotNil(t, ev.Usage)
	assert.Equal(t, 15, ev.Usage.Tokens.Count(usage.KindOutput))

	ev, ignored, err = MapMessagesEvent(&messages.MessageDeltaEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Completed)
	require.NotNil(t, ev.Usage)

	ev, ignored, err = MapMessagesEvent(&messages.StreamErrorEvent{})
	require.NoError(t, err)
	require.False(t, ignored)
	require.NotNil(t, ev.Error)
	assert.Equal(t, messages.EventError, ev.Extras.RawEventName)
}
