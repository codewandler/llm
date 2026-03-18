package anthropic

import (
	"os"

	"github.com/codewandler/llm"
)

// Environment variable names for Anthropic configuration.
const (
	EnvAnthropicAPIKey = "ANTHROPIC_API_KEY"
)

// MaybeRegister registers the Anthropic API provider if ANTHROPIC_API_KEY is set.
//
// Note: The Claude OAuth provider (provider/anthropic/claude) is not auto-registered.
// Use claude.New() with a TokenProvider to create a Claude OAuth provider.
func MaybeRegister(reg *llm.Registry) {
	// Register Anthropic API provider if API key is available
	if os.Getenv(EnvAnthropicAPIKey) != "" {
		reg.Register(New(llm.APIKeyFromEnv(EnvAnthropicAPIKey)))
	}
}
