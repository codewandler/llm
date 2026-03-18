// Package auto provides zero-config multi-provider setup for LLM providers.
package auto

// Provider type names.
const (
	ProviderClaude     = "claude"
	ProviderBedrock    = "bedrock"
	ProviderOpenAI     = "openai"
	ProviderOpenRouter = "openrouter"
	ProviderAnthropic  = "anthropic"
)

// Environment variable names.
const (
	EnvOpenAIKey     = "OPENAI_API_KEY"
	EnvOpenAIKeyAlt  = "OPENAI_KEY"
	EnvOpenRouterKey = "OPENROUTER_API_KEY"
	EnvAnthropicKey  = "ANTHROPIC_API_KEY"
)

// Global model aliases.
const (
	AliasFast     = "fast"
	AliasDefault  = "default"
	AliasPowerful = "powerful"
)

// Claude model IDs.
const (
	ClaudeOpus   = "claude-opus-4-6"
	ClaudeSonnet = "claude-sonnet-4-6"
	ClaudeHaiku  = "claude-haiku-4-5-20251001"
)

// Bedrock model IDs.
const (
	BedrockOpus   = "anthropic.claude-opus-4-20250514-v1:0"
	BedrockSonnet = "anthropic.claude-sonnet-4-20250514-v1:0"
	BedrockHaiku  = "anthropic.claude-3-5-haiku-20241022-v1:0"
)

// Anthropic model IDs (same as Claude, different provider).
const (
	AnthropicOpus   = ClaudeOpus
	AnthropicSonnet = ClaudeSonnet
	AnthropicHaiku  = ClaudeHaiku
)
