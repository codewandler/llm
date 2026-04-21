//go:build integration

package integration

import (
	"fmt"
	"os"
	"strings"

	"github.com/codewandler/llm"
	modelcatalog "github.com/codewandler/llm/internal/modelcatalog"
	"github.com/codewandler/llm/provider/anthropic/claude"
	openaiprovider "github.com/codewandler/llm/provider/openai"
	openrouterprovider "github.com/codewandler/llm/provider/openrouter"
	modeldb "github.com/codewandler/modeldb"
)

type targetExpectation struct {
	ServiceID    string
	ProviderName string
	WireModel    string
	APIType      llm.ApiType
}

type cacheWireKind string

const (
	cacheWireNone            cacheWireKind = ""
	cacheWirePromptRetention cacheWireKind = "prompt_cache_retention"
	cacheWireTopLevelControl cacheWireKind = "top_level_cache_control"
	cacheWireBlockControl    cacheWireKind = "block_cache_control"
)

type cachingContract struct {
	Available                    bool
	Configurable                 bool
	Mode                         string
	ImplicitOnly                 bool
	RequestLevelCaching          bool
	MessageLevelCaching          bool
	RequestLevelWireKind         cacheWireKind
	MessageLevelWireKind         cacheWireKind
	MessageOverridesRequest      bool
	SuppressesRequestLevelMarker bool
	Source                       string
	Note                         string
}

func (c cachingContract) Summary() string {
	if !c.Available {
		return "none"
	}
	parts := []string{}
	if c.Mode != "" {
		parts = append(parts, "mode="+c.Mode)
	}
	if c.Configurable {
		parts = append(parts, "configurable=yes")
	}
	if c.RequestLevelCaching && c.RequestLevelWireKind != "" {
		parts = append(parts, "request="+string(c.RequestLevelWireKind))
	}
	if c.MessageLevelCaching && c.MessageLevelWireKind != "" {
		parts = append(parts, "message="+string(c.MessageLevelWireKind))
	}
	if c.MessageOverridesRequest {
		parts = append(parts, "precedence=yes")
	}
	if c.SuppressesRequestLevelMarker {
		parts = append(parts, "suppress_request=yes")
	}
	if c.ImplicitOnly {
		parts = append(parts, "mode=implicit")
	}
	if c.Source != "" {
		parts = append(parts, "source="+c.Source)
	}
	if c.Note != "" {
		parts = append(parts, "note="+c.Note)
	}
	if len(parts) == 0 {
		return "available"
	}
	return strings.Join(parts, ",")
}

type targetCapabilities struct {
	Reasoning      bool
	Effort         bool
	ThinkingToggle bool
	Caching        cachingContract
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
	caps := targetCapabilities{Caching: providerCachingContract(serviceID, model, apiType)}
	cat, err := modelcatalog.LoadMergedBuiltIn()
	if err != nil {
		return caps
	}
	wireModelID := stripProviderPrefix(model)
	if serviceID == "openrouter" {
		wireModelID = strings.TrimPrefix(model, "openrouter/")
	}
	offering, ok := cat.OfferingByRef(modeldb.OfferingRef{ServiceID: serviceID, WireModelID: wireModelID})
	if !ok {
		return caps
	}
	exposure := offering.Exposure(modelDBAPIType(apiType))
	if exposure == nil || exposure.ExposedCapabilities == nil || exposure.ExposedCapabilities.Reasoning == nil {
		return caps
	}
	r := exposure.ExposedCapabilities.Reasoning
	caps.Reasoning = r.Available
	caps.Effort = exposure.SupportsParameter(modeldb.ParamReasoningEffort)
	caps.ThinkingToggle = containsMode(r.Modes, modeldb.ReasoningModeOff) || exposure.SupportsParameterValue(string(modeldb.ParamReasoningEffort), string(modeldb.ReasoningEffortNone))
	return caps
}

func supportsPromptCaching(serviceID, model string) bool {
	wireModelID := stripProviderPrefix(model)
	switch serviceID {
	case "openai", "codex":
		return openaiprovider.SupportsPromptCaching(wireModelID)
	case "openrouter":
		return openrouterprovider.SupportsPromptCaching(strings.TrimPrefix(model, "openrouter/"))
	default:
		return false
	}
}

func providerCachingContract(serviceID, model string, apiType llm.ApiType) cachingContract {
	if c, ok := cachingContractFromCatalog(serviceID, model, apiType); ok {
		return c
	}
	return fallbackCachingContract(serviceID, model, apiType)
}

func cachingContractFromCatalog(serviceID, model string, apiType llm.ApiType) (cachingContract, bool) {
	cat, err := modelcatalog.LoadMergedBuiltIn()
	if err != nil {
		return cachingContract{}, false
	}
	wireModelID := stripProviderPrefix(model)
	if serviceID == "openrouter" {
		wireModelID = strings.TrimPrefix(model, "openrouter/")
	}
	offering, ok := cat.OfferingByRef(modeldb.OfferingRef{ServiceID: serviceID, WireModelID: wireModelID})
	if !ok {
		return cachingContract{}, false
	}
	exposure := offering.Exposure(modelDBAPIType(apiType))
	if exposure == nil {
		return cachingContract{}, false
	}
	c, ok := cachingContractFromExposure(exposure)
	if !ok {
		return cachingContract{}, false
	}
	c.Source = "modeldb"
	return c, true
}

func cachingContractFromExposure(exposure *modeldb.OfferingExposure) (cachingContract, bool) {
	if exposure == nil {
		return cachingContract{}, false
	}
	var c cachingContract
	if exposure.ExposedCapabilities != nil && exposure.ExposedCapabilities.Caching != nil {
		cache := exposure.ExposedCapabilities.Caching
		if cache.Available || cache.PromptCacheRetention || cache.TopLevelRequestCaching || cache.PerMessageCaching {
			c.Available = true
		}
		if cache.Configurable {
			c.Configurable = true
		}
		if cache.Mode != "" {
			c.Mode = string(cache.Mode)
		}
		if cache.Mode == modeldb.CachingModeImplicit {
			c.ImplicitOnly = true
		}
		if cache.PromptCacheRetention {
			c.RequestLevelCaching = true
			c.RequestLevelWireKind = cacheWirePromptRetention
		}
		if cache.TopLevelRequestCaching {
			c.RequestLevelCaching = true
			if c.RequestLevelWireKind == cacheWireNone {
				c.RequestLevelWireKind = cacheWireTopLevelControl
			}
		}
		if cache.PerMessageCaching {
			c.MessageLevelCaching = true
			c.MessageLevelWireKind = cacheWireBlockControl
		}
		if cache.TopLevelRequestCaching && cache.PerMessageCaching {
			c.MessageOverridesRequest = true
			c.SuppressesRequestLevelMarker = true
		}
	}
	if exposure.SupportsParameter(modeldb.ParamPromptCacheRetention) {
		c.ImplicitOnly = false
		c.Available = true
		c.RequestLevelCaching = true
		c.RequestLevelWireKind = cacheWirePromptRetention
	}
	if exposure.SupportsParameter(modeldb.ParamTopLevelCacheControl) || exposure.SupportsParameter(modeldb.ParamCacheControl) {
		c.ImplicitOnly = false
		c.Available = true
		c.RequestLevelCaching = true
		if c.RequestLevelWireKind == cacheWireNone {
			c.RequestLevelWireKind = cacheWireTopLevelControl
		}
	}
	if exposure.SupportsParameter(modeldb.ParamBlockCacheControl) {
		c.ImplicitOnly = false
		c.Available = true
		c.MessageLevelCaching = true
		c.MessageLevelWireKind = cacheWireBlockControl
	}
	if c.RequestLevelCaching && c.MessageLevelCaching {
		c.MessageOverridesRequest = true
		c.SuppressesRequestLevelMarker = true
	}
	if c.RequestLevelWireKind == cacheWirePromptRetention && !exposure.SupportsParameter(modeldb.ParamBlockCacheControl) {
		c.MessageLevelCaching = false
		c.MessageLevelWireKind = cacheWireNone
		c.MessageOverridesRequest = false
		c.SuppressesRequestLevelMarker = false
	}
	if !c.Available {
		return cachingContract{}, false
	}
	return c, true
}

func fallbackCachingContract(serviceID, model string, apiType llm.ApiType) cachingContract {
	wireModelID := stripProviderPrefix(model)
	switch serviceID {
	case "openai":
		if openaiprovider.SupportsPromptCaching(wireModelID) {
			return cachingContract{Available: true, Configurable: true, Mode: string(modeldb.CachingModeExplicit), RequestLevelCaching: true, RequestLevelWireKind: cacheWirePromptRetention, Source: "fallback"}
		}
	case "codex":
		if openaiprovider.SupportsPromptCaching(wireModelID) {
			return cachingContract{Available: true, ImplicitOnly: true, Source: "fallback", Note: "implicit caching"}
		}
	case "openrouter":
		if openrouterprovider.SupportsPromptCaching(strings.TrimPrefix(model, "openrouter/")) {
			return cachingContract{Available: true, Configurable: true, Mode: string(modeldb.CachingModeExplicit), RequestLevelCaching: true, RequestLevelWireKind: cacheWirePromptRetention, Source: "fallback", Note: "openrouter heuristic"}
		}
	case "claude", "anthropic":
		return cachingContract{Available: true, Configurable: true, Mode: string(modeldb.CachingModeExplicit), RequestLevelCaching: true, MessageLevelCaching: true, RequestLevelWireKind: cacheWireTopLevelControl, MessageLevelWireKind: cacheWireBlockControl, MessageOverridesRequest: true, SuppressesRequestLevelMarker: true, Source: "fallback"}
	case "minimax":
		return cachingContract{Available: true, Configurable: true, Mode: string(modeldb.CachingModeExplicit), RequestLevelCaching: true, RequestLevelWireKind: cacheWireTopLevelControl, Source: "fallback"}
	}
	_ = apiType
	return cachingContract{}
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
	claudeModel := envOr("CLAUDE_MODEL", "claude/claude-sonnet-4-6")
	claudeCaps := providerCapabilities("claude", claudeModel, llm.ApiTypeAnthropicMessages)
	anthropicModel := envOr("ANTHROPIC_MODEL", "anthropic/claude-sonnet-4-6")
	minimaxModel := envOr("MINIMAX_MODEL", "minimax/MiniMax-M2.7")
	minimaxCaps := providerCapabilities("minimax", minimaxModel, llm.ApiTypeAnthropicMessages)
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
			model:     claudeModel,
			available: requireClaudeTokenProvider,
			expect:    targetExpectation{ServiceID: "claude", APIType: llm.ApiTypeAnthropicMessages},
			supports:  providerCapabilities("anthropic", anthropicModel, llm.ApiTypeAnthropicMessages),
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
			model:     anthropicModel,
			available: requireEnv("ANTHROPIC_API_KEY"),
			expect:    targetExpectation{ServiceID: "anthropic", APIType: llm.ApiTypeAnthropicMessages},
			supports:  claudeCaps,
		},
		{
			name:      "minimax_m27",
			model:     minimaxModel,
			available: requireEnv("MINIMAX_API_KEY"),
			expect:    targetExpectation{ServiceID: "minimax", APIType: llm.ApiTypeAnthropicMessages},
			supports:  minimaxCaps,
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
