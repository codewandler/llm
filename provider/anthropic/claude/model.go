package claude

import (
	"sort"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/modeldb"
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
	provider, ok := modeldb.GetProvider("anthropic")
	if !ok || len(provider.Models) == 0 {
		return nil
	}

	ids := make([]string, 0, len(supportedModels))
	for id := range provider.Models {
		if supportedModels[id] {
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	sort.Strings(ids)
	out := make([]llm.Model, 0, len(ids))
	for _, id := range ids {
		m := provider.Models[id]
		name := m.Name
		if name == "" {
			name = id
		}
		mm := llm.Model{ID: id, Name: name, Provider: providerName}
		// Inject curated aliases from preferredModelsByID (O(1) lookup).
		if pref, ok := preferredModelsByID[id]; ok {
			mm.Aliases = pref.Aliases
		}
		out = append(out, mm)
	}

	// preferredModels comes first: its ordering determines the provider default
	// (position 0) and the alias set visible to callers.
	return append(preferredModels, out...)
}
