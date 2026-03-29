package anthropic

import (
	"context"
	"fmt"

	"github.com/codewandler/llm/tokencount"
)

// Compile-time assertion that *Provider implements llm.TokenCounter.
var _ tokencount.TokenCounter = (*Provider)(nil)

// CountTokens estimates the number of input tokens for the given Request.
//
// This is a local approximation using the cl100k_base BPE encoding — no
// network call is made. Anthropic's tokenizer is proprietary and not publicly
// available; cl100k_base gives ±5–10% accuracy for English text.
//
// For exact counts, use the Anthropic /v1/messages/count_tokens API directly.
func (p *Provider) CountTokens(_ context.Context, req tokencount.TokenCountRequest) (*tokencount.TokenCount, error) {
	tc := &tokencount.TokenCount{}
	if err := tokencount.CountMessagesAndToolsAnthropic(tc, req); err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	return tc, nil
}
