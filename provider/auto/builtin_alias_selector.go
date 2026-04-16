package auto

import (
	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/openai"
)

func resolveBuiltinAliasModels(providerType string) (builtinAliasModels, bool) {
	if models, ok := selectBuiltinAliasModelsFromCatalog(providerType); ok {
		return models, true
	}
	models, ok := builtinAliasFallbacks[providerType]
	if !ok {
		return builtinAliasModels{}, false
	}
	return models, true
}

func selectBuiltinAliasModelsFromCatalog(providerType string) (builtinAliasModels, bool) {
	serviceID, ok := builtinAliasServiceID(providerType)
	if !ok {
		return builtinAliasModels{}, false
	}
	catalogSnapshot, ok := autoCatalog()
	if !ok {
		return builtinAliasModels{}, false
	}
	models := llm.CatalogModelsForService(catalogSnapshot, serviceID, llm.CatalogModelProjectionOptions{
		ProviderName:         providerType,
		ExcludeIntentAliases: true,
	})
	if len(models) == 0 {
		return builtinAliasModels{}, false
	}
	switch providerType {
	case ProviderClaude, ProviderAnthropic:
		return selectAnthropicBuiltinAliases(models)
	case ProviderOpenAI:
		return selectOpenAIBuiltinAliases(models)
	case ProviderChatGPT:
		return selectChatGPTBuiltinAliases(models)
	default:
		return builtinAliasModels{}, false
	}
}

func builtinAliasServiceID(providerType string) (string, bool) {
	switch providerType {
	case ProviderClaude, ProviderAnthropic:
		return "anthropic", true
	case ProviderOpenAI, ProviderChatGPT:
		return "openai", true
	default:
		return "", false
	}
}

func selectAnthropicBuiltinAliases(models llm.Models) (builtinAliasModels, bool) {
	fast, ok := firstPresent(models, anthropic.ModelHaiku)
	if !ok {
		return builtinAliasModels{}, false
	}
	normal, ok := firstPresent(models, anthropic.ModelSonnet)
	if !ok {
		return builtinAliasModels{}, false
	}
	powerful, ok := firstPresent(models, anthropic.ModelOpus)
	if !ok {
		return builtinAliasModels{}, false
	}
	return builtinAliasModels{fast: fast, normal: normal, powerful: powerful}, true
}

func selectOpenAIBuiltinAliases(models llm.Models) (builtinAliasModels, bool) {
	fast, ok := firstPresent(models,
		openai.ModelGPT54Mini,
		openai.ModelGPT5Mini,
		openai.ModelGPT4oMini,
		openai.ModelGPT41Mini,
		openai.ModelGPT54Nano,
		openai.ModelGPT5Nano,
	)
	if !ok {
		return builtinAliasModels{}, false
	}
	normal, ok := firstPresent(models,
		openai.ModelGPT54,
		openai.ModelGPT5,
		openai.ModelGPT4o,
		openai.ModelGPT41,
	)
	if !ok {
		return builtinAliasModels{}, false
	}
	powerful, ok := firstPresent(models,
		openai.ModelGPT54Pro,
		openai.ModelGPT5Pro,
		openai.ModelO3,
		openai.ModelO3Pro,
		openai.ModelGPT52Pro,
	)
	if !ok {
		return builtinAliasModels{}, false
	}
	return builtinAliasModels{fast: fast, normal: normal, powerful: powerful}, true
}

func selectChatGPTBuiltinAliases(models llm.Models) (builtinAliasModels, bool) {
	fast, ok := firstPresent(models, openai.ModelGPT51CodexMini)
	if !ok {
		return builtinAliasModels{}, false
	}
	normal, ok := firstPresent(models,
		openai.ModelGPT53Codex,
		openai.ModelGPT51Codex,
	)
	if !ok {
		return builtinAliasModels{}, false
	}
	powerful, ok := firstPresent(models,
		openai.ModelGPT53Codex,
		openai.ModelGPT51CodexMax,
	)
	if !ok {
		return builtinAliasModels{}, false
	}
	return builtinAliasModels{fast: fast, normal: normal, powerful: powerful}, true
}

func firstPresent(models llm.Models, preferred ...string) (string, bool) {
	for _, id := range preferred {
		if _, ok := models.ByID(id); ok {
			return id, true
		}
	}
	return "", false
}
