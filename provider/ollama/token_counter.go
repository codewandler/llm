package ollama

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
// network call is made. Ollama does not expose a public tokenize API endpoint;
// the internal Tokenize function is not accessible over HTTP. Models hosted by
// Ollama use a variety of tokenizers (llama, qwen, phi, gemma); cl100k_base
// gives a rough ±10% estimate for English text.
func (p *Provider) CountTokens(_ context.Context, req llm.TokenCountRequest) (*llm.TokenCount, error) {
	tc := &llm.TokenCount{}
	if err := llm.CountMessagesAndTools(tc, req, tokencount.EncodingCL100K, 0, 0); err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	return tc, nil
}
