package openai

import (
	"os"

	"github.com/codewandler/llm"
)

// Environment variable names for OpenAI configuration.
const (
	EnvOpenAIAPIKey = "OPENAI_API_KEY"
	EnvOpenAIKey    = "OPENAI_KEY" // Alternative env var name
)

// MaybeRegister registers the OpenAI provider if an API key is available.
// Checks OPENAI_API_KEY and OPENAI_KEY environment variables.
func MaybeRegister(reg *llm.Registry) {
	if os.Getenv(EnvOpenAIAPIKey) == "" && os.Getenv(EnvOpenAIKey) == "" {
		return
	}

	reg.Register(New())
}
