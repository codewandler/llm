package provider

import (
	"context"
	"os"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/ollama"
	"github.com/codewandler/llm/provider/openrouter"
)

// NewDefaultRegistry creates a registry with all available providers pre-registered.
// Providers are configured using environment variables:
//   - OPENROUTER_API_KEY for OpenRouter
//   - OLLAMA_BASE_URL for Ollama (optional, defaults to http://localhost:11434)
//
// Claude Code provider is always registered (uses local claude CLI).
func NewDefaultRegistry() *llm.Registry {
	reg := llm.NewRegistry()

	// Register Claude Code provider (uses local claude CLI)
	reg.Register(anthropic.NewClaudeCodeProvider())

	// Register Ollama provider
	ollamaURL := os.Getenv("OLLAMA_BASE_URL")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}
	reg.Register(ollama.New(ollamaURL))

	// Register OpenRouter provider if API key is available
	if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
		reg.Register(openrouter.New(apiKey))
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
