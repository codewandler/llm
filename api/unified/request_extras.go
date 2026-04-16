package unified

import "github.com/codewandler/llm/api/messages"

// RequestExtras preserves protocol/provider-specific request details that are
// not part of the canonical schema.
type RequestExtras struct {
	Messages    *MessagesExtras
	Completions *CompletionsExtras
	Responses   *ResponsesExtras
	Provider    map[string]any
}

// MessagesExtras stores Anthropic Messages-specific request fields.
type MessagesExtras struct {
	AnthropicBeta []string
	StopSequences []string

	ThinkingType         string
	ThinkingBudgetTokens int
	ThinkingDisplay      string

	RequestCacheControl   *messages.CacheControl
	MessageCachePartIndex map[int]int
}

// CompletionsExtras stores Chat Completions-specific request fields.
type CompletionsExtras struct {
	PromptCacheRetention string
	Stop                 []string
	N                    int
	PresencePenalty      float64
	FrequencyPenalty     float64
	LogProbs             bool
	TopLogProbs          int
	Store                bool
	ParallelToolCalls    bool
	ServiceTier          string
	ExtraMetadata        map[string]any
}

// ResponsesExtras stores Responses API-specific request fields.
type ResponsesExtras struct {
	PromptCacheRetention string
	PreviousResponseID   string
	ReasoningSummary     string
	Store                bool
	ParallelToolCalls    bool
	UseInstructions      *bool
	UsedMaxTokenField    string
	ExtraMetadata        map[string]any
}
