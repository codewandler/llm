package openrouter

import (
	"context"
	"fmt"
	"strings"

	"github.com/codewandler/llm/tokencount"
)

// Compile-time assertion that *Provider implements llm.TokenCounter.
var _ tokencount.TokenCounter = (*Provider)(nil)

// CountTokens estimates the number of input tokens for the given request.
//
// The BPE encoding is selected based on the model ToolCallID (o200k_base for GPT-4o /
// o-series; cl100k_base for everything else including unknown models).
// No per-message overhead is applied — OpenRouter routes to many providers and
// the overhead constants are model-family-specific.
func (p *Provider) CountTokens(_ context.Context, req tokencount.TokenCountRequest) (*tokencount.TokenCount, error) {
	req.Model = p.normalizeRequestModel(req.Model)
	normalizedModel := normalizeTokenizerModel(req.Model)
	enc, _ := tokencount.EncodingForModel(normalizedModel)

	tc := &tokencount.TokenCount{}
	if err := tokencount.CountMessagesAndTools(tc, req, tokencount.CountOpts{Encoding: enc}); err != nil {
		return nil, fmt.Errorf("openrouter: %w", err)
	}
	return tc, nil
}

func normalizeTokenizerModel(model string) string {
	if slash := strings.IndexByte(model, '/'); slash >= 0 && slash < len(model)-1 {
		return model[slash+1:]
	}
	return model
}
