package claude

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
// The estimate includes:
//   - The three system blocks this provider injects at send time
//     (billingHeader, systemCore, systemIdentity), each counted as a
//     separate content block matching how buildRequest serialises them
//   - Anthropic's hidden tool-use system preamble (~330 tokens, when tools present)
//   - Per-tool serialisation framing overhead (~126 first, ~85 each additional)
//   - cl100k_base token counts for all messages and tool definitions
//
// The underlying tokenizer is cl100k_base — an approximation since Anthropic's
// tokenizer is proprietary. Expect ±10-15% accuracy for English text.
func (p *Provider) CountTokens(_ context.Context, req llm.TokenCountRequest) (*llm.TokenCount, error) {
	// Count the three injected system blocks individually, mirroring the
	// separate {Type:"text", Text:...} entries that buildRequest prepends.
	injectedTokens := 0
	for _, text := range []string{billingHeader, systemCore, systemIdentity} {
		n, err := tokencount.CountText(tokencount.EncodingCL100K, text)
		if err != nil {
			return nil, fmt.Errorf("claude: count injected system tokens: %w", err)
		}
		injectedTokens += n
	}

	tc := &llm.TokenCount{}
	if err := llm.CountMessagesAndToolsAnthropic(tc, req); err != nil {
		return nil, fmt.Errorf("claude: %w", err)
	}

	// Add the injected system tokens to the total and the system breakdown.
	tc.InputTokens += injectedTokens
	tc.SystemTokens += injectedTokens

	return tc, nil
}
