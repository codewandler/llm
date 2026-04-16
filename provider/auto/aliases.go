package auto

import (
	"sync"

	"github.com/codewandler/llm/catalog"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/minimax"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/router"
)

// aliasModels defines which model to use for each global alias per provider.
type aliasModels struct {
	fast     string
	normal   string
	powerful string
	codex    string // optional: the codex model to use for the AliasCodex global alias
}

// providerAliasModels maps provider types to their alias model mappings.
// These are used for the built-in global aliases (fast, default, powerful, codex).
var providerAliasModels = map[string]aliasModels{
	ProviderClaude: {
		fast:     anthropic.ModelHaiku,
		normal:   anthropic.ModelSonnet,
		powerful: anthropic.ModelOpus,
	},
	ProviderAnthropic: {
		fast:     anthropic.ModelHaiku,
		normal:   anthropic.ModelSonnet,
		powerful: anthropic.ModelOpus,
	},
	ProviderBedrock: {
		fast:     bedrock.ModelHaikuLatest,
		normal:   bedrock.ModelSonnetLatest,
		powerful: bedrock.ModelOpusLatest,
	},
	ProviderOpenAI: {
		fast:     openai.ModelGPT4oMini,
		normal:   openai.ModelGPT4o,
		powerful: openai.ModelO3,
	},
	ProviderMiniMax: {
		fast:     minimax.ModelM27,
		normal:   minimax.ModelM27,
		powerful: minimax.ModelM27,
	},
	// ChatGPT / Codex: accessed via chatgpt.com/backend-api using Codex CLI OAuth.
	// All models are Codex-category; no general-purpose GPT aliases here.
	ProviderChatGPT: {
		fast:     openai.ModelGPT51CodexMini,
		normal:   openai.ModelGPT53Codex,
		powerful: openai.ModelGPT53Codex,
		codex:    openai.ModelGPT53Codex,
	},
}

var (
	builtinCatalogOnce sync.Once
	builtinCatalog     catalog.Catalog
	builtinCatalogErr  error
)

// buildAliasTargets creates alias targets for a provider instance.
func buildAliasTargets(instanceName, providerType string) map[string]router.AliasTarget {
	models, ok := providerAliasModels[providerType]
	if !ok {
		return nil
	}

	targets := map[string]router.AliasTarget{
		AliasFast:     {Provider: instanceName, Model: models.fast},
		AliasDefault:  {Provider: instanceName, Model: models.normal},
		AliasPowerful: {Provider: instanceName, Model: models.powerful},
	}
	// Wire the global "codex" alias for providers that designate a codex model.
	if models.codex != "" {
		targets[AliasCodex] = router.AliasTarget{Provider: instanceName, Model: models.codex}
	}
	return targets
}

// modelAliasesForProvider returns the local model aliases for a provider type.
// Catalog-backed factual aliases are preferred when available. Provider-local
// fallback registries remain in place for providers not yet modeled in catalog.
func modelAliasesForProvider(providerType string) map[string]string {
	if c, ok := autoCatalog(); ok {
		if aliases := modelAliasesFromCatalog(c, providerType); len(aliases) > 0 {
			return aliases
		}
	}

	switch providerType {
	case ProviderClaude, ProviderAnthropic:
		return anthropic.ModelAliases
	case ProviderOpenAI:
		return openai.ModelAliases
	case ProviderOpenRouter:
		return nil
	case ProviderChatGPT:
		return openai.CodexModelAliases
	case ProviderBedrock:
		return bedrock.ModelAliases
	case ProviderMiniMax:
		return minimax.ModelAliases
	default:
		return nil
	}
}

func autoCatalog() (catalog.Catalog, bool) {
	builtinCatalogOnce.Do(func() {
		builtinCatalog, builtinCatalogErr = catalog.LoadBuiltIn()
	})
	return builtinCatalog, builtinCatalogErr == nil
}

func modelAliasesFromCatalog(c catalog.Catalog, providerType string) map[string]string {
	serviceID, ok := catalogServiceID(providerType)
	if !ok {
		return nil
	}
	return c.FactualAliasesForService(serviceID)
}

func catalogServiceID(providerType string) (string, bool) {
	switch providerType {
	case ProviderClaude, ProviderAnthropic:
		return "anthropic", true
	case ProviderOpenAI:
		return "openai", true
	case ProviderOpenRouter:
		return "openrouter", true
	default:
		return "", false
	}
}
