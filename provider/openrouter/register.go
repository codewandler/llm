package openrouter

import (
	"os"

	"github.com/codewandler/llm"
)

// Environment variable names for OpenRouter configuration.
const (
	EnvOpenRouterAPIKey = "OPENROUTER_API_KEY"
)

// MaybeRegister registers the OpenRouter provider if an API key is available.
// Checks OPENROUTER_API_KEY environment variable.
func MaybeRegister(reg *llm.Registry) {
	if os.Getenv(EnvOpenRouterAPIKey) == "" {
		return
	}

	reg.Register(New(llm.APIKeyFromEnv(EnvOpenRouterAPIKey)))
}
