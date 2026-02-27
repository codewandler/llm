package anthropic

import (
	"os"
	"os/exec"

	"github.com/codewandler/llm"
)

// Environment variable names for Anthropic configuration.
const (
	EnvAnthropicAPIKey = "ANTHROPIC_API_KEY"
)

// MaybeRegister registers available Anthropic providers:
//   - Claude Code provider (via local claude CLI) if claude is in PATH
//   - Anthropic API provider if ANTHROPIC_API_KEY is set
func MaybeRegister(reg *llm.Registry) {
	// Register Claude Code provider if claude CLI is available
	if isClaudeAvailable() {
		reg.Register(NewClaudeCodeProvider())
	}

	// Register Anthropic API provider if API key is available
	if os.Getenv(EnvAnthropicAPIKey) != "" {
		reg.Register(New(llm.APIKeyFromEnv(EnvAnthropicAPIKey)))
	}
}

// isClaudeAvailable checks if the claude CLI is available in PATH.
func isClaudeAvailable() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}
