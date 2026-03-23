package bedrock

import (
	"context"
	"fmt"

	"github.com/codewandler/llm"
)

// Compile-time assertion that *Provider implements llm.TokenCounter.
var _ llm.TokenCounter = (*Provider)(nil)

// CountTokens estimates the number of input tokens for the given request.
//
// This is a local approximation using the cl100k_base BPE encoding — no
// network call is made. Claude on Bedrock uses the same proprietary tokenizer
// as direct Anthropic; cl100k_base gives ±5–10% accuracy for English text.
// Bedrock does not expose a server-side token counting endpoint.
func (p *Provider) CountTokens(_ context.Context, req llm.TokenCountRequest) (*llm.TokenCount, error) {
	tc := &llm.TokenCount{}
	if err := llm.CountMessagesAndToolsAnthropic(tc, req); err != nil {
		return nil, fmt.Errorf("bedrock: %w", err)
	}
	return tc, nil
}
