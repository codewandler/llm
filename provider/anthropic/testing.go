package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/codewandler/llm"
)

// Harness wires a streamProcessor to a collector so tests can feed typed
// Anthropic events directly and inspect all emitted llm.Envelope values —
// no SSE encoding or decoding involved.
//
// Use NewHarness to create one. Call Send with any sequence of event structs;
// it routes each to the matching processor method and returns all envelopes
// emitted during that sequence.
type Harness struct {
	proc *streamProcessor
	pub  llm.Publisher
	ch   <-chan llm.Envelope
}

// NewHarness creates a Harness backed by a fresh streamProcessor and publisher.
func NewHarness(opts ParseOpts) *Harness {
	pub, ch := llm.NewEventPublisher()
	return &Harness{
		proc: newStreamProcessor(opts, pub),
		pub:  pub,
		ch:   ch,
	}
}

// Send routes each typed event to the matching on* method on the processor,
// then closes the publisher and drains all emitted envelopes.
//
// Accepted event types:
//   - MessageStartEvent
//   - MessageDeltaEvent
//   - MessageStopEvent
//   - ContentBlockStartEvent
//   - ContentBlockDeltaEvent
//   - ContentBlockStopEvent
//   - StreamErrorEvent
//
// Unrecognised types cause a panic to surface test mistakes early.
func (h *Harness) Send(events ...any) []llm.Envelope {
	for _, ev := range events {
		switch evt := ev.(type) {
		case MessageStartEvent:
			h.proc.onMessageStart(evt)
		case MessageDeltaEvent:
			h.proc.onMessageDelta(evt)
		case MessageStopEvent:
			h.proc.onMessageStop()
		case ContentBlockStartEvent:
			h.proc.onContentBlockStart(evt)
		case ContentBlockDeltaEvent:
			h.proc.onContentBlockDelta(evt)
		case ContentBlockStopEvent:
			h.proc.onContentBlockStop(evt)
		case StreamErrorEvent:
			h.proc.onError(evt)
		default:
			panic(fmt.Sprintf("anthropic.Harness.Send: unsupported event type %T", ev))
		}
	}
	h.pub.Close()

	var out []llm.Envelope
	for env := range h.ch {
		out = append(out, env)
	}
	return out
}

// BuildSSEBody serialises a sequence of typed Anthropic event structs into a
// valid SSE wire-format io.ReadCloser. The returned body can be passed directly
// to ParseStream or any provider that delegates to it (e.g. minimax).
//
// Accepted event types:
//   - MessageStartEvent       → "message_start"
//   - MessageDeltaEvent       → "message_delta"
//   - MessageStopEvent        → "message_stop"
//   - ContentBlockStartEvent  → "content_block_start"
//   - ContentBlockDeltaEvent  → "content_block_delta"
//   - ContentBlockStopEvent   → "content_block_stop"
//   - StreamErrorEvent        → "error"
//
// Unrecognised types cause a panic to surface test mistakes early.
func BuildSSEBody(events ...any) io.ReadCloser {
	var b strings.Builder
	for _, ev := range events {
		var eventType string
		switch ev.(type) {
		case MessageStartEvent:
			eventType = "message_start"
		case MessageDeltaEvent:
			eventType = "message_delta"
		case MessageStopEvent:
			eventType = "message_stop"
		case ContentBlockStartEvent:
			eventType = "content_block_start"
		case ContentBlockDeltaEvent:
			eventType = "content_block_delta"
		case ContentBlockStopEvent:
			eventType = "content_block_stop"
		case StreamErrorEvent:
			eventType = "error"
		default:
			panic(fmt.Sprintf("anthropic.BuildSSEBody: unsupported event type %T", ev))
		}

		payload := sseWithTypeField(ev, eventType)
		data, err := json.Marshal(payload)
		if err != nil {
			panic(fmt.Sprintf("anthropic.BuildSSEBody: marshal %T: %v", ev, err))
		}
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", eventType, data)
	}
	return io.NopCloser(strings.NewReader(b.String()))
}

// sseWithTypeField returns a map[string]any containing all fields of v plus
// "type": typeName, mirroring the Anthropic wire format where every SSE data
// payload includes a "type" discriminator field.
func sseWithTypeField(v any, typeName string) map[string]any {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("anthropic.BuildSSEBody: marshal: %v", err))
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		// MessageStopEvent is an empty struct — produces null or empty JSON.
		m = make(map[string]any)
	}
	m["type"] = typeName
	return m
}
