package anthropic

import (
	"os"

	"github.com/codewandler/llm"
)

// Environment variable names for Anthropic configuration.
const (
	EnvAnthropicAPIKey = "ANTHROPIC_API_KEY"
)

// MaybeRegister registers available Anthropic providers:
//   - Anthropic API provider if ANTHROPIC_API_KEY is set
//   - Claude Code profile provider if $HOME/.claude/.credentials.json exists
func MaybeRegister(reg *llm.Registry) {
	// Register Anthropic API provider if API key is available
	if os.Getenv(EnvAnthropicAPIKey) != "" {
		reg.Register(New(llm.APIKeyFromEnv(EnvAnthropicAPIKey)))
	}

	if path := defaultClaudeCredentialsPath(); path != "" {
		if _, err := os.Stat(path); err == nil {
			reg.Register(NewClaudeCodeProvider())
		}
	}
}
