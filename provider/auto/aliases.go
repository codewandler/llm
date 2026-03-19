package auto

import (
	"github.com/codewandler/llm/provider/aggregate"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/openai"
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

// openaiModelAliases maps short names to full OpenAI model IDs.
var openaiModelAliases = map[string]string{
	// GPT-5.4 tier (flagship)
	"flagship": openai.ModelGPT54,
	"mini":     openai.ModelGPT54Mini,
	"nano":     openai.ModelGPT54Nano,
	"pro":      openai.ModelGPT54Pro,

	// Coding models
	"codex": openai.ModelGPT53Codex,

	// Reasoning models
	"o4": openai.ModelO4Mini,
	"o3": openai.ModelO3,
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
	case ProviderOpenAI:
		return openaiModelAliases
	default:
		return nil
	}
}
