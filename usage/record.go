package usage

import "time"

// TokenKind identifies one independently-priced token category.
type TokenKind string

const (
	// KindInput are regular (non-cache) input tokens.
	// For providers with cache support this is:
	//   total_input_tokens - cache_read_tokens - cache_write_tokens
	// Priced at the model's standard input rate.
	KindInput TokenKind = "input"

	// KindOutput are generated/completion tokens, EXCLUDING reasoning tokens.
	// When the upstream API reports reasoning tokens separately, providers MUST
	// subtract them here so that KindOutput + KindReasoning == total completions
	// with no overlap. When the API cannot distinguish, KindOutput holds the
	// full completion count and KindReasoning is omitted.
	// Priced at the model's standard output rate.
	KindOutput TokenKind = "output"

	// KindReasoning are thinking/reasoning tokens reported by the provider.
	// Only emitted when the upstream API reports them as a distinct value.
	// They are NOT included in KindOutput (no double-counting).
	// Priced at the model's reasoning rate; falls back to output rate when
	// Pricing.Reasoning == 0 (true for all current providers).
	KindReasoning TokenKind = "reasoning"

	// KindCacheRead are input tokens served from an existing prompt cache entry.
	// Anthropic: cache_read_input_tokens.
	// OpenAI:    prompt_tokens_details.cached_tokens.
	// Priced at a reduced cache-read rate; NOT included in KindInput.
	KindCacheRead TokenKind = "cache_read"

	// KindCacheWrite are input tokens written to a new prompt cache entry.
	// Anthropic / Bedrock only (cache_creation_input_tokens).
	// Priced at a cache-write rate; NOT included in KindInput.
	KindCacheWrite TokenKind = "cache_write"
)

// TokenItem is one independently-priced token entry.
type TokenItem struct {
	Kind  TokenKind `json:"kind"`
	Count int       `json:"count"`
}

// TokenItems is the ordered list of token items for a Record.
// Each kind appears at most once per record.
type TokenItems []TokenItem

// Count returns the count for the item with the given kind.
// Returns 0 if no item with that kind exists.
func (t TokenItems) Count(kind TokenKind) int {
	for _, item := range t {
		if item.Kind == kind {
			return item.Count
		}
	}
	return 0
}

// TotalInput returns Input + CacheRead + CacheWrite.
func (t TokenItems) TotalInput() int {
	return t.Count(KindInput) + t.Count(KindCacheRead) + t.Count(KindCacheWrite)
}

// TotalOutput returns Output + Reasoning.
func (t TokenItems) TotalOutput() int {
	return t.Count(KindOutput) + t.Count(KindReasoning)
}

// Total returns TotalInput + TotalOutput.
func (t TokenItems) Total() int {
	return t.TotalInput() + t.TotalOutput()
}

// NonZero returns a new slice with zero-count items removed.
func (t TokenItems) NonZero() TokenItems {
	var result TokenItems
	for _, item := range t {
		if item.Count > 0 {
			result = append(result, item)
		}
	}
	return result
}

// Cost holds monetary amounts in USD.
// All fields are derived UNLESS Source == "reported" (provider sent the value directly).
type Cost struct {
	Total      float64 `json:"total"`
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	Reasoning  float64 `json:"reasoning,omitempty"` // zero when KindReasoning not present
	CacheRead  float64 `json:"cache_read,omitempty"`
	CacheWrite float64 `json:"cache_write,omitempty"`

	// Source describes how the cost was determined.
	//   "calculated" — via CalcCost from KnownPricing or the built-in catalog
	//   "reported"   — API-provided total (OpenRouter)
	//   "estimated"  — pre-request estimate from CountTokens
	//   ""           — no pricing available (Ollama local, unknown model)
	Source string `json:"source,omitempty"`
}

func (c Cost) IsZero() bool { return c.Source == "" && c.Total == 0 }

// Dims carries attribution context for a Record.
type Dims struct {
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`    // caller-assigned turn identifier
	SessionID string `json:"session_id,omitempty"` // caller-assigned session identifier

	// Labels are arbitrary string key-value annotations on the Record.
	// Used to distinguish sub-breakdowns within a request, e.g. in estimates:
	//   {"category": "system"}       — system prompt tokens
	//   {"category": "conversation"} — conversation history tokens
	//   {"category": "tools"}        — tool definition tokens
	// Provider-reported records carry no labels.
	Labels map[string]string `json:"labels,omitempty"`
}

// Record is a single, fully-attributed usage record.
type Record struct {
	Tokens     TokenItems `json:"tokens"`
	Cost       Cost       `json:"cost"`
	Dims       Dims       `json:"dims"`
	IsEstimate bool       `json:"is_estimate,omitempty"`
	RecordedAt time.Time  `json:"recorded_at"`

	// Source describes how the token count was obtained.
	//   "api"       — exact count from the provider's token-counting endpoint
	//   "heuristic" — local BPE approximation (e.g. cl100k_base)
	//   ""          — actual usage reported by the provider (post-request)
	Source string `json:"source,omitempty"`

	// Encoder identifies the tokenizer or counting method used for heuristic estimates.
	// Examples: "cl100k_base", "o200k_base", "cl100k_base+anthropic_overhead".
	// Empty for API-sourced counts and post-request actual usage records.
	Encoder string `json:"encoder,omitempty"`

	// Extras holds provider-specific metadata captured at request time.
	// Mirrors the StreamStartedEvent.Extra convention (same map[string]any type).
	// Keys and value types are defined per provider:
	//
	//   Anthropic / Claude-OAuth: "rate_limits" -> *llm.RateLimits
	//     Contains 5h/7d window utilisation, overage status, fallback percentage,
	//     and representative claim. Populated from HTTP response headers.
	//
	// nil for estimate records and for providers that return no extras.
	Extras map[string]any `json:"extras,omitempty"`
}
