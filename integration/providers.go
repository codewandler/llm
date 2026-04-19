//go:build integration

package integration

import (
	"fmt"
	"os"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/codex"
)

type targetExpectation struct {
	ServiceID    string
	ProviderName string
	WireModel    string
	APIType      llm.ApiType
}

type targetCapabilities struct {
	Reasoning      bool
	Effort         bool
	ThinkingToggle bool
}

type integrationTarget struct {
	name           string
	model          string
	available      func() (bool, string)
	expect         targetExpectation
	supports       targetCapabilities
	prepareRequest func(req llm.Request) llm.Request
}

func integrationTargets() []integrationTarget {
	return []integrationTarget{
		{
			name:      "openrouter_openai_mini",
			model:     envOr("OPENROUTER_MODEL", "openrouter/openai/gpt-4o-mini"),
			available: requireEnv("OPENROUTER_API_KEY"),
			expect: targetExpectation{
				ServiceID: "openrouter",
				APIType:   llm.ApiTypeOpenAIResponses,
			},
		},
		{
			name:      "claude_sonnet",
			model:     envOr("CLAUDE_MODEL", "claude/claude-sonnet-4-6"),
			available: requireClaudeTokenProvider,
			expect: targetExpectation{
				ServiceID: "claude",
				APIType:   llm.ApiTypeAnthropicMessages,
			},
			supports: targetCapabilities{Reasoning: true, Effort: true, ThinkingToggle: true},
		},
		{
			name:      "openai_default",
			model:     envOr("OPENAI_MODEL", "openai/gpt-4o"),
			available: requireAnyEnv("OPENAI_API_KEY", "OPENAI_KEY"),
			expect: targetExpectation{
				ServiceID: "openai",
				// gpt-4o currently goes through chat completions; newer responses-only
				// models should use a dedicated target instead of overloading this one.
				APIType: llm.ApiTypeOpenAIChatCompletion,
			},
			prepareRequest: func(req llm.Request) llm.Request {
				if req.MaxTokens < 1024 {
					req.MaxTokens = 1024
				}
				return req
			},
		},
		{
			name:      "anthropic_api_sonnet",
			model:     envOr("ANTHROPIC_MODEL", "anthropic/claude-sonnet-4-6"),
			available: requireEnv("ANTHROPIC_API_KEY"),
			expect: targetExpectation{
				ServiceID: "anthropic",
				APIType:   llm.ApiTypeAnthropicMessages,
			},
			supports: targetCapabilities{Reasoning: true, Effort: true, ThinkingToggle: true},
		},
		{
			name:      "minimax_m27",
			model:     envOr("MINIMAX_MODEL", "minimax/MiniMax-M2.7"),
			available: requireEnv("MINIMAX_API_KEY"),
			expect: targetExpectation{
				ServiceID: "minimax",
				APIType:   llm.ApiTypeAnthropicMessages,
			},
			supports: targetCapabilities{Reasoning: true, Effort: false, ThinkingToggle: false},
			prepareRequest: func(req llm.Request) llm.Request {
				if req.MaxTokens < 4096 {
					req.MaxTokens = 4096
				}
				return req
			},
		},
		{
			name:      "codex_default",
			model:     envOr("CODEX_MODEL", "codex/gpt-5.4"),
			available: requireCodexAuth,
			expect: targetExpectation{
				ServiceID: "codex",
				APIType:   llm.ApiTypeOpenAIResponses,
			},
			supports: targetCapabilities{Effort: true},
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
			return false, "set " + name + " to run integration scenarios"
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

func expectSummary(expect targetExpectation) string {
	parts := []string{}
	if expect.ServiceID != "" {
		parts = append(parts, fmt.Sprintf("service=%s", expect.ServiceID))
	}
	if expect.ProviderName != "" {
		parts = append(parts, fmt.Sprintf("provider=%s", expect.ProviderName))
	}
	if expect.WireModel != "" {
		parts = append(parts, fmt.Sprintf("wire_model=%s", expect.WireModel))
	}
	if expect.APIType != "" {
		parts = append(parts, fmt.Sprintf("api=%s", expect.APIType))
	}
	return strings.Join(parts, ", ")
}
