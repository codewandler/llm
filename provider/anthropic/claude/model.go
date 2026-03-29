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

type modelMap map[string]llm.Model

func (m modelMap) Models() llm.Models {
	all := make([]llm.Model, 0, len(m))
	for _, mm := range m {
		all = append(all, mm)
	}
	return all
}

var modelPreferences = modelMap{
	ModelHaiku:  {ID: ModelHaiku, Name: "Claude Haiku 4.5", Provider: providerName, Aliases: []string{modelNameHaiku, llm.ModelDefault, llm.ModelFast}},
	ModelSonnet: {ID: ModelSonnet, Name: "Claude Sonnet 4.6", Provider: providerName, Aliases: []string{modelNameSonnet}},
	ModelOpus:   {ID: ModelOpus, Name: "Claude Opus 4.6", Provider: providerName, Aliases: []string{modelNameOpus, llm.ModelPowerful}},
}

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
		if aliases, ok := modelPreferences[id]; ok {
			mm.Aliases = aliases.Aliases
		}
		out = append(out, mm)
	}

	return append(modelPreferences.Models(), out...)
}
