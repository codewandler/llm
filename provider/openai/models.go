package openai

import (
	"fmt"

	"github.com/codewandler/llm"
)

// modelCategory identifies reasoning support level for a model.
type modelCategory int

const (
	categoryNonReasoning modelCategory = iota // gpt-4o, gpt-4, gpt-3.5, gpt-4.1
	categoryPreGPT51                          // gpt-5, gpt-5-mini, gpt-5-nano, o1, o3, o4-mini
	categoryGPT51                             // gpt-5.1
	categoryPro                               // gpt-5-pro, gpt-5.2-pro, o1-pro, o3-pro
	categoryCodex                             // codex models (support xhigh)
)

// modelInfo contains metadata, pricing, and reasoning support for a model.
type modelInfo struct {
	ID                    string        // API model ID
	Name                  string        // Human-readable name
	InputPrice            float64       // USD per 1M input tokens
	OutputPrice           float64       // USD per 1M output tokens
	CachedInputPrice      float64       // USD per 1M cached input tokens (0 if not supported)
	Category              modelCategory // Reasoning support category
	SupportsExtendedCache bool          // True if model supports 24h prompt cache retention
}

// modelRegistry maps model IDs to their info.
// Pricing data sourced from LiteLLM: https://github.com/BerriAI/litellm
// Extended cache support per OpenAI docs: gpt-5.2, gpt-5.1, gpt-5, gpt-4.1, and codex models
var modelRegistry = map[string]modelInfo{
	// GPT-4o series (no extended cache support)
	"gpt-4o":      {ID: "gpt-4o", Name: "GPT-4o", InputPrice: 2.50, OutputPrice: 10.00, CachedInputPrice: 1.25, Category: categoryNonReasoning},
	"gpt-4o-mini": {ID: "gpt-4o-mini", Name: "GPT-4o Mini", InputPrice: 0.15, OutputPrice: 0.60, CachedInputPrice: 0.075, Category: categoryNonReasoning},

	// GPT-4.1 series (extended cache supported)
	"gpt-4.1":      {ID: "gpt-4.1", Name: "GPT-4.1", InputPrice: 2.00, OutputPrice: 8.00, CachedInputPrice: 0.50, Category: categoryNonReasoning, SupportsExtendedCache: true},
	"gpt-4.1-mini": {ID: "gpt-4.1-mini", Name: "GPT-4.1 Mini", InputPrice: 0.40, OutputPrice: 1.60, CachedInputPrice: 0.10, Category: categoryNonReasoning, SupportsExtendedCache: true},
	"gpt-4.1-nano": {ID: "gpt-4.1-nano", Name: "GPT-4.1 Nano", InputPrice: 0.10, OutputPrice: 0.40, CachedInputPrice: 0.025, Category: categoryNonReasoning, SupportsExtendedCache: true},

	// GPT-4 series (legacy, no extended cache support)
	"gpt-4-turbo": {ID: "gpt-4-turbo", Name: "GPT-4 Turbo", InputPrice: 10.00, OutputPrice: 30.00, CachedInputPrice: 0, Category: categoryNonReasoning},
	"gpt-4":       {ID: "gpt-4", Name: "GPT-4", InputPrice: 30.00, OutputPrice: 60.00, CachedInputPrice: 0, Category: categoryNonReasoning},

	// GPT-3.5 series (legacy, no extended cache support)
	"gpt-3.5-turbo": {ID: "gpt-3.5-turbo", Name: "GPT-3.5 Turbo", InputPrice: 0.50, OutputPrice: 1.50, CachedInputPrice: 0, Category: categoryNonReasoning},

	// GPT-5 series (extended cache supported)
	"gpt-5":      {ID: "gpt-5", Name: "GPT-5", InputPrice: 1.25, OutputPrice: 10.00, CachedInputPrice: 0.125, Category: categoryPreGPT51, SupportsExtendedCache: true},
	"gpt-5-mini": {ID: "gpt-5-mini", Name: "GPT-5 Mini", InputPrice: 0.25, OutputPrice: 2.00, CachedInputPrice: 0.025, Category: categoryPreGPT51, SupportsExtendedCache: true},
	"gpt-5-nano": {ID: "gpt-5-nano", Name: "GPT-5 Nano", InputPrice: 0.05, OutputPrice: 0.40, CachedInputPrice: 0.005, Category: categoryPreGPT51, SupportsExtendedCache: true},
	"gpt-5.2":    {ID: "gpt-5.2", Name: "GPT-5.2", InputPrice: 1.75, OutputPrice: 14.00, CachedInputPrice: 0.175, Category: categoryPreGPT51, SupportsExtendedCache: true},

	// GPT-5.1 (extended cache supported)
	"gpt-5.1": {ID: "gpt-5.1", Name: "GPT-5.1", InputPrice: 1.25, OutputPrice: 10.00, CachedInputPrice: 0.125, Category: categoryGPT51, SupportsExtendedCache: true},

	// GPT-5 Pro models (no extended cache support per docs)
	"gpt-5-pro":   {ID: "gpt-5-pro", Name: "GPT-5 Pro", InputPrice: 15.00, OutputPrice: 120.00, CachedInputPrice: 0, Category: categoryPro},
	"gpt-5.2-pro": {ID: "gpt-5.2-pro", Name: "GPT-5.2 Pro", InputPrice: 21.00, OutputPrice: 168.00, CachedInputPrice: 0, Category: categoryPro},

	// Codex models (extended cache supported)
	"gpt-5-codex":        {ID: "gpt-5-codex", Name: "GPT-5 Codex", InputPrice: 1.25, OutputPrice: 10.00, CachedInputPrice: 0.125, Category: categoryCodex, SupportsExtendedCache: true},
	"gpt-5.1-codex":      {ID: "gpt-5.1-codex", Name: "GPT-5.1 Codex", InputPrice: 1.25, OutputPrice: 10.00, CachedInputPrice: 0.125, Category: categoryCodex, SupportsExtendedCache: true},
	"gpt-5.1-codex-max":  {ID: "gpt-5.1-codex-max", Name: "GPT-5.1 Codex Max", InputPrice: 1.25, OutputPrice: 10.00, CachedInputPrice: 0.125, Category: categoryCodex, SupportsExtendedCache: true},
	"gpt-5.1-codex-mini": {ID: "gpt-5.1-codex-mini", Name: "GPT-5.1 Codex Mini", InputPrice: 0.25, OutputPrice: 2.00, CachedInputPrice: 0.025, Category: categoryCodex, SupportsExtendedCache: true},
	"gpt-5.2-codex":      {ID: "gpt-5.2-codex", Name: "GPT-5.2 Codex", InputPrice: 1.75, OutputPrice: 14.00, CachedInputPrice: 0.175, Category: categoryCodex, SupportsExtendedCache: true},

	// o-series reasoning models (no extended cache support)
	"o1":      {ID: "o1", Name: "o1", InputPrice: 15.00, OutputPrice: 60.00, CachedInputPrice: 7.50, Category: categoryPreGPT51},
	"o1-mini": {ID: "o1-mini", Name: "o1 Mini", InputPrice: 1.10, OutputPrice: 4.40, CachedInputPrice: 0.55, Category: categoryPreGPT51},
	"o3":      {ID: "o3", Name: "o3", InputPrice: 2.00, OutputPrice: 8.00, CachedInputPrice: 0.50, Category: categoryPreGPT51},
	"o3-mini": {ID: "o3-mini", Name: "o3 Mini", InputPrice: 1.10, OutputPrice: 4.40, CachedInputPrice: 0.55, Category: categoryPreGPT51},
	"o4-mini": {ID: "o4-mini", Name: "o4 Mini", InputPrice: 1.10, OutputPrice: 4.40, CachedInputPrice: 0.275, Category: categoryPreGPT51},

	// o-series Pro models (no extended cache support)
	"o1-pro": {ID: "o1-pro", Name: "o1 Pro", InputPrice: 150.00, OutputPrice: 600.00, CachedInputPrice: 0, Category: categoryPro},
	"o3-pro": {ID: "o3-pro", Name: "o3 Pro", InputPrice: 20.00, OutputPrice: 80.00, CachedInputPrice: 0, Category: categoryPro},
}

// ErrUnknownModel is returned when a model ID is not in the registry.
var ErrUnknownModel = fmt.Errorf("unknown model")

// modelOrder defines the display order for Models().
// This is a curated list of the most popular/useful models.
var modelOrder = []string{
	// GPT-4o series (latest, most capable non-reasoning)
	"gpt-4o",
	"gpt-4o-mini",

	// GPT-5 series (newest generation)
	"gpt-5",
	"gpt-5.2",
	"gpt-5.2-pro",
	"gpt-5.1",
	"gpt-5-pro",
	"gpt-5-mini",
	"gpt-5-nano",
	"gpt-5.1-codex",
	"gpt-5.2-codex",

	// GPT-4.1 series
	"gpt-4.1",
	"gpt-4.1-mini",
	"gpt-4.1-nano",

	// GPT-4 series (previous generation)
	"gpt-4-turbo",
	"gpt-4",

	// GPT-3.5 series (legacy)
	"gpt-3.5-turbo",

	// o-series (reasoning models)
	"o3",
	"o3-mini",
	"o3-pro",
	"o4-mini",
	"o1",
	"o1-mini",
	"o1-pro",
}

// getModelInfo returns the model info for the given model ID.
// Returns ErrUnknownModel if the model is not in the registry.
func getModelInfo(model string) (modelInfo, error) {
	info, ok := modelRegistry[model]
	if !ok {
		return modelInfo{}, fmt.Errorf("%w: %s", ErrUnknownModel, model)
	}
	return info, nil
}

// mapReasoningEffort maps the user-requested reasoning effort to a valid OpenAI API value.
// Returns empty string if the parameter should be omitted, or an error if the value is invalid.
func mapReasoningEffort(model string, effort llm.ReasoningEffort) (string, error) {
	if effort == "" {
		return "", nil // omit, let API use its default
	}

	info, err := getModelInfo(model)
	if err != nil {
		return "", err
	}

	switch info.Category {
	case categoryNonReasoning:
		// Non-reasoning models ignore reasoning_effort
		return "", nil

	case categoryPreGPT51:
		// Supports: minimal, low, medium, high
		// Does NOT support: none, xhigh
		switch effort {
		case llm.ReasoningEffortNone:
			return "", fmt.Errorf("reasoning_effort %q not supported for model %q (use minimal, low, medium, or high)", effort, model)
		case llm.ReasoningEffortXHigh:
			return "", fmt.Errorf("reasoning_effort %q not supported for model %q (use minimal, low, medium, or high)", effort, model)
		case llm.ReasoningEffortMinimal, llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh:
			return string(effort), nil
		}

	case categoryGPT51:
		// Supports: none, low, medium, high
		// Does NOT support: minimal, xhigh
		// Map minimal -> low
		switch effort {
		case llm.ReasoningEffortMinimal:
			return "low", nil // map minimal -> low
		case llm.ReasoningEffortXHigh:
			return "", fmt.Errorf("reasoning_effort %q not supported for model %q (use none, low, medium, or high)", effort, model)
		case llm.ReasoningEffortNone, llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh:
			return string(effort), nil
		}

	case categoryPro:
		// Only supports: high
		if effort != llm.ReasoningEffortHigh {
			return "", fmt.Errorf("reasoning_effort must be %q for model %q", llm.ReasoningEffortHigh, model)
		}
		return "high", nil

	case categoryCodex:
		// Supports: none, low, medium, high, xhigh
		// Map minimal -> low
		switch effort {
		case llm.ReasoningEffortMinimal:
			return "low", nil // map minimal -> low
		case llm.ReasoningEffortNone, llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh:
			return string(effort), nil
		}
	}

	// Unknown effort value - shouldn't happen if Valid() was called
	return "", fmt.Errorf("unknown reasoning_effort value %q", effort)
}

// calculateCost computes the cost in USD for the given usage and model.
// Returns 0 if the model is unknown.
func calculateCost(model string, usage *llm.Usage) float64 {
	if usage == nil {
		return 0
	}

	info, err := getModelInfo(model)
	if err != nil {
		return 0 // unknown model, can't calculate cost
	}

	// Regular input tokens (non-cached)
	regularInput := usage.InputTokens - usage.CachedTokens
	if regularInput < 0 {
		regularInput = 0
	}

	cost := (float64(regularInput) / 1_000_000) * info.InputPrice
	cost += (float64(usage.CachedTokens) / 1_000_000) * info.CachedInputPrice
	cost += (float64(usage.OutputTokens) / 1_000_000) * info.OutputPrice

	return cost
}
