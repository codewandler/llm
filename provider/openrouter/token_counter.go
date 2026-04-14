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
// For Anthropic models (claude-*), this uses the Anthropic-specific counter
// which includes tool-use preamble and per-tool framing overhead.
// For OpenAI models, the encoding is selected based on model ID (o200k_base
// for GPT-4o/o-series; cl100k_base for everything else).
func (p *Provider) CountTokens(_ context.Context, req tokencount.TokenCountRequest) (*tokencount.TokenCount, error) {
	req.Model = p.normalizeRequestModel(req.Model)
	normalizedModel := normalizeTokenizerModel(req.Model)

	// Anthropic models behind OpenRouter use the same tokenizer and tool
	// overhead as the native Anthropic provider.
	if isAnthropicModel(normalizedModel) {
		tc := &tokencount.TokenCount{}
		if err := tokencount.CountMessagesAndToolsAnthropic(tc, req); err != nil {
			return nil, fmt.Errorf("openrouter: %w", err)
		}
		return tc, nil
	}

	enc, _ := tokencount.EncodingForModel(normalizedModel)
	tc := &tokencount.TokenCount{}
	if err := tokencount.CountMessagesAndTools(tc, req, tokencount.CountOpts{Encoding: enc}); err != nil {
		return nil, fmt.Errorf("openrouter: %w", err)
	}
	return tc, nil
}

// isAnthropicModel returns true if the (provider-prefix-stripped) model ID
// identifies an Anthropic Claude model.
func isAnthropicModel(model string) bool {
	return strings.HasPrefix(model, "claude-") || strings.HasPrefix(model, "claude3") || strings.HasPrefix(model, "claude-3")
}

func normalizeTokenizerModel(model string) string {
	if slash := strings.IndexByte(model, '/'); slash >= 0 && slash < len(model)-1 {
		return model[slash+1:]
	}
	return model
}
