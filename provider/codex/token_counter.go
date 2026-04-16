package codex

import (
	"context"
	"fmt"

	"github.com/codewandler/llm/tokencount"
)

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
	const perMsgOverhead = 4
	const replyPriming = 3
	if err := tokencount.CountMessagesAndTools(tc, req, tokencount.CountOpts{
		Encoding:       enc,
		PerMsgOverhead: perMsgOverhead,
		ReplyPriming:   replyPriming,
	}); err != nil {
		return nil, fmt.Errorf("codex: %w", err)
	}
	return tc, nil
}
