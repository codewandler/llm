package claude

import (
	"context"
	"fmt"

	"github.com/codewandler/llm/tokencount"
)

// Compile-time assertion that *Provider implements llm.TokenCounter.
var _ tokencount.TokenCounter = (*Provider)(nil)

// CountTokens estimates the number of input tokens for the given request.
//
// The estimate includes:
//   - The two system blocks this provider injects at send time
//     (billingHeader, systemCore), each counted as a
//     separate content block matching how buildRequest serialises them
//   - Anthropic's hidden tool-use system preamble (~330 tokens, when tools present)
//   - Per-tool serialisation framing overhead (~126 first, ~85 each additional)
//   - cl100k_base token counts for all messages and tool definitions
//
// The underlying tokenizer is cl100k_base — an approximation since Anthropic's
// tokenizer is proprietary. Expect ±10-15% accuracy for English text.
func (p *Provider) CountTokens(_ context.Context, req tokencount.TokenCountRequest) (*tokencount.TokenCount, error) {
	// Count the two injected system blocks individually, mirroring the
	// separate {EventType:"text", Content:...} entries that buildRequest prepends.
	injectedTokens := 0
	for _, text := range []string{billingHeader, systemCore} {
		n, err := tokencount.CountTextForEncoding(tokencount.EncodingCL100K, text)
		if err != nil {
			return nil, fmt.Errorf("claude: count injected system tokens: %w", err)
		}
		injectedTokens += n
	}

	tc := &tokencount.TokenCount{}
	if err := tokencount.CountMessagesAndToolsAnthropic(tc, req); err != nil {
		return nil, fmt.Errorf("claude: %w", err)
	}

	// The injected system blocks are provider overhead — the caller did not
	// supply them. Track them in OverheadTokens rather than SystemTokens so
	// SystemTokens reflects only what the caller sent.
	tc.OverheadTokens += injectedTokens
	tc.InputTokens += injectedTokens

	return tc, nil
}
