package anthropic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDispatch_EmptyDataLine(t *testing.T) {
	h := newHarness(ParseOpts{Model: "m"})
	result := h.proc.dispatch("")
	assert.True(t, result, "empty line should return true (stream continues)")
}

func TestDispatch_MalformedJSON(t *testing.T) {
	h := newHarness(ParseOpts{Model: "m"})
	result := h.proc.dispatch("not valid json at all {{{")
	assert.True(t, result, "malformed JSON should return true (stream continues)")
}

func TestDispatch_UnknownEventType(t *testing.T) {
	h := newHarness(ParseOpts{Model: "m"})
	result := h.proc.dispatch(`{"type":"ping","data":"keepalive"}`)
	assert.True(t, result, "unknown event type should return true (stream continues)")
}
