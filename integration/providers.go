//go:build integration

package integration

import (
	"fmt"
	"os"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/codex"
	"github.com/codewandler/llm/provider/minimax"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/openrouter"
)

type matrixProvider struct {
	name              string
	model             string
	available         func() (bool, string)
	newProvider       func() (llm.Provider, error)
	expectedAPIType   func(req llm.Request) llm.ApiType
	supportsReasoning func(req llm.Request) bool
	prepareRequest    func(req llm.Request) llm.Request
}

func matrixProviders() []matrixProvider {
	return []matrixProvider{
		{
			name:      "openrouter",
			model:     envOr("OPENROUTER_MODEL", "openai/gpt-4o-mini"),
			available: requireEnv("OPENROUTER_API_KEY"),
			newProvider: func() (llm.Provider, error) {
				opts := []llm.Option{llm.WithAPIKey(os.Getenv("OPENROUTER_API_KEY"))}
				if baseURL := os.Getenv("OPENROUTER_BASE_URL"); baseURL != "" {
					opts = append(opts, llm.WithBaseURL(baseURL))
				}
				return openrouter.New(opts...), nil
			},
			expectedAPIType: func(req llm.Request) llm.ApiType {
				if req.ApiTypeHint == llm.ApiTypeAnthropicMessages || strings.HasPrefix(req.Model, "anthropic/") {
					return llm.ApiTypeAnthropicMessages
				}
				return llm.ApiTypeOpenAIResponses
			},
			supportsReasoning: func(req llm.Request) bool {
				return req.ApiTypeHint == llm.ApiTypeAnthropicMessages || strings.HasPrefix(req.Model, "anthropic/")
			},
		},
		{
			name:      "claude",
			model:     envOr("CLAUDE_MODEL", "sonnet"),
			available: requireClaudeTokenProvider,
			newProvider: func() (llm.Provider, error) {
				return claude.New(), nil
			},
			expectedAPIType: func(req llm.Request) llm.ApiType {
				return llm.ApiTypeAnthropicMessages
			},
			supportsReasoning: func(req llm.Request) bool {
				return true
			},
		},
		{
			name:      "openai",
			model:     envOr("OPENAI_MODEL", openai.DefaultModel),
			available: requireAnyEnv("OPENAI_API_KEY", "OPENAI_KEY"),
			newProvider: func() (llm.Provider, error) {
				var opts []llm.Option
				if baseURL := os.Getenv("OPENAI_BASE_URL"); baseURL != "" {
					opts = append(opts, llm.WithBaseURL(baseURL))
				}
				return openai.New(opts...), nil
			},
			expectedAPIType: func(req llm.Request) llm.ApiType {
				if openai.UseResponsesAPI(req.Model) {
					return llm.ApiTypeOpenAIResponses
				}
				return llm.ApiTypeOpenAIChatCompletion
			},
			supportsReasoning: func(req llm.Request) bool {
				return false
			},
			prepareRequest: func(req llm.Request) llm.Request {
				if req.MaxTokens < 1024 {
					req.MaxTokens = 1024
				}
				return req
			},
		},
		{
			name:      "anthropic",
			model:     envOr("ANTHROPIC_MODEL", "sonnet"),
			available: requireEnv("ANTHROPIC_API_KEY"),
			newProvider: func() (llm.Provider, error) {
				var opts []llm.Option
				if baseURL := os.Getenv("ANTHROPIC_BASE_URL"); baseURL != "" {
					opts = append(opts, llm.WithBaseURL(baseURL))
				}
				return anthropic.New(opts...), nil
			},
			expectedAPIType: func(req llm.Request) llm.ApiType {
				return llm.ApiTypeAnthropicMessages
			},
			supportsReasoning: func(req llm.Request) bool {
				return true
			},
		},
		{
			name:      "minimax",
			model:     envOr("MINIMAX_MODEL", minimax.ModelM27),
			available: requireEnv("MINIMAX_API_KEY"),
			newProvider: func() (llm.Provider, error) {
				var opts []llm.Option
				if baseURL := os.Getenv("MINIMAX_BASE_URL"); baseURL != "" {
					opts = append(opts, llm.WithBaseURL(baseURL))
				}
				return minimax.New(minimax.WithLLMOpts(opts...)), nil
			},
			expectedAPIType: func(req llm.Request) llm.ApiType {
				return llm.ApiTypeAnthropicMessages
			},
			supportsReasoning: func(req llm.Request) bool {
				return false
			},
		},
		{
			name:      "codex",
			model:     envOr("CODEX_MODEL", codex.DefaultModelID()),
			available: requireCodexAuth,
			newProvider: func() (llm.Provider, error) {
				auth, err := codex.LoadAuth()
				if err != nil {
					return nil, fmt.Errorf("load codex auth: %w", err)
				}
				var opts []llm.Option
				if baseURL := os.Getenv("CODEX_BASE_URL"); baseURL != "" {
					opts = append(opts, llm.WithBaseURL(baseURL))
				}
				return codex.New(auth, opts...), nil
			},
			expectedAPIType: func(req llm.Request) llm.ApiType {
				return llm.ApiTypeOpenAIResponses
			},
			supportsReasoning: func(req llm.Request) bool {
				return false
			},
		},
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func requireEnv(name string) func() (bool, string) {
	return func() (bool, string) {
		if os.Getenv(name) == "" {
			return false, "set " + name + " to run " + strings.ToLower(name) + " integration scenarios"
		}
		return true, ""
	}
}

func requireAnyEnv(names ...string) func() (bool, string) {
	return func() (bool, string) {
		for _, name := range names {
			if os.Getenv(name) != "" {
				return true, ""
			}
		}
		return false, "set one of " + strings.Join(names, ", ") + " to run integration scenarios"
	}
}

func requireClaudeTokenProvider() (bool, string) {
	if !claude.LocalTokenProviderAvailable() {
		return false, "Claude local token provider not available"
	}
	return true, ""
}

func requireCodexAuth() (bool, string) {
	if !codex.LocalAvailable() {
		return false, "Codex local auth not available"
	}
	return true, ""
}
