package minimax

import (
	"context"
	"fmt"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tokencount"
)

// Compile-time assertion that *Provider implements llm.TokenCounter.
var _ tokencount.TokenCounter = (*Provider)(nil)

// Token overhead constants measured empirically against the current
// MiniMax-M2.7 Anthropic-compatible API. The current endpoint behavior no
// longer shows a separate hidden-system surcharge or stable extra tool framing
// overhead beyond the raw schema tokens counted by CountMessagesAndTools.
const (
	// perMsgOverhead is added per message by CountMessagesAndTools to approximate
	// MiniMax's per-message framing tokens. Derived from calibration against the
	// live API after the Anthropic-compatible endpoint switched to a lower
	// hidden-framing overhead in 2026.
	perMsgOverhead = 3

	// minimaxHiddenSystemPromptTokens is the cost of the hidden default system prompt
	// MiniMax injects when no system message is provided by the caller.
	// Disappears when the caller provides a system message.
	minimaxHiddenSystemPromptTokens = 0

	// minimaxToolPreamble is the framing overhead injected once when tools are
	// present in the request (tool schema serialisation overhead beyond raw JSON).
	minimaxToolPreamble = 0

	// minimaxToolPerExtra is the per-tool framing overhead for each tool beyond
	// the first.
	minimaxToolPerExtra = 0
)

// hasSystemMessage reports whether msgs contains at least one System.
func hasSystemMessage(msgs llm.Messages) bool {
	for _, m := range msgs {
		if m.IsSystem() {
			return true
		}
	}
	return false
}

// CountTokens estimates the number of input tokens for the given request using
// the MiniMax BPE tokenizer (200K vocab, loaded from HuggingFace on first use).
//
// The estimate accounts for:
//   - Raw BPE token counts per message and tool definition
//   - Current per-message framing overhead (3 tokens/message)
//   - No additional hidden-system surcharge in current endpoint behavior
//   - No additional stable tool-framing surcharge beyond raw schema tokens
//
// The constants are calibrated against the current MiniMax-M2.7 API and are
// intentionally conservative; integration tests track drift against live usage.
func (p *Provider) CountTokens(_ context.Context, req tokencount.TokenCountRequest) (*tokencount.TokenCount, error) {
	tc := &tokencount.TokenCount{}
	if err := tokencount.CountMessagesAndTools(tc, req, tokencount.CountOpts{
		Encoding:       tokencount.EncodingMinimax,
		PerMsgOverhead: perMsgOverhead,
	}); err != nil {
		return nil, fmt.Errorf("minimax: %w", err)
	}

	// Account for hidden system prompt when no system message is provided.
	if !hasSystemMessage(req.Messages) {
		tc.OverheadTokens += minimaxHiddenSystemPromptTokens
		tc.InputTokens += minimaxHiddenSystemPromptTokens
	}

	// Account for tool schema framing overhead.
	if n := len(req.Tools); n > 0 {
		toolOverhead := minimaxToolPreamble + (n-1)*minimaxToolPerExtra
		tc.OverheadTokens += toolOverhead
		tc.InputTokens += toolOverhead
	}

	return tc, nil
}
