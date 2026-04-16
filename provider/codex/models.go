package codex

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/catalog"
)

const modelsClientVersion = "0.121.0"

//go:embed models.json
var embeddedModelsJSON []byte

type reasoningEffortPreset struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

type modelInfo struct {
	Slug                     string                  `json:"slug"`
	DisplayName              string                  `json:"display_name"`
	Description              *string                 `json:"description"`
	DefaultReasoningLevel    *string                 `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels []reasoningEffortPreset `json:"supported_reasoning_levels,omitempty"`
	Visibility               string                  `json:"visibility"`
	SupportedInAPI           bool                    `json:"supported_in_api"`
	Priority                 int                     `json:"priority"`
	AdditionalSpeedTiers     []string                `json:"additional_speed_tiers,omitempty"`
	SupportVerbosity         bool                    `json:"support_verbosity,omitempty"`
	DefaultVerbosity         *string                 `json:"default_verbosity,omitempty"`
	SupportsReasoningSummary bool                    `json:"supports_reasoning_summaries,omitempty"`
	DefaultReasoningSummary  *string                 `json:"default_reasoning_summary,omitempty"`
	ContextWindow            int                     `json:"context_window,omitempty"`
	InputModalities          []string                `json:"input_modalities,omitempty"`
	OutputModalities         []string                `json:"output_modalities,omitempty"`
	KnowledgeCutoff          *string                 `json:"knowledge_cutoff,omitempty"`
	LastUpdated              *string                 `json:"last_updated,omitempty"`
	Deprecated               bool                    `json:"deprecated,omitempty"`
	SupportsParallelTools    bool                    `json:"supports_parallel_tool_calls,omitempty"`
	TruncationPolicy         *struct {
		Mode  string `json:"mode"`
		Limit int    `json:"limit"`
	} `json:"truncation_policy,omitempty"`
}

type modelsResponse struct {
	Models []modelInfo `json:"models"`
}

var embeddedModels = mustLoadEmbeddedModels()

func EmbeddedModels() []modelInfo {
	out := make([]modelInfo, len(embeddedModels.Models))
	copy(out, embeddedModels.Models)
	return out
}

func DefaultModelID() string {
	if model, ok := firstPresent(embeddedModels.Models, "gpt-5.4"); ok {
		return model.Slug
	}
	if model, ok := firstVisibleByPriority(embeddedModels.Models); ok {
		return model.Slug
	}
	if len(embeddedModels.Models) == 0 {
		return ""
	}
	return embeddedModels.Models[0].Slug
}

func FastModelID() string {
	if model, ok := firstPresent(embeddedModels.Models, "gpt-5.4-mini", "gpt-5.3-codex-spark", "gpt-5.1-codex-mini"); ok {
		return model.Slug
	}
	return DefaultModelID()
}

func PowerfulModelID() string {
	if model, ok := firstPresent(embeddedModels.Models, "gpt-5.1-codex-max", "gpt-5.4", "gpt-5.3-codex"); ok {
		return model.Slug
	}
	return DefaultModelID()
}

func BuiltinAliasModels() (fast, normal, powerful string) {
	return FastModelID(), DefaultModelID(), PowerfulModelID()
}

func ModelAliases() map[string]string {
	aliases := map[string]string{}
	if model := DefaultModelID(); model != "" {
		aliases["codex"] = model
	}
	if model := FastModelID(); model != "" {
		aliases["mini"] = model
	}
	if model, ok := firstPresent(embeddedModels.Models, "gpt-5.3-codex-spark"); ok {
		aliases["spark"] = model.Slug
	}
	return aliases
}

func fallbackModels() llm.Models {
	models := EmbeddedModels()
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].Priority != models[j].Priority {
			return models[i].Priority < models[j].Priority
		}
		return models[i].Slug < models[j].Slug
	})
	out := make(llm.Models, 0, len(models))
	for _, model := range models {
		if model.Visibility != "list" {
			continue
		}
		out = append(out, llm.Model{
			ID:       model.Slug,
			Name:     firstNonEmpty(model.DisplayName, model.Slug),
			Provider: llm.ProviderNameCodex,
		})
	}
	return attachProviderAliases(out)
}

func attachProviderAliases(models llm.Models) llm.Models {
	aliasMap := ModelAliases()
	for i := range models {
		aliases := append([]string(nil), models[i].Aliases...)
		for alias, target := range aliasMap {
			if target == models[i].ID {
				aliases = appendAlias(aliases, alias)
			}
		}
		models[i].Aliases = aliases
	}
	return models
}

func appendAlias(existing []string, alias string) []string {
	if alias == "" {
		return existing
	}
	for _, current := range existing {
		if current == alias {
			return existing
		}
	}
	return append(existing, alias)
}

func firstPresent(models []modelInfo, preferred ...string) (modelInfo, bool) {
	for _, slug := range preferred {
		for _, model := range models {
			if model.Slug == slug {
				return model, true
			}
		}
	}
	return modelInfo{}, false
}

func firstVisibleByPriority(models []modelInfo) (modelInfo, bool) {
	visible := make([]modelInfo, 0, len(models))
	for _, model := range models {
		if model.Visibility == "list" {
			visible = append(visible, model)
		}
	}
	if len(visible) == 0 {
		return modelInfo{}, false
	}
	sort.SliceStable(visible, func(i, j int) bool {
		if visible[i].Priority != visible[j].Priority {
			return visible[i].Priority < visible[j].Priority
		}
		return visible[i].Slug < visible[j].Slug
	})
	return visible[0], true
}

func mustLoadEmbeddedModels() modelsResponse {
	var payload modelsResponse
	if err := json.Unmarshal(embeddedModelsJSON, &payload); err != nil {
		panic(fmt.Sprintf("codex: parse embedded models.json: %v", err))
	}
	return payload
}

func modelKeyForSlug(slug string) (catalog.ModelKey, bool) {
	id := strings.TrimSpace(slug)
	if id == "" {
		return catalog.ModelKey{}, false
	}
	if strings.HasPrefix(id, "gpt-oss-") {
		return catalog.NormalizeKey(catalog.ModelKey{Creator: "openai", Family: "gpt-oss", Variant: strings.TrimPrefix(id, "gpt-oss-")}), true
	}
	if strings.HasPrefix(id, "gpt-") {
		rest := strings.TrimPrefix(id, "gpt-")
		parts := strings.Split(rest, "-")
		if len(parts) == 0 || parts[0] == "" {
			return catalog.ModelKey{}, false
		}
		version := parts[0]
		variant := ""
		if len(parts) > 1 {
			variant = strings.Join(parts[1:], "-")
		}
		return catalog.NormalizeKey(catalog.ModelKey{Creator: "openai", Family: "gpt", Version: version, Variant: variant}), true
	}
	if len(id) >= 2 && id[0] == 'o' {
		parts := strings.Split(strings.TrimPrefix(id, "o"), "-")
		if len(parts) == 0 || parts[0] == "" {
			return catalog.ModelKey{}, false
		}
		version := parts[0]
		variant := ""
		if len(parts) > 1 {
			variant = strings.Join(parts[1:], "-")
		}
		return catalog.NormalizeKey(catalog.ModelKey{Creator: "openai", Family: "o", Version: version, Variant: variant}), true
	}
	return catalog.ModelKey{}, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
