package anthropic

import (
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

var allModels = llm.Models{
	{ID: "claude-sonnet-4-5-20250929", Name: "Claude Sonnet 4.5", Provider: providerName},
	{ID: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", Provider: providerName},
}
var allModelsWithAliases = llm.Models{
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
