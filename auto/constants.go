// Package auto provides zero-config service construction on top of llm.Service.
package auto

// Provider type names.
const (
	ProviderClaude     = "claude"
	ProviderBedrock    = "bedrock"
	ProviderCodex      = "codex"
	ProviderOpenAI     = "openai"
	ProviderOpenRouter = "openrouter"
	ProviderAnthropic  = "anthropic"
	ProviderMiniMax    = "minimax"
	ProviderOllama     = "ollama"
	ProviderDockerMR   = "dockermr"
)

// Environment variable names.
const (
	EnvOpenAIKey     = "OPENAI_API_KEY"
	EnvOpenAIKeyAlt  = "OPENAI_KEY"
	EnvOpenRouterKey = "OPENROUTER_API_KEY"
	EnvAnthropicKey  = "ANTHROPIC_API_KEY"
	EnvMiniMaxKey    = "MINIMAX_API_KEY"
)

// Built-in top-level aliases provided by provider/auto.
const (
	AliasFast     = "fast"
	AliasDefault  = "default"
	AliasPowerful = "powerful"
)

// Common provider/user alias names.
const (
	AliasCodex = "codex"
)
