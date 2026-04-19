//go:build integration

package integration

import (
	"fmt"
	"os"
	"strings"

	"github.com/codewandler/llm"
	modelcatalog "github.com/codewandler/llm/internal/modelcatalog"
	"github.com/codewandler/llm/provider/anthropic/claude"
	modeldb "github.com/codewandler/modeldb"
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

func providerCapabilities(serviceID, model string, apiType llm.ApiType) targetCapabilities {
	cat, err := modelcatalog.LoadMergedBuiltIn()
	if err != nil {
		return targetCapabilities{}
	}
	wireModelID := stripProviderPrefix(model)
	if serviceID == "openrouter" {
		wireModelID = strings.TrimPrefix(model, "openrouter/")
	}
	offering, ok := cat.OfferingByRef(modeldb.OfferingRef{ServiceID: serviceID, WireModelID: wireModelID})
	if !ok {
		return targetCapabilities{}
	}
	exposure := offering.Exposure(modelDBAPIType(apiType))
	if exposure == nil || exposure.ExposedCapabilities == nil || exposure.ExposedCapabilities.Reasoning == nil {
		return targetCapabilities{}
	}
	r := exposure.ExposedCapabilities.Reasoning
	caps := targetCapabilities{Reasoning: r.Available}
	caps.Effort = exposure.SupportsParameter(modeldb.ParamReasoningEffort)
	caps.ThinkingToggle = containsMode(r.Modes, modeldb.ReasoningModeOff) || exposure.SupportsParameterValue(string(modeldb.ParamReasoningEffort), string(modeldb.ReasoningEffortNone))
	return caps
}

func containsMode(modes []modeldb.ReasoningMode, want modeldb.ReasoningMode) bool {
	for _, mode := range modes {
		if mode == want {
			return true
		}
	}
	return false
}

func modelDBAPIType(apiType llm.ApiType) modeldb.APIType {
	switch apiType {
	case llm.ApiTypeOpenAIResponses:
		return modeldb.APITypeOpenAIResponses
	case llm.ApiTypeOpenAIChatCompletion:
		return modeldb.APITypeOpenAIChat
	case llm.ApiTypeAnthropicMessages:
		return modeldb.APITypeAnthropicMessages
	default:
		return modeldb.APITypeDefault
	}
}

func stripProviderPrefix(model string) string {
	for _, prefix := range []string{"openai/", "openrouter/", "codex/"} {
		if strings.HasPrefix(model, prefix) {
			return strings.TrimPrefix(model, prefix)
		}
	}
	return model
}

func integrationTargets() []integrationTarget {
	openrouterModel := envOr("OPENROUTER_MODEL", "openrouter/openai/gpt-4o-mini")
	openrouterCaps := providerCapabilities("openrouter", openrouterModel, llm.ApiTypeOpenAIResponses)
	openaiModel := envOr("OPENAI_MODEL", "openai/gpt-4o")
	openaiCaps := providerCapabilities("openai", openaiModel, llm.ApiTypeOpenAIChatCompletion)
	codexModel := envOr("CODEX_MODEL", "codex/gpt-5.4")
	codexCaps := providerCapabilities("codex", codexModel, llm.ApiTypeOpenAIResponses)
	return []integrationTarget{
		{
			name:      "openrouter_openai_gpt4o_mini",
			model:     openrouterModel,
			available: requireEnv("OPENROUTER_API_KEY"),
			expect:    targetExpectation{ServiceID: "openrouter", APIType: llm.ApiTypeOpenAIResponses},
			supports:  openrouterCaps,
		},
		{
			name:      "openrouter_openai_gpt51",
			model:     "openrouter/openai/gpt-5.1",
			available: requireEnv("OPENROUTER_API_KEY"),
			expect:    targetExpectation{ServiceID: "openrouter", APIType: llm.ApiTypeOpenAIResponses},
			supports:  providerCapabilities("openrouter", "openrouter/openai/gpt-5.1", llm.ApiTypeOpenAIResponses),
		},
		{
			name:      "openrouter_openai_gpt54",
			model:     "openrouter/openai/gpt-5.4",
			available: requireEnv("OPENROUTER_API_KEY"),
			expect:    targetExpectation{ServiceID: "openrouter", APIType: llm.ApiTypeOpenAIResponses},
			supports:  providerCapabilities("openrouter", "openrouter/openai/gpt-5.4", llm.ApiTypeOpenAIResponses),
		},
		{
			name:      "claude_sonnet",
			model:     envOr("CLAUDE_MODEL", "claude/claude-sonnet-4-6"),
			available: requireClaudeTokenProvider,
			expect:    targetExpectation{ServiceID: "claude", APIType: llm.ApiTypeAnthropicMessages},
			supports:  targetCapabilities{Reasoning: true, Effort: true, ThinkingToggle: true},
		},
		{
			name:      "openai_gpt4o",
			model:     openaiModel,
			available: requireAnyEnv("OPENAI_API_KEY", "OPENAI_KEY"),
			expect:    targetExpectation{ServiceID: "openai", APIType: llm.ApiTypeOpenAIChatCompletion},
			supports:  openaiCaps,
			prepareRequest: func(req llm.Request) llm.Request {
				if req.MaxTokens < 1024 {
					req.MaxTokens = 1024
				}
				return req
			},
		},
		{
			name:      "openai_gpt51",
			model:     "openai/gpt-5.1",
			available: requireAnyEnv("OPENAI_API_KEY", "OPENAI_KEY"),
			expect:    targetExpectation{ServiceID: "openai", APIType: llm.ApiTypeOpenAIResponses},
			supports:  providerCapabilities("openai", "openai/gpt-5.1", llm.ApiTypeOpenAIResponses),
		},
		{
			name:      "openai_gpt54",
			model:     "openai/gpt-5.4",
			available: requireAnyEnv("OPENAI_API_KEY", "OPENAI_KEY"),
			expect:    targetExpectation{ServiceID: "openai", APIType: llm.ApiTypeOpenAIResponses},
			supports:  providerCapabilities("openai", "openai/gpt-5.4", llm.ApiTypeOpenAIResponses),
		},
		{
			name:      "anthropic_api_sonnet",
			model:     envOr("ANTHROPIC_MODEL", "anthropic/claude-sonnet-4-6"),
			available: requireEnv("ANTHROPIC_API_KEY"),
			expect:    targetExpectation{ServiceID: "anthropic", APIType: llm.ApiTypeAnthropicMessages},
			supports:  targetCapabilities{Reasoning: true, Effort: true, ThinkingToggle: true},
		},
		{
			name:      "minimax_m27",
			model:     envOr("MINIMAX_MODEL", "minimax/MiniMax-M2.7"),
			available: requireEnv("MINIMAX_API_KEY"),
			expect:    targetExpectation{ServiceID: "minimax", APIType: llm.ApiTypeAnthropicMessages},
			supports:  targetCapabilities{Reasoning: true, Effort: false, ThinkingToggle: false},
			prepareRequest: func(req llm.Request) llm.Request {
				if req.MaxTokens < 4096 {
					req.MaxTokens = 4096
				}
				return req
			},
		},
		{
			name:      "codex_gpt54",
			model:     codexModel,
			available: requireCodexAuth,
			expect:    targetExpectation{ServiceID: "codex", APIType: llm.ApiTypeOpenAIResponses},
			supports:  codexCaps,
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
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false, "Codex local auth not available"
	}
	if _, err := os.Stat(home + "/.codex/auth.json"); err != nil {
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
