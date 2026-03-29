package anthropic

import (
	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

// CacheControl is the Anthropic API wire type for cache breakpoints.
type CacheControl struct {
	Type string `json:"type"`          // always "ephemeral"
	TTL  string `json:"ttl,omitempty"` // "1h" for extended TTL; omit for default 5m
}

// buildCacheControl converts a CacheHint to the Anthropic wire type.
// Returns nil if hint is nil or not enabled.
func buildCacheControl(h *llm.CacheHint) *CacheControl {
	if h == nil || !h.Enabled {
		return nil
	}
	cc := &CacheControl{Type: "ephemeral"}
	if h.TTL == "1h" {
		cc.TTL = "1h"
	}
	return cc
}

func (m *Message) setCacheControl(cacheHint *msg.CacheHint) {
	if m == nil || len(m.Content) == 0 {
		return
	}

	cc := buildCacheControl(cacheHint)
	if cc == nil {
		return
	}

	// TODO: find most recent cachable part of block

	m.Content[len(m.Content)-1].setCacheControl(cc)

	// TODO: set cacheControl for that block

}
