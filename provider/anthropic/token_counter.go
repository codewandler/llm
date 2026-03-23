package anthropic

import (
	"context"
	"fmt"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tokencount"
)

// Compile-time assertion that *Provider implements llm.TokenCounter.
var _ llm.TokenCounter = (*Provider)(nil)

// CountTokens estimates the number of input tokens for the given request.
//
// This is a local approximation using the cl100k_base BPE encoding — no
// network call is made. Anthropic's tokenizer is proprietary and not publicly
// available; cl100k_base gives ±5–10% accuracy for English text.
//
// For exact counts, use the Anthropic /v1/messages/count_tokens API directly.
func (p *Provider) CountTokens(_ context.Context, req llm.TokenCountRequest) (*llm.TokenCount, error) {
	tc := &llm.TokenCount{}
	if err := llm.CountMessagesAndTools(tc, req, tokencount.EncodingCL100K, 0, 0); err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	return tc, nil
}
