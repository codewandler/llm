package anthropic

import (
	"sort"

	"github.com/codewandler/llm"
)

// Model ID constants for programmatic use.
const (
	// Claude 4.6 (current).
	ModelOpus   = "claude-opus-4-6"
	ModelSonnet = "claude-sonnet-4-6"

	// Claude 4.5 (Haiku latest).
	ModelHaiku = "claude-haiku-4-5-20251001"
)

// ModelAliases maps short alias names to full model IDs.
// These are used by the auto package for provider-prefixed resolution (e.g., "claude/sonnet").
var ModelAliases = map[string]string{
	"opus":   ModelOpus,
	"sonnet": ModelSonnet,
	"haiku":  ModelHaiku,
}

var fallbackModels = llm.Models{
	{ID: ModelSonnet, Name: "Claude Sonnet 4.6", Provider: providerName, Aliases: []string{llm.ModelDefault, llm.ModelFast, ModelAliases["sonnet"], "claude-sonnet-4-6"}},
	{ID: ModelOpus, Name: "Claude Opus 4.6", Provider: providerName, Aliases: []string{llm.ModelPowerful, ModelAliases["opus"], "claude-opus-4-6"}},
	{ID: ModelHaiku, Name: "Claude Haiku 4.5", Provider: providerName, Aliases: []string{ModelAliases["haiku"], "claude-haiku-4-5-20251001"}},
	{ID: "claude-sonnet-4-5-20250929", Name: "Claude Sonnet 4.5", Provider: providerName},
	{ID: "claude-opus-4-5", Name: "Claude Opus 4.5", Provider: providerName},
	{ID: "claude-opus-4-5-20251101", Name: "Claude Opus 4.5", Provider: providerName},
	{ID: "claude-haiku-4-5", Name: "Claude Haiku 4.5", Provider: providerName},
	{ID: "claude-opus-4-1", Name: "Claude Opus 4.1", Provider: providerName},
	{ID: "claude-opus-4-1-20250805", Name: "Claude Opus 4.1", Provider: providerName},
	{ID: "claude-opus-4", Name: "Claude Opus 4.0", Provider: providerName},
	{ID: "claude-opus-4-20250514", Name: "Claude Opus 4.0", Provider: providerName},
	{ID: "claude-sonnet-4", Name: "Claude Sonnet 4.0", Provider: providerName},
	{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4.0", Provider: providerName},
}

var fallbackModelsByID = func() map[string]llm.Model {
	models := make(map[string]llm.Model, len(fallbackModels))
	for _, model := range fallbackModels {
		models[model.ID] = model
	}
	return models
}()

var allModelsWithAliases = loadAnthropicModels()

func loadAnthropicModels() llm.Models {
	catalogSnapshot, err := llm.LoadBuiltInCatalog()
	if err != nil {
		return fallbackModels
	}

	models := llm.CatalogModelsForService(catalogSnapshot, providerName, llm.CatalogModelProjectionOptions{
		ProviderName:         providerName,
		ExcludeIntentAliases: true,
	})
	if len(models) == 0 {
		return fallbackModels
	}

	remaining := make(map[string]llm.Model, len(models))
	for _, model := range models {
		if fallback, ok := fallbackModelsByID[model.ID]; ok {
			if model.Name == "" || model.Name == model.ID {
				model.Name = fallback.Name
			}
			model.Aliases = mergeAnthropicAliases(fallback.Aliases, model.Aliases)
		}
		remaining[model.ID] = model
	}

	out := make(llm.Models, 0, len(remaining))
	for _, fallback := range fallbackModels {
		if model, ok := remaining[fallback.ID]; ok {
			out = append(out, model)
			delete(remaining, fallback.ID)
			continue
		}
		out = append(out, fallback)
	}

	ids := make([]string, 0, len(remaining))
	for id := range remaining {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out = append(out, remaining[id])
	}
	return out
}

func mergeAnthropicAliases(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, values := range [][]string{a, b} {
		for _, value := range values {
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}
