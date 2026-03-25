package minimax

import (
	"context"
	"fmt"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tokencount"
)

// Compile-time assertion that *Provider implements llm.TokenCounter.
var _ llm.TokenCounter = (*Provider)(nil)

// Token overhead constants measured empirically against MiniMax-M2.7 API.
//
// Base framing:
//   - MiniMax injects a hidden default system prompt when no System is provided.
//     Cost: ~35 tokens. Add a system message, this disappears (user's system message
//     replaces the hidden one). For realistic multi-message conversations with a system
//     message, perMsgOverhead=6 covers the per-message framing with 0–7% drift.
//
// Tool framing (measured via calibration with 1/2/3 tools, user-only message):
//   - minimaxToolPreamble: tokens injected once when any tools are present.
//   - minimaxToolPerExtra: additional tokens per tool beyond the first.
//
// Calibration data (MiniMax-M2.7, user("What is the weather in Berlin?"), no system):
//
//	0 tools: actual=48,  estimated(raw+perMsg)=13, unaccounted=35 (hidden system prompt)
//	1 tool:  actual=205, estimated=54,              unaccounted=151 → tool_overhead=151-35=116
//	2 tools: actual=262, estimated=91,              unaccounted=171 → delta_tool=20
//	3 tools: actual=318, estimated=127,             unaccounted=191 → delta_tool=20
const (
	// perMsgOverhead is added per message by CountMessagesAndTools to approximate
	// MiniMax's per-message framing tokens. Derived from multi-message tests:
	//   2 msgs (system+user, actual=27, raw=13): 14/2 = 7/msg
	//   4 msgs (actual=51, raw=27): 24/4 = 6/msg
	perMsgOverhead = 6

	// minimaxHiddenSystemPromptTokens is the cost of the hidden default system prompt
	// MiniMax injects when no system message is provided by the caller.
	// Disappears when the caller provides a system message.
	minimaxHiddenSystemPromptTokens = 35

	// minimaxToolPreamble is the framing overhead injected once when tools are
	// present in the request (tool schema serialisation overhead beyond raw JSON).
	minimaxToolPreamble = 116

	// minimaxToolPerExtra is the per-tool framing overhead for each tool beyond
	// the first.
	minimaxToolPerExtra = 20
)

// hasSystemMessage reports whether msgs contains at least one System.
func hasSystemMessage(msgs llm.Messages) bool {
	for _, m := range msgs {
		if _, ok := m.(llm.SystemMessage); ok {
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
//   - Per-message framing overhead (6 tokens/message)
//   - Hidden default system prompt (35 tokens) when no system message is provided
//   - Tool schema framing (116 tokens once + 20 tokens per additional tool)
//
// All constants are empirically calibrated against the MiniMax-M2.7 API.
func (p *Provider) CountTokens(_ context.Context, req llm.TokenCountRequest) (*llm.TokenCount, error) {
	tc := &llm.TokenCount{}
	if err := llm.CountMessagesAndTools(tc, req, llm.CountOpts{
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
