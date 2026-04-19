package openai

import (
	"fmt"
	"sort"

	"github.com/codewandler/llm"
	modelcatalog "github.com/codewandler/llm/internal/modelcatalog"
	modelcatalogview "github.com/codewandler/llm/internal/modelview"
)

// Model ID constants for programmatic use.
const (
	// GPT-5.4 series (flagship, latest).
	ModelGPT54     = "gpt-5.4"
	ModelGPT54Mini = "gpt-5.4-mini"
	ModelGPT54Nano = "gpt-5.4-nano"
	ModelGPT54Pro  = "gpt-5.4-pro"

	// GPT-5.3 series.
	ModelGPT53Codex = "gpt-5.3-codex"

	// GPT-5.2 series.
	ModelGPT52      = "gpt-5.2"
	ModelGPT52Pro   = "gpt-5.2-pro"
	ModelGPT52Codex = "gpt-5.2-codex"

	// GPT-5.1 series.
	ModelGPT51          = "gpt-5.1"
	ModelGPT51Codex     = "gpt-5.1-codex"
	ModelGPT51CodexMax  = "gpt-5.1-codex-max"
	ModelGPT51CodexMini = "gpt-5.1-codex-mini"

	// GPT-5 series.
	ModelGPT5      = "gpt-5"
	ModelGPT5Mini  = "gpt-5-mini"
	ModelGPT5Nano  = "gpt-5-nano"
	ModelGPT5Pro   = "gpt-5-pro"
	ModelGPT5Codex = "gpt-5-codex"

	// GPT-4o series.
	ModelGPT4o     = "gpt-4o"
	ModelGPT4oMini = "gpt-4o-mini"

	// GPT-4.1 series.
	ModelGPT41     = "gpt-4.1"
	ModelGPT41Mini = "gpt-4.1-mini"
	ModelGPT41Nano = "gpt-4.1-nano"

	// Legacy models.
	ModelGPT4Turbo = "gpt-4-turbo"
	ModelGPT4      = "gpt-4"
	ModelGPT35     = "gpt-3.5-turbo"

	// O-series reasoning models.
	ModelO4Mini = "o4-mini"
	ModelO3     = "o3"
	ModelO3Mini = "o3-mini"
	ModelO3Pro  = "o3-pro"
	ModelO1     = "o1"
	ModelO1Mini = "o1-mini"
	ModelO1Pro  = "o1-pro"
)

// ModelAliases maps short alias names to full model IDs.
// These are used by the auto package for provider-prefixed resolution (e.g., "openai/mini").
var ModelAliases = map[string]string{
	// GPT-5.4 tier (flagship)
	"flagship": ModelGPT54,
	"mini":     ModelGPT54Mini,
	"nano":     ModelGPT54Nano,
	"pro":      ModelGPT54Pro,

	// Coding models
	"codex": ModelGPT53Codex,

	// Thought models
	"o4": ModelO4Mini,
	"o3": ModelO3,
}

// modelCategory identifies reasoning support level for a model.
type modelCategory int

const (
	categoryNonReasoning modelCategory = iota // gpt-4o, gpt-4, gpt-3.5, gpt-4.1
	categoryPreGPT51                          // gpt-5, gpt-5-mini, gpt-5-nano, o1, o3, o4-mini
	categoryGPT51                             // gpt-5.1
	categoryPro                               // gpt-5-pro, gpt-5.2-pro, o1-pro, o3-pro
	categoryCodex                             // codex models (support xhigh)
)

// modelInfo contains metadata and routing properties for a model.
// Pricing is managed centrally in usage/pricing.go (KnownPricing).
type modelInfo struct {
	ID                    string        // API model ID
	Name                  string        // Human-readable name
	Category              modelCategory // Thought support category
	SupportsExtendedCache bool          // True if model supports 24h prompt cache retention
	UseResponsesAPI       bool          // True if the model must be called via /v1/responses instead of /v1/chat/completions
}

// modelRegistry maps model IDs to provider-specific routing metadata.
// The built-in catalog is the source of truth for model discovery; this table
// only captures OpenAI behavior such as API selection and reasoning category.
var modelRegistry = map[string]modelInfo{
	// GPT-5.4 series (flagship, latest) — requires Responses API (/v1/responses)
	"gpt-5.4":      {ID: "gpt-5.4", Name: "GPT-5.4", Category: categoryPreGPT51, SupportsExtendedCache: true, UseResponsesAPI: true},
	"gpt-5.4-mini": {ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini", Category: categoryPreGPT51, SupportsExtendedCache: true, UseResponsesAPI: true},
	"gpt-5.4-nano": {ID: "gpt-5.4-nano", Name: "GPT-5.4 Nano", Category: categoryPreGPT51, SupportsExtendedCache: true, UseResponsesAPI: true},
	"gpt-5.4-pro":  {ID: "gpt-5.4-pro", Name: "GPT-5.4 Pro", Category: categoryPro, UseResponsesAPI: true},

	// GPT-5.3 series
	"gpt-5.3-codex": {ID: "gpt-5.3-codex", Name: "GPT-5.3 Codex", Category: categoryCodex, SupportsExtendedCache: true},

	// GPT-5.2 series
	"gpt-5.2":       {ID: "gpt-5.2", Name: "GPT-5.2", Category: categoryPreGPT51, SupportsExtendedCache: true},
	"gpt-5.2-pro":   {ID: "gpt-5.2-pro", Name: "GPT-5.2 Pro", Category: categoryPro},
	"gpt-5.2-codex": {ID: "gpt-5.2-codex", Name: "GPT-5.2 Codex", Category: categoryCodex, SupportsExtendedCache: true},

	// GPT-5.1 series
	"gpt-5.1":            {ID: "gpt-5.1", Name: "GPT-5.1", Category: categoryGPT51, SupportsExtendedCache: true, UseResponsesAPI: true},
	"gpt-5.1-codex":      {ID: "gpt-5.1-codex", Name: "GPT-5.1 Codex", Category: categoryCodex, SupportsExtendedCache: true},
	"gpt-5.1-codex-max":  {ID: "gpt-5.1-codex-max", Name: "GPT-5.1 Codex Max", Category: categoryCodex, SupportsExtendedCache: true},
	"gpt-5.1-codex-mini": {ID: "gpt-5.1-codex-mini", Name: "GPT-5.1 Codex Mini", Category: categoryCodex, SupportsExtendedCache: true},

	// GPT-5 series
	"gpt-5":       {ID: "gpt-5", Name: "GPT-5", Category: categoryPreGPT51, SupportsExtendedCache: true},
	"gpt-5-mini":  {ID: "gpt-5-mini", Name: "GPT-5 Mini", Category: categoryPreGPT51, SupportsExtendedCache: true},
	"gpt-5-nano":  {ID: "gpt-5-nano", Name: "GPT-5 Nano", Category: categoryPreGPT51, SupportsExtendedCache: true},
	"gpt-5-pro":   {ID: "gpt-5-pro", Name: "GPT-5 Pro", Category: categoryPro},
	"gpt-5-codex": {ID: "gpt-5-codex", Name: "GPT-5 Codex", Category: categoryCodex, SupportsExtendedCache: true},

	// GPT-4o series
	"gpt-4o":      {ID: "gpt-4o", Name: "GPT-4o", Category: categoryNonReasoning},
	"gpt-4o-mini": {ID: "gpt-4o-mini", Name: "GPT-4o Mini", Category: categoryNonReasoning},

	// GPT-4.1 series (extended cache supported)
	"gpt-4.1":      {ID: "gpt-4.1", Name: "GPT-4.1", Category: categoryNonReasoning, SupportsExtendedCache: true},
	"gpt-4.1-mini": {ID: "gpt-4.1-mini", Name: "GPT-4.1 Mini", Category: categoryNonReasoning, SupportsExtendedCache: true},
	"gpt-4.1-nano": {ID: "gpt-4.1-nano", Name: "GPT-4.1 Nano", Category: categoryNonReasoning, SupportsExtendedCache: true},

	// GPT-4 series (legacy)
	"gpt-4-turbo": {ID: "gpt-4-turbo", Name: "GPT-4 Turbo", Category: categoryNonReasoning},
	"gpt-4":       {ID: "gpt-4", Name: "GPT-4", Category: categoryNonReasoning},

	// GPT-3.5 series (legacy)
	"gpt-3.5-turbo": {ID: "gpt-3.5-turbo", Name: "GPT-3.5 Turbo", Category: categoryNonReasoning},

	// o4 series
	"o4-mini": {ID: "o4-mini", Name: "o4 Mini", Category: categoryPreGPT51},

	// o3 series
	"o3":      {ID: "o3", Name: "o3", Category: categoryPreGPT51},
	"o3-mini": {ID: "o3-mini", Name: "o3 Mini", Category: categoryPreGPT51},
	"o3-pro":  {ID: "o3-pro", Name: "o3 Pro", Category: categoryPro},

	// o1 series (legacy reasoning)
	"o1":      {ID: "o1", Name: "o1", Category: categoryPreGPT51},
	"o1-mini": {ID: "o1-mini", Name: "o1 Mini", Category: categoryPreGPT51},
	"o1-pro":  {ID: "o1-pro", Name: "o1 Pro", Category: categoryPro},
}

// ErrUnknownModel is returned when a model ID is not in the registry.
var ErrUnknownModel = fmt.Errorf("unknown model")

// modelOrder defines the display order for Models().
// This is a curated list of the most popular/useful models.
var modelOrder = []string{
	// GPT-5.4 series (flagship, latest)
	"gpt-5.4",
	"gpt-5.4-mini",
	"gpt-5.4-nano",
	"gpt-5.4-pro",

	// GPT-5.3 series
	"gpt-5.3-codex",

	// GPT-5.2 series
	"gpt-5.2",
	"gpt-5.2-pro",
	"gpt-5.2-codex",

	// GPT-5.1 series
	"gpt-5.1",
	"gpt-5.1-codex",
	"gpt-5.1-codex-max",
	"gpt-5.1-codex-mini",

	// GPT-5 series
	"gpt-5",
	"gpt-5-mini",
	"gpt-5-nano",
	"gpt-5-pro",
	"gpt-5-codex",

	// GPT-4o series
	"gpt-4o",
	"gpt-4o-mini",

	// GPT-4.1 series
	"gpt-4.1",
	"gpt-4.1-mini",
	"gpt-4.1-nano",

	// GPT-4 series (legacy)
	"gpt-4-turbo",
	"gpt-4",

	// GPT-3.5 series (legacy)
	"gpt-3.5-turbo",

	// o-series (reasoning models)
	"o4-mini",
	"o3",
	"o3-mini",
	"o3-pro",
	"o1",
	"o1-mini",
	"o1-pro",
}

func (p *Provider) catalogModels() llm.Models {
	return loadOpenAIModels(p.Name())
}

func loadOpenAIModels(providerName string) llm.Models {
	fallback := fallbackOpenAIModels(providerName)
	catalogSnapshot, err := modelcatalog.LoadBuiltIn()
	if err != nil {
		return fallback
	}

	models := modelcatalogview.ModelsForService(catalogSnapshot, "openai", modelcatalogview.ProjectionOptions{
		ProviderName:          providerName,
		ExcludeBuiltinAliases: true,
	})
	if len(models) == 0 {
		return fallback
	}

	remaining := make(map[string]llm.Model, len(models))
	for _, model := range models {
		if info, ok := modelRegistry[model.ID]; ok && (model.Name == "" || model.Name == model.ID) {
			model.Name = info.Name
		}
		model.Aliases = mergeOpenAIAliases(policyAliasesForModel(model.ID), model.Aliases)
		remaining[model.ID] = model
	}
	if len(remaining) == 0 {
		return fallback
	}

	out := make(llm.Models, 0, len(remaining))
	for _, model := range fallback {
		if catalogModel, ok := remaining[model.ID]; ok {
			out = append(out, catalogModel)
			delete(remaining, model.ID)
			continue
		}
		out = append(out, model)
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

func fallbackOpenAIModels(providerName string) llm.Models {
	models := make(llm.Models, 0, len(modelOrder))
	for _, id := range modelOrder {
		info, ok := modelRegistry[id]
		if !ok {
			continue
		}
		models = append(models, llm.Model{
			ID:       info.ID,
			Name:     info.Name,
			Provider: providerName,
			Aliases:  policyAliasesForModel(info.ID),
		})
	}
	return models
}

func policyAliasesForModel(modelID string) []string {
	aliases := make([]string, 0, 2)
	for alias, target := range ModelAliases {
		if target == modelID {
			aliases = append(aliases, alias)
		}
	}
	return aliases
}

func mergeOpenAIAliases(a, b []string) []string {
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

// useResponsesAPI reports whether the given model ID should be routed to the
// Responses API (/v1/responses) instead of Chat Completions (/v1/chat/completions).
// Two model classes require /v1/responses:
//   - Codex models (categoryCodex) — always routed via Responses API.
//   - Models with UseResponsesAPI: true — newer non-Codex models (e.g. gpt-5.4 series)
//     that are only available on the Responses API endpoint.
//
// Unknown models default to false so they are routed to Chat Completions.
func useResponsesAPI(model string) bool {
	info, ok := modelRegistry[model]
	if !ok {
		return false
	}
	return info.Category == categoryCodex || info.UseResponsesAPI
}

// UseResponsesAPI reports whether the given model requires the OpenAI
// Responses API (/v1/responses). Exported for use by providers that route
// through the OpenAI wire format (e.g. OpenRouter).
func UseResponsesAPI(model string) bool { return useResponsesAPI(model) }

// getModelInfo returns the model info for the given model ID.
// Returns ErrUnknownModel if the model is not in the registry.
func getModelInfo(model string) (modelInfo, error) {
	info, ok := modelRegistry[model]
	if !ok {
		return modelInfo{}, fmt.Errorf("%w: %s", ErrUnknownModel, model)
	}
	return info, nil
}

// mapEffortAndThinking maps the user-requested Effort and ThinkingMode to a
// valid OpenAI reasoning_effort API value.
// Returns empty string if the parameter should be omitted, or an error if the
// model is unknown.
func mapEffortAndThinking(model string, effort llm.Effort, thinking llm.ThinkingMode) (string, error) {
	info, err := getModelInfo(model)
	if err != nil {
		// Unknown model (e.g. an OpenAI-compatible provider like Docker Model Runner).
		// Treat as non-reasoning: no reasoning_effort field, no error.
		return "", nil
	}

	// Non-reasoning models never send reasoning_effort.
	if info.Category == categoryNonReasoning {
		return "", nil
	}

	// Thinking explicitly off → disable where possible.
	if thinking.IsOff() {
		switch info.Category {
		case categoryGPT51:
			return "none", nil
		default:
			// pre-GPT-5.1, Pro, Codex: can't reliably disable reasoning
			// (codex-mini variants reject "none"). Omit gracefully.
			return "", nil
		}
	}

	// Clamp EffortMax → xhigh for Codex, else High.
	if effort == llm.EffortMax {
		if info.Category == categoryCodex {
			return "xhigh", nil
		}
		effort = llm.EffortHigh
	}

	// Thinking explicitly on but no effort specified → default to high.
	if thinking.IsOn() && effort.IsEmpty() {
		return "high", nil
	}

	// No effort specified → omit, let API use its default.
	if effort.IsEmpty() {
		return "", nil
	}

	return string(effort), nil
}
