package anthropic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

// findContentBlockEvents returns all ContentBlockEvent envelopes from the stream.
func findContentBlockEvents(envelopes []llm.Envelope) []*llm.ContentPartEvent {
	var out []*llm.ContentPartEvent
	for _, env := range envelopes {
		if env.Type == llm.StreamEventContentPart {
			cbe, ok := env.Data.(*llm.ContentPartEvent)
			if ok {
				out = append(out, cbe)
			}
		}
	}
	return out
}

// TestProcessor_ContentBlockEvent_TextBlock verifies that completing a text
// block emits a ContentBlockEvent with Kind=text, the accumulated text, and an
// empty Signature.
func TestProcessor_ContentBlockEvent_TextBlock(t *testing.T) {
	h := newHarness(ParseOpts{Model: "claude-sonnet-4-5"})

	envelopes := h.Send(
		MessageStartEvent{Message: MessageStartPayload{ID: "msg_t1", Model: "claude-sonnet-4-5", Usage: MessageUsage{InputTokens: 5}}},
		ContentBlockStartEvent{Index: 0, ContentBlock: ContentBlock{Type: "text"}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "text_delta", Text: "Hello, "}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "text_delta", Text: "world!"}},
		ContentBlockStopEvent{Index: 0},
		MessageDeltaEvent{Delta: MessageDelta{StopReason: "end_turn"}, Usage: OutputUsage{OutputTokens: 4}},
		MessageStopEvent{},
	)

	blocks := findContentBlockEvents(envelopes)
	require.Len(t, blocks, 1, "exactly one ContentBlockEvent expected for a single text block")

	b := blocks[0]
	assert.Equal(t, msg.PartTypeText, b.Part.Type, "block kind must be text")
	assert.Equal(t, "Hello, world!", b.Part.Text, "accumulated text must match all deltas")
	assert.Nil(t, b.Part.Thinking, "text blocks must not carry ThinkingConfig")
	assert.Equal(t, 0, b.Index, "block index must be preserved")
}

// TestProcessor_ContentBlockEvent_ThinkingBlock verifies that completing a
// ThinkingConfig block emits a ContentBlockEvent with Kind=ThinkingConfig, the accumulated
// ThinkingConfig text, and the exact Signature from the signature_delta event.
func TestProcessor_ContentBlockEvent_ThinkingBlock(t *testing.T) {
	h := newHarness(ParseOpts{Model: "claude-sonnet-4-5"})

	const wantSig = "eyJhbGciOiJFZERTQSJ9.abc123"

	envelopes := h.Send(
		MessageStartEvent{Message: MessageStartPayload{ID: "msg_t2", Model: "claude-sonnet-4-5", Usage: MessageUsage{InputTokens: 10}}},
		ContentBlockStartEvent{Index: 0, ContentBlock: ContentBlock{Type: "thinking"}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "thinking_delta", Thinking: "Let me reason"}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "thinking_delta", Thinking: " carefully."}},
		// signature_delta arrives once, after all thinking_delta events
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "signature_delta", Signature: wantSig}},
		ContentBlockStopEvent{Index: 0},
		MessageDeltaEvent{Delta: MessageDelta{StopReason: "end_turn"}, Usage: OutputUsage{OutputTokens: 8}},
		MessageStopEvent{},
	)

	blocks := findContentBlockEvents(envelopes)
	require.Len(t, blocks, 1, "exactly one ContentBlockEvent expected for a single ThinkingConfig block")

	b := blocks[0]
	assert.Equal(t, msg.PartTypeThinking, b.Part.Type, "block kind must be ThinkingConfig")
	assert.Equal(t, "Let me reason carefully.", b.Part.Thinking.Text, "accumulated ThinkingConfig text must match all deltas")
	assert.Equal(t, wantSig, b.Part.Thinking.Signature, "signature must be captured exactly from signature_delta — no mutation")
	assert.Equal(t, 0, b.Index, "block index must be preserved")
}

// TestProcessor_ContentBlockEvent_SignatureDeltaNotDropped verifies that if
// signature_delta arrives before the block stop, its value survives into the
// emitted ContentBlockEvent (i.e. is not silently discarded).
func TestProcessor_ContentBlockEvent_SignatureDeltaNotDropped(t *testing.T) {
	h := newHarness(ParseOpts{Model: "claude-sonnet-4-5"})

	const sig = "sig-must-survive"

	envelopes := h.Send(
		MessageStartEvent{Message: MessageStartPayload{ID: "msg_t3", Model: "claude-sonnet-4-5", Usage: MessageUsage{InputTokens: 10}}},
		ContentBlockStartEvent{Index: 0, ContentBlock: ContentBlock{Type: "thinking"}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "thinking_delta", Thinking: "Thought..."}},
		// Signature arrives as a delta event BEFORE the stop event.
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "signature_delta", Signature: sig}},
		ContentBlockStopEvent{Index: 0},
		MessageDeltaEvent{Delta: MessageDelta{StopReason: "end_turn"}, Usage: OutputUsage{OutputTokens: 3}},
		MessageStopEvent{},
	)

	blocks := findContentBlockEvents(envelopes)
	require.Len(t, blocks, 1)
	require.Equal(t, sig, blocks[0].Part.Thinking.Signature,
		"signature_delta value must not be dropped — it is required for API re-submission")
}

// TestProcessor_ContentBlockEvent_InterleavedOrder verifies that when a ThinkingConfig
// block (index 0) precedes a text block (index 1), both ContentBlockEvents are
// emitted and their indices match the wire order.
func TestProcessor_ContentBlockEvent_InterleavedOrder(t *testing.T) {
	h := newHarness(ParseOpts{Model: "claude-sonnet-4-5"})

	envelopes := h.Send(
		MessageStartEvent{Message: MessageStartPayload{ID: "msg_t4", Model: "claude-sonnet-4-5", Usage: MessageUsage{InputTokens: 15}}},
		// Block 0: ThinkingConfig
		ContentBlockStartEvent{Index: 0, ContentBlock: ContentBlock{Type: "thinking"}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "thinking_delta", Thinking: "Step 1."}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "signature_delta", Signature: "sig-idx0"}},
		ContentBlockStopEvent{Index: 0},
		// Block 1: text
		ContentBlockStartEvent{Index: 1, ContentBlock: ContentBlock{Type: "text"}},
		ContentBlockDeltaEvent{Index: 1, Delta: ContentBlockDelta{Type: "text_delta", Text: "The answer."}},
		ContentBlockStopEvent{Index: 1},
		MessageDeltaEvent{Delta: MessageDelta{StopReason: "end_turn"}, Usage: OutputUsage{OutputTokens: 10}},
		MessageStopEvent{},
	)

	blocks := findContentBlockEvents(envelopes)
	require.Len(t, blocks, 2, "one ContentBlockEvent per block")

	// Blocks must be emitted in wire order (stop order = index ascending).
	assert.Equal(t, 0, blocks[0].Index, "first block must have index 0")
	assert.Equal(t, msg.PartTypeThinking, blocks[0].Part.Type)
	assert.Equal(t, "Step 1.", blocks[0].Part.Thinking.Text)
	assert.Equal(t, "sig-idx0", blocks[0].Part.Thinking.Signature)

	assert.Equal(t, 1, blocks[1].Index, "second block must have index 1")
	assert.Equal(t, msg.PartTypeText, blocks[1].Part.Type)
	assert.Equal(t, "The answer.", blocks[1].Part.Text)
	assert.Nil(t, blocks[1].Part.Thinking)
}
