package openai

import (
	"log/slog"

	"github.com/codewandler/llm/usage"
)

// buildUsageTokenItems converts raw OpenAI API token counts into usage.TokenItems.
//
// OpenAI reports:
//   - prompt_tokens: total input tokens (including cached)
//   - completion_tokens: total output tokens (including reasoning)
//   - prompt_tokens_details.cached_tokens: subset served from cache
//   - completion_tokens_details.reasoning_tokens: subset used for reasoning
//
// This function:
//  1. Separates regular input from cached input (KindInput = prompt - cached)
//  2. Separates regular output from reasoning (KindOutput = completion - reasoning)
//  3. Clamps negative values to 0 with optional logging
//
// The returned items contain no overlap: every token is counted exactly once
// under one kind.
func buildUsageTokenItems(input, output, cached, reasoning int, logger *slog.Logger, provider, model string) usage.TokenItems {
	regularInput := input - cached
	if regularInput < 0 {
		if logger != nil {
			logger.Warn("clamping negative regularInput to 0",
				"provider", provider,
				"model", model,
				"inputTokens", input,
				"cachedTokens", cached,
			)
		}
		regularInput = 0
	}

	var outputItem, reasoningItem usage.TokenItem
	if reasoning > 0 {
		netOutput := output - reasoning
		if netOutput < 0 {
			if logger != nil {
				logger.Warn("clamping negative netOutput to 0",
					"provider", provider,
					"model", model,
					"outputTokens", output,
					"reasoningTokens", reasoning,
				)
			}
			netOutput = 0
		}
		outputItem = usage.TokenItem{Kind: usage.KindOutput, Count: netOutput}
		reasoningItem = usage.TokenItem{Kind: usage.KindReasoning, Count: reasoning}
	} else {
		outputItem = usage.TokenItem{Kind: usage.KindOutput, Count: output}
	}

	items := usage.TokenItems{
		{Kind: usage.KindInput, Count: regularInput},
		outputItem,
	}.NonZero()
	if cached > 0 {
		items = append(items, usage.TokenItem{Kind: usage.KindCacheRead, Count: cached})
	}
	if reasoning > 0 {
		items = append(items, reasoningItem)
	}

	return items
}
