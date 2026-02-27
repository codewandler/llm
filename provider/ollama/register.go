package ollama

import (
	"os"

	"github.com/codewandler/llm"
)

// Environment variable names for Ollama configuration.
const (
	EnvOllamaBaseURL = "OLLAMA_BASE_URL"
)

// MaybeRegister always registers the Ollama provider.
// Ollama runs locally and doesn't require authentication.
// If OLLAMA_BASE_URL is set, it will be used instead of the default localhost:11434.
func MaybeRegister(reg *llm.Registry) {
	var opts []llm.Option
	if baseURL := os.Getenv(EnvOllamaBaseURL); baseURL != "" {
		opts = append(opts, llm.WithBaseURL(baseURL))
	}

	reg.Register(New(opts...))
}
