package auto

import (
	"github.com/codewandler/llm/provider/aggregate"
	"github.com/codewandler/llm/provider/bedrock"
)

// claudeModelAliases maps short names to full Claude model IDs.
var claudeModelAliases = map[string]string{
	"opus":   ClaudeOpus,
	"sonnet": ClaudeSonnet,
	"haiku":  ClaudeHaiku,
}

// anthropicModelAliases maps short names to full Anthropic model IDs.
var anthropicModelAliases = map[string]string{
	"opus":   AnthropicOpus,
	"sonnet": AnthropicSonnet,
	"haiku":  AnthropicHaiku,
}

// aliasModels defines which model to use for each global alias per provider.
type aliasModels struct {
	fast     string
	normal   string
	powerful string
}

// providerAliasModels maps provider types to their alias model mappings.
var providerAliasModels = map[string]aliasModels{
	ProviderClaude: {
		fast:     "haiku",
		normal:   "sonnet",
		powerful: "opus",
	},
	ProviderAnthropic: {
		fast:     "haiku",
		normal:   "sonnet",
		powerful: "opus",
	},
	ProviderBedrock: {
		fast:     bedrock.ModelHaikuLatest,
		normal:   bedrock.ModelSonnetLatest,
		powerful: bedrock.ModelOpusLatest,
	},
}

// buildAliasTargets creates alias targets for a provider instance.
func buildAliasTargets(instanceName, providerType string) map[string]aggregate.AliasTarget {
	models, ok := providerAliasModels[providerType]
	if !ok {
		return nil
	}

	return map[string]aggregate.AliasTarget{
		AliasFast:     {Provider: instanceName, Model: models.fast},
		AliasDefault:  {Provider: instanceName, Model: models.normal},
		AliasPowerful: {Provider: instanceName, Model: models.powerful},
	}
}

// modelAliasesForProvider returns the local model aliases for a provider type.
func modelAliasesForProvider(providerType string) map[string]string {
	switch providerType {
	case ProviderClaude:
		return claudeModelAliases
	case ProviderAnthropic:
		return anthropicModelAliases
	default:
		return nil
	}
}
