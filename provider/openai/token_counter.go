package openai

import (
	"context"
	"fmt"

	"github.com/codewandler/llm/tokencount"
)

// Compile-time assertion that *Provider implements llm.TokenCounter.
var _ tokencount.TokenCounter = (*Provider)(nil)

// CountTokens estimates the number of input tokens for the given request using
// the tiktoken tokenizer. The encoding is selected per-model (o200k_base for
// GPT-4o and o-series; cl100k_base for GPT-4 and GPT-3.5).
//
// The formula follows the OpenAI cookbook:
//   - +4 tokens per message (role/framing overhead)
//   - +3 tokens reply priming ("assistant" token prepended by the API)
//
// Counts are exact for text-only conversations. Tool-heavy requests may vary
// by a small margin due to serialisation format differences.
func (p *Provider) CountTokens(_ context.Context, req tokencount.TokenCountRequest) (*tokencount.TokenCount, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel
	}

	enc, _ := tokencount.EncodingForModel(model)
	if enc == "" {
		enc = tokencount.EncodingCL100K
	}

	tc := &tokencount.TokenCount{}
	// OpenAI overhead: 4 tokens per message + 3 tokens reply priming
	const perMsgOverhead = 4
	const replyPriming = 3

	if err := tokencount.CountMessagesAndTools(tc, req, tokencount.CountOpts{
		Encoding:       enc,
		PerMsgOverhead: perMsgOverhead,
		ReplyPriming:   replyPriming,
	}); err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}
	return tc, nil
}
