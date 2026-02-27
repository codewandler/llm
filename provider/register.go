package provider

import (
	"context"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/ollama"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/openrouter"
)

// NewDefaultRegistry creates a registry with all available providers pre-registered.
// Each provider checks its own environment variables and registers itself if configured.
//
// Provider configuration:
//   - Anthropic: ANTHROPIC_API_KEY, or claude CLI in PATH for Claude Code
//   - OpenAI: OPENAI_API_KEY or OPENAI_KEY
//   - OpenRouter: OPENROUTER_API_KEY
//   - Bedrock: AWS_ACCESS_KEY_ID or ~/.aws/credentials, AWS_REGION
//   - Ollama: Always registered, OLLAMA_BASE_URL (optional)
func NewDefaultRegistry() *llm.Registry {
	reg := llm.NewRegistry()
	reg.RegisterAll(
		anthropic.MaybeRegister,
		ollama.MaybeRegister,
		openai.MaybeRegister,
		openrouter.MaybeRegister,
		bedrock.MaybeRegister,
	)
	return reg
}

var defaultRegistry = NewDefaultRegistry()

func Provider(name string) (llm.Provider, error) {
	return defaultRegistry.Provider(name)
}

func CreateStream(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	return defaultRegistry.CreateStream(ctx, opts)
}

func AllModels() []llm.Model {
	return defaultRegistry.AllModels()
}
