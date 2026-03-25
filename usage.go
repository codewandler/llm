package llm

// Usage holds token counts and cost from a provider response.
type Usage struct {
	// InputTokens is the total number of input tokens processed, including
	// tokens served from cache (CacheReadTokens) and tokens written to cache
	// (CacheWriteTokens). Callers can use this as the single "how many input
	// tokens did this request consume" figure.
	InputTokens int `json:"input_tokens"`

	// OutputTokens is the number of tokens generated in the response.
	OutputTokens int `json:"output_tokens"`

	// TotalTokens is InputTokens + OutputTokens.
	TotalTokens int `json:"total_tokens"`

	// Cost is the total request cost in USD.
	// For Anthropic, Bedrock, and OpenAI this is locally calculated from
	// provider pricing tables and equals the sum of the breakdown fields below.
	// For OpenRouter this is API-reported by the proxy (already includes cache pricing).
	Cost float64 `json:"cost"`

	// Detailed token breakdown (provider-specific, may be zero).
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`  // Input tokens served from an existing cache entry (all providers).
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"` // Input tokens written to a new cache entry (Anthropic, Bedrock).
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`   // ToolOutput tokens consumed by model reasoning (e.g. extended thinking).

	// Granular cost breakdown in USD (zero if provider/model pricing is unknown).
	// Sum of InputCost + CacheReadCost + CacheWriteCost + OutputCost == Cost.
	// Not populated for OpenRouter (API-reported cost is used instead).
	//
	// InputCost covers only the non-cached, non-write portion:
	// InputTokens - CacheReadTokens - CacheWriteTokens tokens at the regular input rate.
	InputCost      float64 `json:"input_cost,omitempty"`       // Cost of non-cached, non-write input tokens.
	CacheReadCost  float64 `json:"cache_read_cost,omitempty"`  // Cost of cache-read tokens.
	CacheWriteCost float64 `json:"cache_write_cost,omitempty"` // Cost of cache-write tokens.
	OutputCost     float64 `json:"output_cost,omitempty"`      // Cost of output tokens.
}
