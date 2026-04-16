package claude

import (
	"sort"

	"github.com/codewandler/llm"
)

const (
	modelNameSonnet = "sonnet"
	modelNameOpus   = "opus"
	modelNameHaiku  = "haiku"
)

const (
	ModelHaiku   = "claude-haiku-4-5-20251001"
	ModelSonnet  = "claude-sonnet-4-6"
	ModelOpus    = "claude-opus-4-6"
	ModelDefault = ModelHaiku
)

// preferredModels is the ordered, canonical list of curated Claude models.
// The first entry determines which model is used when CreateStream receives an
// empty model ID — the substitution happens in CreateStream, not in Resolve.
// A lookup map (preferredModelsByID) is derived from this slice at init time
// for O(1) alias injection in getClaudeModels.
var preferredModels = []llm.Model{
	{ID: ModelHaiku, Name: "Claude Haiku 4.5", Provider: providerName, Aliases: []string{modelNameHaiku, llm.ModelFast}},
	{ID: ModelSonnet, Name: "Claude Sonnet 4.6", Provider: providerName, Aliases: []string{modelNameSonnet, llm.ModelDefault}},
	{ID: ModelOpus, Name: "Claude Opus 4.6", Provider: providerName, Aliases: []string{modelNameOpus, llm.ModelPowerful}},
}

// preferredModelsByID is a map built once from preferredModels for O(1) lookup.
var preferredModelsByID = func() map[string]llm.Model {
	m := make(map[string]llm.Model, len(preferredModels))
	for _, model := range preferredModels {
		m[model.ID] = model
	}
	return m
}()

type claudeModels struct {
	models llm.Models
}

func newClaudeModels() *claudeModels {
	return &claudeModels{
		models: getClaudeModels(),
	}
}

func (m *claudeModels) Models() llm.Models                        { return m.models }
func (m *claudeModels) Resolve(modelID string) (llm.Model, error) { return m.models.Resolve(modelID) }

var _ llm.ModelResolver = (*claudeModels)(nil)
var _ llm.ModelsProvider = (*claudeModels)(nil)

func getClaudeModels() []llm.Model {
	catalogSnapshot, err := llm.LoadBuiltInCatalog()
	if err != nil {
		return preferredModels
	}
	models := llm.CatalogModelsForService(catalogSnapshot, "anthropic", llm.CatalogModelProjectionOptions{
		ProviderName:         providerName,
		ExcludeIntentAliases: true,
	})
	if len(models) == 0 {
		return preferredModels
	}

	remaining := make(map[string]llm.Model)
	for _, model := range models {
		if !supportedModels[model.ID] {
			continue
		}
		if pref, ok := preferredModelsByID[model.ID]; ok {
			model.Aliases = mergeClaudeAliases(pref.Aliases, model.Aliases)
		}
		remaining[model.ID] = model
	}
	if len(remaining) == 0 {
		return preferredModels
	}

	out := make([]llm.Model, 0, len(remaining))
	for _, pref := range preferredModels {
		if model, ok := remaining[pref.ID]; ok {
			out = append(out, model)
			delete(remaining, pref.ID)
			continue
		}
		out = append(out, pref)
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

func mergeClaudeAliases(a, b []string) []string {
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
