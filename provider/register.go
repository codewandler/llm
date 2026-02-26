package provider

import (
	"context"
	"os"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/ollama"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/openrouter"
)

// NewDefaultRegistry creates a registry with all available providers pre-registered.
// Providers are configured using environment variables:
//   - OPENAI_KEY for OpenAI
//   - OPENROUTER_API_KEY for OpenRouter
//   - ANTHROPIC_API_KEY for Anthropic
//   - OLLAMA_BASE_URL for Ollama (optional, defaults to http://localhost:11434)
//
// Claude Code provider is always registered (uses local claude CLI).
func NewDefaultRegistry() *llm.Registry {
	reg := llm.NewRegistry()

	// Register Claude Code provider (uses local claude CLI)
	reg.Register(anthropic.NewClaudeCodeProvider())

	// Register Ollama provider (no API key needed, custom base URL optional)
	var ollamaOpts []llm.Option
	if ollamaURL := os.Getenv("OLLAMA_BASE_URL"); ollamaURL != "" {
		ollamaOpts = append(ollamaOpts, llm.WithBaseURL(ollamaURL))
	}
	reg.Register(ollama.New(ollamaOpts...))

	// Register OpenAI provider if API key is available
	if os.Getenv("OPENAI_KEY") != "" {
		reg.Register(openai.New(llm.APIKeyFromEnv("OPENAI_KEY")))
	}

	// Register OpenRouter provider if API key is available
	if os.Getenv("OPENROUTER_API_KEY") != "" {
		reg.Register(openrouter.New(llm.APIKeyFromEnv("OPENROUTER_API_KEY")))
	}

	// Register Anthropic provider if API key is available
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		reg.Register(anthropic.New(llm.APIKeyFromEnv("ANTHROPIC_API_KEY")))
	}

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
