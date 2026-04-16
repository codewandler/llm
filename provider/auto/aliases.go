package auto

import (
	"sync"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/codex"
	"github.com/codewandler/llm/provider/minimax"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/router"
)

// builtinAliasModels defines which model to use for each built-in top-level alias per provider.
type builtinAliasModels struct {
	fast     string
	normal   string
	powerful string
}

// builtinAliasFallbacks maps provider types to fallback model mappings used for
// built-in top-level aliases (fast, default, powerful).
var builtinAliasFallbacks = map[string]builtinAliasModels{
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
	ProviderCodex: func() builtinAliasModels {
		fast, normal, powerful := codex.BuiltinAliasModels()
		return builtinAliasModels{fast: fast, normal: normal, powerful: powerful}
	}(),
}

var (
	builtinCatalogOnce sync.Once
	builtinCatalog     llm.CatalogSnapshot
	builtinCatalogErr  error
)

// buildBuiltinAliasTargets creates the built-in top-level alias targets for a provider instance.
func buildBuiltinAliasTargets(instanceName, providerType string) map[string]router.AliasTarget {
	models, ok := resolveBuiltinAliasModels(providerType)
	if !ok {
		return nil
	}

	return map[string]router.AliasTarget{
		AliasFast:     {Provider: instanceName, Model: models.fast},
		AliasDefault:  {Provider: instanceName, Model: models.normal},
		AliasPowerful: {Provider: instanceName, Model: models.powerful},
	}
}

// modelAliasesForProvider returns provider-scoped aliases for a provider type.
//
// Factual aliases are loaded from the built-in catalog when a provider is
// modeled there. Provider-local fallback maps remain for two reasons:
//
//  1. Some providers are not yet catalog-backed for shorthand aliases.
//  2. Consumer policy aliases such as OpenAI's "flagship" or Codex's
//     Codex-only shorthands are intentionally not catalog truth.
//
// This function therefore merges catalog-backed factual aliases with provider
// policy/fallback aliases, preferring catalog entries when the same key exists.
func modelAliasesForProvider(providerType string) map[string]string {
	var aliases map[string]string
	if c, ok := autoCatalog(); ok {
		aliases = mergeAliasMaps(aliases, modelAliasesFromCatalog(c, providerType))
	}
	return mergeAliasMaps(aliases, fallbackModelAliasesForProvider(providerType))
}

func fallbackModelAliasesForProvider(providerType string) map[string]string {
	switch providerType {
	case ProviderClaude, ProviderAnthropic:
		return anthropic.ModelAliases
	case ProviderOpenAI:
		return openai.ModelAliases
	case ProviderOpenRouter:
		return nil
	case ProviderCodex:
		return codex.ModelAliases()
	case ProviderBedrock:
		return bedrock.ModelAliases
	case ProviderMiniMax:
		return minimax.ModelAliases
	default:
		return nil
	}
}

func autoCatalog() (llm.CatalogSnapshot, bool) {
	builtinCatalogOnce.Do(func() {
		builtinCatalog, builtinCatalogErr = llm.LoadBuiltInCatalog()
	})
	return builtinCatalog, builtinCatalogErr == nil
}

func modelAliasesFromCatalog(c llm.CatalogSnapshot, providerType string) map[string]string {
	serviceID, ok := catalogServiceID(providerType)
	if !ok {
		return nil
	}
	return llm.CatalogFactualAliasesForService(c, serviceID)
}

func mergeAliasMaps(base, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(extra))
	for _, aliases := range []map[string]string{base, extra} {
		for alias, target := range aliases {
			if alias == "" || target == "" {
				continue
			}
			if _, ok := out[alias]; ok {
				continue
			}
			out[alias] = target
		}
	}
	return out
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
