package unified

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
}

// CompletionsExtras stores Chat Completions-specific request fields.
type CompletionsExtras struct {
	PromptCacheRetention string
}

// ResponsesExtras stores Responses API-specific request fields.
type ResponsesExtras struct {
	PromptCacheRetention string
}
