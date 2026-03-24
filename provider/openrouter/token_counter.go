package openrouter

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
// The BPE encoding is selected based on the model ID (o200k_base for GPT-4o /
// o-series; cl100k_base for everything else including unknown models).
// No per-message overhead is applied — OpenRouter routes to many providers and
// the overhead constants are model-family-specific.
func (p *Provider) CountTokens(_ context.Context, req llm.TokenCountRequest) (*llm.TokenCount, error) {
	enc, _ := tokencount.EncodingForModel(req.Model)

	tc := &llm.TokenCount{}
	if err := llm.CountMessagesAndTools(tc, req, llm.CountOpts{Encoding: enc}); err != nil {
		return nil, fmt.Errorf("openrouter: %w", err)
	}
	return tc, nil
}
