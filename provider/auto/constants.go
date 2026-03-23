// Package auto provides zero-config multi-provider setup for LLM providers.
package auto

// Provider type names.
const (
	ProviderClaude     = "claude"
	ProviderBedrock    = "bedrock"
	ProviderOpenAI     = "openai"
	ProviderOpenRouter = "openrouter"
	ProviderAnthropic  = "anthropic"
	ProviderMiniMax    = "minimax"
)

// Environment variable names.
const (
	EnvOpenAIKey     = "OPENAI_API_KEY"
	EnvOpenAIKeyAlt  = "OPENAI_KEY"
	EnvOpenRouterKey = "OPENROUTER_API_KEY"
	EnvAnthropicKey  = "ANTHROPIC_API_KEY"
	EnvMiniMaxKey    = "MINIMAX_API_KEY"
)

// Global model aliases.
const (
	AliasFast     = "fast"
	AliasDefault  = "default"
	AliasPowerful = "powerful"
	AliasCodex    = "codex"
)
