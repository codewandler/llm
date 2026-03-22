package auto

import (
	"github.com/codewandler/llm/provider/aggregate"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/openai"
)

// aliasModels defines which model to use for each global alias per provider.
type aliasModels struct {
	fast     string
	normal   string
	powerful string
}

// providerAliasModels maps provider types to their alias model mappings.
// These are used for the built-in global aliases (fast, default, powerful).
var providerAliasModels = map[string]aliasModels{
	ProviderClaude: {
		fast:     anthropic.ModelHaiku,
		normal:   anthropic.ModelSonnet,
		powerful: anthropic.ModelOpus,
	},
	ProviderAnthropic: {
		fast:     anthropic.ModelHaiku,
		normal:   anthropic.ModelSonnet,
		powerful: anthropic.ModelOpus,
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
// Aliases are defined in each provider package (e.g., openai.ModelAliases).
func modelAliasesForProvider(providerType string) map[string]string {
	switch providerType {
	case ProviderClaude, ProviderAnthropic:
		return anthropic.ModelAliases
	case ProviderOpenAI:
		return openai.ModelAliases
	case ProviderBedrock:
		return bedrock.ModelAliases
	default:
		return nil
	}
}
