package auto

import (
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/minimax"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/router"
)

// aliasModels defines which model to use for each global alias per provider.
type aliasModels struct {
	fast     string
	normal   string
	powerful string
	codex    string // optional: the codex model to use for the AliasCodex global alias
}

// providerAliasModels maps provider types to their alias model mappings.
// These are used for the built-in global aliases (fast, default, powerful, codex).
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
	ProviderOpenAI: {
		fast:     openai.ModelGPT4oMini,
		normal:   openai.ModelGPT4o,
		powerful: openai.ModelO3,
	},
	ProviderMiniMax: {
		fast:     minimax.ModelM27,
		normal:   minimax.ModelM27,
		powerful: minimax.ModelM27,
	},
	// ChatGPT / Codex: accessed via chatgpt.com/backend-api using Codex CLI OAuth.
	// All models are Codex-category; no general-purpose GPT aliases here.
	ProviderChatGPT: {
		fast:     openai.ModelGPT51CodexMini,
		normal:   openai.ModelGPT53Codex,
		powerful: openai.ModelGPT53Codex,
		codex:    openai.ModelGPT53Codex,
	},
}

// buildAliasTargets creates alias targets for a provider instance.
func buildAliasTargets(instanceName, providerType string) map[string]router.AliasTarget {
	models, ok := providerAliasModels[providerType]
	if !ok {
		return nil
	}

	targets := map[string]router.AliasTarget{
		AliasFast:     {Provider: instanceName, Model: models.fast},
		AliasDefault:  {Provider: instanceName, Model: models.normal},
		AliasPowerful: {Provider: instanceName, Model: models.powerful},
	}
	// Wire the global "codex" alias for providers that designate a codex model.
	if models.codex != "" {
		targets[AliasCodex] = router.AliasTarget{Provider: instanceName, Model: models.codex}
	}
	return targets
}

// modelAliasesForProvider returns the local model aliases for a provider type.
// Aliases are defined in each provider package (e.g., openai.ModelAliases).
func modelAliasesForProvider(providerType string) map[string]string {
	switch providerType {
	case ProviderClaude, ProviderAnthropic:
		return anthropic.ModelAliases
	case ProviderOpenAI:
		return openai.ModelAliases
	case ProviderChatGPT:
		return openai.CodexModelAliases
	case ProviderBedrock:
		return bedrock.ModelAliases
	case ProviderMiniMax:
		return minimax.ModelAliases
	default:
		return nil
	}
}
