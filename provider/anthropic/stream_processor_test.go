package anthropic

import (
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collectText returns all text content from delta envelopes.
func collectText(envelopes []llm.Envelope) string {
	var b strings.Builder
	for _, env := range envelopes {
		if env.Type != llm.StreamEventDelta {
			continue
		}
		d, ok := env.Data.(*llm.DeltaEvent)
		if ok && d.Text != "" {
			b.WriteString(d.Text)
		}
	}
	return b.String()
}

// collectReasoning returns all reasoning content from delta envelopes.
func collectReasoning(envelopes []llm.Envelope) string {
	var b strings.Builder
	for _, env := range envelopes {
		if env.Type != llm.StreamEventDelta {
			continue
		}
		d, ok := env.Data.(*llm.DeltaEvent)
		if ok && d.Reasoning != "" {
			b.WriteString(d.Reasoning)
		}
	}
	return b.String()
}

// findUsage returns the Usage from the first UsageUpdated envelope, or nil.
func findUsage(envelopes []llm.Envelope) *llm.Usage {
	for _, env := range envelopes {
		if env.Type == llm.StreamEventUsageUpdated {
			ue := env.Data.(*llm.UsageUpdatedEvent)
			return &ue.Usage
		}
	}
	return nil
}

// findToolCall returns the first ToolCall envelope matching name, or nil.
func findToolCall(envelopes []llm.Envelope, name string) *llm.ToolCallEvent {
	for _, env := range envelopes {
		if env.Type == llm.StreamEventToolCall {
			tc := env.Data.(*llm.ToolCallEvent)
			if tc.ToolCall.ToolName() == name {
				return tc
			}
		}
	}
	return nil
}

// findError returns the first error envelope, or nil.
func findError(envelopes []llm.Envelope) error {
	for _, env := range envelopes {
		if env.Type == llm.StreamEventError {
			return env.Data.(*llm.ErrorEvent).Error
		}
	}
	return nil
}

func TestProcessor_TextDeltaFlow(t *testing.T) {
	h := newHarness(ParseOpts{ResolvedModel: "claude-sonnet-4-5"})

	envelopes := h.Send(
		MessageStartEvent{Message: MessageStartPayload{
			ID: "msg_01", Model: "claude-sonnet-4-5",
			Usage: MessageUsage{InputTokens: 10},
		}},
		ContentBlockStartEvent{Index: 0, ContentBlock: ContentBlock{Type: "text"}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "text_delta", Text: "Hello"}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "text_delta", Text: ", world"}},
		ContentBlockStopEvent{Index: 0},
		MessageDeltaEvent{
			Delta: MessageDelta{StopReason: "end_turn"},
			Usage: OutputUsage{OutputTokens: 5},
		},
		MessageStopEvent{},
	)

	assert.Equal(t, "Hello, world", collectText(envelopes))

	u := findUsage(envelopes)
	require.NotNil(t, u)
	assert.Equal(t, 10, u.InputTokens)
	assert.Equal(t, 5, u.OutputTokens)
}

func TestProcessor_ReasoningDeltaFlow(t *testing.T) {
	h := newHarness(ParseOpts{ResolvedModel: "claude-sonnet-4-5"})

	envelopes := h.Send(
		MessageStartEvent{Message: MessageStartPayload{
			ID: "msg_02", Model: "claude-sonnet-4-5",
			Usage: MessageUsage{InputTokens: 20},
		}},
		ContentBlockStartEvent{Index: 0, ContentBlock: ContentBlock{Type: "thinking"}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "thinking_delta", Thinking: "Let me think"}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "thinking_delta", Thinking: " carefully."}},
		ContentBlockStopEvent{Index: 0},
		ContentBlockStartEvent{Index: 1, ContentBlock: ContentBlock{Type: "text"}},
		ContentBlockDeltaEvent{Index: 1, Delta: ContentBlockDelta{Type: "text_delta", Text: "Answer."}},
		ContentBlockStopEvent{Index: 1},
		MessageDeltaEvent{
			Delta: MessageDelta{StopReason: "end_turn"},
			Usage: OutputUsage{OutputTokens: 8},
		},
		MessageStopEvent{},
	)

	assert.Equal(t, "Let me think carefully.", collectReasoning(envelopes))
	assert.Equal(t, "Answer.", collectText(envelopes))
}

func TestProcessor_ToolCallAccumulation(t *testing.T) {
	h := newHarness(ParseOpts{ResolvedModel: "claude-sonnet-4-5"})

	envelopes := h.Send(
		MessageStartEvent{Message: MessageStartPayload{ID: "msg_03", Model: "claude-sonnet-4-5"}},
		ContentBlockStartEvent{Index: 0, ContentBlock: ContentBlock{
			Type: "tool_use", ID: "call_abc", Name: "get_weather",
		}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{
			Type: "input_json_delta", PartialJSON: `{"loca`,
		}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{
			Type: "input_json_delta", PartialJSON: `tion":"Berlin"}`,
		}},
		ContentBlockStopEvent{Index: 0},
		MessageDeltaEvent{Delta: MessageDelta{StopReason: "tool_use"}},
		MessageStopEvent{},
	)

	tc := findToolCall(envelopes, "get_weather")
	require.NotNil(t, tc, "expected a get_weather tool call envelope")
	assert.Equal(t, "call_abc", tc.ToolCall.ToolCallID())
	assert.Equal(t, "Berlin", tc.ToolCall.ToolArgs()["location"])
}

func TestProcessor_CacheTokenAccounting(t *testing.T) {
	h := newHarness(ParseOpts{ResolvedModel: "claude-sonnet-4-5"})

	envelopes := h.Send(
		MessageStartEvent{Message: MessageStartPayload{
			ID: "msg_04", Model: "claude-sonnet-4-5",
			Usage: MessageUsage{
				InputTokens:              10,
				CacheCreationInputTokens: 512,
				CacheReadInputTokens:     1024,
			},
		}},
		ContentBlockStartEvent{Index: 0, ContentBlock: ContentBlock{Type: "text"}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "text_delta", Text: "Hi"}},
		ContentBlockStopEvent{Index: 0},
		MessageDeltaEvent{
			Delta: MessageDelta{StopReason: "end_turn"},
			Usage: OutputUsage{OutputTokens: 5},
		},
		MessageStopEvent{},
	)

	u := findUsage(envelopes)
	require.NotNil(t, u)
	// InputTokens should be base + cache write + cache read
	assert.Equal(t, 10+512+1024, u.InputTokens)
	assert.Equal(t, 512, u.CacheWriteTokens)
	assert.Equal(t, 1024, u.CacheReadTokens)
	assert.Equal(t, 5, u.OutputTokens)
}

func TestProcessor_ErrorEventTerminatesStream(t *testing.T) {
	h := newHarness(ParseOpts{ResolvedModel: "claude-sonnet-4-5"})

	envelopes := h.Send(
		MessageStartEvent{Message: MessageStartPayload{ID: "msg_05", Model: "claude-sonnet-4-5"}},
		StreamErrorEvent{Error: StreamErrorPayload{Message: "overloaded_error"}},
	)

	err := findError(envelopes)
	require.NotNil(t, err, "expected an error envelope")
	assert.Contains(t, err.Error(), "overloaded_error")
}

func TestProcessor_StopReasonMapping(t *testing.T) {
	cases := []struct {
		raw  string
		want llm.StopReason
	}{
		{"end_turn", llm.StopReasonEndTurn},
		{"tool_use", llm.StopReasonToolUse},
		{"max_tokens", llm.StopReasonMaxTokens},
		{"custom_reason", llm.StopReason("custom_reason")},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			assert.Equal(t, tc.want, mapAnthropicStopReason(tc.raw))
		})
	}
}
