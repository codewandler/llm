package bedrock

import (
	"strings"

	"github.com/codewandler/llm"
)

// -----------------------------------------------------------------------------
// Model ID Constants
// -----------------------------------------------------------------------------
// Exported constants for all Bedrock model IDs, organized by provider.

// Anthropic Claude models.
const (
	// Claude latest (recommended)
	ModelHaikuLatest  = "anthropic.claude-haiku-4-5-20251001-v1:0"
	ModelOpusLatest   = "anthropic.claude-opus-4-6-v1"
	ModelSonnetLatest = "anthropic.claude-sonnet-4-6"

	// Claude 4.5 series
	ModelOpus45   = "anthropic.claude-opus-4-5-20251101-v1:0"
	ModelSonnet45 = "anthropic.claude-sonnet-4-5-20250929-v1:0"

	// Claude 3.x series
	ModelHaiku3   = "anthropic.claude-3-haiku-20240307-v1:0"
	ModelSonnet35 = "anthropic.claude-3-5-sonnet-20240620-v1:0"
	ModelSonnet37 = "anthropic.claude-3-7-sonnet-20250219-v1:0"
)

// Amazon Nova models.
const (
	ModelNova2Lite   = "amazon.nova-2-lite-v1:0"
	ModelNovaLite    = "amazon.nova-lite-v1:0"
	ModelNovaMicro   = "amazon.nova-micro-v1:0"
	ModelNovaPremier = "amazon.nova-premier-v1:0"
	ModelNovaPro     = "amazon.nova-pro-v1:0"
)

// Cohere models.
const (
	ModelCohereEmbedV4 = "cohere.embed-v4:0"
	ModelCommandR      = "cohere.command-r-v1:0"
	ModelCommandRPlus  = "cohere.command-r-plus-v1:0"
)

// DeepSeek models.
const (
	ModelDeepSeekR1 = "deepseek.r1-v1:0"
)

// Meta Llama models.
const (
	// Llama 4
	ModelLlama4Maverick = "meta.llama4-maverick-17b-instruct-v1:0"
	ModelLlama4Scout    = "meta.llama4-scout-17b-instruct-v1:0"

	// Llama 3.3
	ModelLlama33_70B = "meta.llama3-3-70b-instruct-v1:0"

	// Llama 3.2
	ModelLlama32_1B  = "meta.llama3-2-1b-instruct-v1:0"
	ModelLlama32_3B  = "meta.llama3-2-3b-instruct-v1:0"
	ModelLlama32_11B = "meta.llama3-2-11b-instruct-v1:0"
	ModelLlama32_90B = "meta.llama3-2-90b-instruct-v1:0"

	// Llama 3.1
	ModelLlama31_8B  = "meta.llama3-1-8b-instruct-v1:0"
	ModelLlama31_70B = "meta.llama3-1-70b-instruct-v1:0"

	// Llama 3
	ModelLlama3_8B  = "meta.llama3-8b-instruct-v1:0"
	ModelLlama3_70B = "meta.llama3-70b-instruct-v1:0"
)

// Mistral models.
const (
	ModelDevstral2        = "mistral.devstral-2-123b"
	ModelMagistralSmall   = "mistral.magistral-small-2509"
	ModelMinistral3B      = "mistral.ministral-3-3b-instruct"
	ModelMinistral8B      = "mistral.ministral-3-8b-instruct"
	ModelMinistral14B     = "mistral.ministral-3-14b-instruct"
	ModelMistral7B        = "mistral.mistral-7b-instruct-v0:2"
	ModelMistralLarge2402 = "mistral.mistral-large-2402-v1:0"
	ModelMistralLarge3    = "mistral.mistral-large-3-675b-instruct"
	ModelMistralSmall     = "mistral.mistral-small-2402-v1:0"
	ModelMixtral8x7B      = "mistral.mixtral-8x7b-instruct-v0:1"
	ModelPixtralLarge     = "mistral.pixtral-large-2502-v1:0"
	ModelVoxtralMini      = "mistral.voxtral-mini-3b-2507"
	ModelVoxtralSmall     = "mistral.voxtral-small-24b-2507"
)

// Writer models.
const (
	ModelPalmyraX4 = "writer.palmyra-x4-v1:0"
	ModelPalmyraX5 = "writer.palmyra-x5-v1:0"
)

// -----------------------------------------------------------------------------
// Model Registry (Single Source of Truth)
// -----------------------------------------------------------------------------

// modelDef defines a single model with all its metadata.
type modelDef struct {
	ID               string   // Bedrock model ID (use constants above)
	Name             string   // Human-readable name
	InputPrice       float64  // USD per 1M input tokens (0 if unknown)
	OutputPrice      float64  // USD per 1M output tokens (0 if unknown)
	CachedInputPrice float64  // USD per 1M cache read tokens (~0.1× input)
	CacheWritePrice  float64  // USD per 1M cache write tokens (~1.25× input)
	Prefixes         []string // Inference profile prefixes (nil = no profile)
}

// Common prefix combinations for inference profiles.
var (
	prefixesEUUSGlobal = []string{PrefixEU, PrefixUS, PrefixGlobal}
	prefixesEUUSAPAC   = []string{PrefixEU, PrefixUS, PrefixAPAC}
	prefixesEUUS       = []string{PrefixEU, PrefixUS}
	prefixesUSOnly     = []string{PrefixUS}
)

// allModels is the single source of truth for all Bedrock models.
// Order defines display order in Models().
// Pricing data from AWS Bedrock pricing page (us-east-1 region).
var allModels = []modelDef{
	// -------------------------------------------------------------------------
	// Anthropic Claude (cache pricing: read=0.1×input, write=1.25×input)
	// -------------------------------------------------------------------------
	{ModelOpusLatest, "Claude Opus 4.6", 5.00, 25.00, 0.50, 6.25, prefixesEUUSGlobal},
	{ModelSonnetLatest, "Claude Sonnet 4.6", 3.00, 15.00, 0.30, 3.75, prefixesEUUSGlobal},
	{ModelHaikuLatest, "Claude Haiku 4.5", 1.00, 5.00, 0.10, 1.25, prefixesEUUSGlobal},
	{ModelOpus45, "Claude Opus 4.5", 5.00, 25.00, 0.50, 6.25, prefixesEUUSGlobal},
	{ModelSonnet45, "Claude Sonnet 4.5", 3.00, 15.00, 0.30, 3.75, prefixesEUUSGlobal},
	{ModelSonnet37, "Claude 3.7 Sonnet", 3.00, 15.00, 0.30, 3.75, prefixesEUUSAPAC},
	{ModelSonnet35, "Claude 3.5 Sonnet", 3.00, 15.00, 0.30, 3.75, prefixesEUUSAPAC},
	{ModelHaiku3, "Claude 3 Haiku", 0.25, 1.25, 0.025, 0.3125, prefixesEUUSAPAC},

	// -------------------------------------------------------------------------
	// Amazon Nova
	// -------------------------------------------------------------------------
	{ModelNovaPremier, "Amazon Nova Premier", 2.50, 10.00, 0, 0, prefixesUSOnly},
	{ModelNovaPro, "Amazon Nova Pro", 0.80, 3.20, 0, 0, prefixesEUUSAPAC},
	{ModelNova2Lite, "Amazon Nova 2 Lite", 0.06, 0.24, 0, 0, prefixesEUUSGlobal},
	{ModelNovaLite, "Amazon Nova Lite", 0.06, 0.24, 0, 0, prefixesEUUSAPAC},
	{ModelNovaMicro, "Amazon Nova Micro", 0.035, 0.14, 0, 0, prefixesEUUSAPAC},

	// -------------------------------------------------------------------------
	// Cohere
	// -------------------------------------------------------------------------
	{ModelCommandRPlus, "Command R+", 2.50, 10.00, 0, 0, nil},
	{ModelCommandR, "Command R", 0.15, 0.60, 0, 0, nil},
	{ModelCohereEmbedV4, "Cohere Embed v4", 0.10, 0, 0, 0, prefixesEUUSGlobal},

	// -------------------------------------------------------------------------
	// DeepSeek
	// -------------------------------------------------------------------------
	{ModelDeepSeekR1, "DeepSeek R1", 1.35, 5.40, 0, 0, prefixesUSOnly},

	// -------------------------------------------------------------------------
	// Meta Llama
	// -------------------------------------------------------------------------
	{ModelLlama4Maverick, "Llama 4 Maverick 17B", 0.22, 0.88, 0, 0, prefixesUSOnly},
	{ModelLlama4Scout, "Llama 4 Scout 17B", 0.22, 0.88, 0, 0, prefixesUSOnly},
	{ModelLlama33_70B, "Llama 3.3 70B Instruct", 0.72, 0.72, 0, 0, prefixesUSOnly},
	{ModelLlama32_90B, "Llama 3.2 90B Instruct", 0.72, 0.72, 0, 0, prefixesUSOnly},
	{ModelLlama32_11B, "Llama 3.2 11B Instruct", 0.16, 0.16, 0, 0, prefixesUSOnly},
	{ModelLlama32_3B, "Llama 3.2 3B Instruct", 0.15, 0.15, 0, 0, prefixesEUUS},
	{ModelLlama32_1B, "Llama 3.2 1B Instruct", 0.10, 0.10, 0, 0, prefixesEUUS},
	{ModelLlama31_70B, "Llama 3.1 70B Instruct", 0.72, 0.72, 0, 0, prefixesUSOnly},
	{ModelLlama31_8B, "Llama 3.1 8B Instruct", 0.22, 0.22, 0, 0, prefixesUSOnly},
	{ModelLlama3_70B, "Llama 3 70B Instruct", 2.65, 3.50, 0, 0, nil},
	{ModelLlama3_8B, "Llama 3 8B Instruct", 0.30, 0.60, 0, 0, nil},

	// -------------------------------------------------------------------------
	// Mistral
	// -------------------------------------------------------------------------
	{ModelMistralLarge3, "Mistral Large 3", 0.50, 1.50, 0, 0, nil},
	{ModelPixtralLarge, "Pixtral Large", 0.50, 1.50, 0, 0, prefixesEUUS},
	{ModelDevstral2, "Devstral 2", 0.40, 2.00, 0, 0, nil},
	{ModelMagistralSmall, "Magistral Small", 0.50, 1.50, 0, 0, nil},
	{ModelMinistral14B, "Ministral 14B", 0.20, 0.20, 0, 0, nil},
	{ModelMinistral8B, "Ministral 8B", 0.15, 0.15, 0, 0, nil},
	{ModelMinistral3B, "Ministral 3B", 0.10, 0.10, 0, 0, nil},
	{ModelVoxtralSmall, "Voxtral Small", 0.10, 0.30, 0, 0, nil},
	{ModelVoxtralMini, "Voxtral Mini", 0.04, 0.04, 0, 0, nil},
	{ModelMistralLarge2402, "Mistral Large (24.02)", 4.00, 12.00, 0, 0, nil},
	{ModelMistralSmall, "Mistral Small", 0.10, 0.30, 0, 0, nil},
	{ModelMixtral8x7B, "Mixtral 8x7B Instruct", 0.45, 0.70, 0, 0, nil},
	{ModelMistral7B, "Mistral 7B Instruct", 0.15, 0.20, 0, 0, nil},

	// -------------------------------------------------------------------------
	// Writer
	// -------------------------------------------------------------------------
	{ModelPalmyraX4, "Palmyra X4", 2.50, 10.00, 0, 0, prefixesUSOnly},
	{ModelPalmyraX5, "Palmyra X5", 0.60, 6.00, 0, 0, prefixesUSOnly},
}

// -----------------------------------------------------------------------------
// Derived Data Structures
// -----------------------------------------------------------------------------

// modelInfo contains metadata and pricing for a Bedrock model.
type modelInfo struct {
	ID               string  // Bedrock model ID
	Name             string  // Human-readable name
	InputPrice       float64 // USD per 1M input tokens (0 if unknown)
	OutputPrice      float64 // USD per 1M output tokens (0 if unknown)
	CachedInputPrice float64 // USD per 1M cache read tokens (~0.1× input)
	CacheWritePrice  float64 // USD per 1M cache write tokens (~1.25× input)
}

// modelRegistry maps model IDs to their info (derived from allModels).
var modelRegistry map[string]modelInfo

// inferenceProfiles maps model IDs to their inference profiles (derived from allModels).
var inferenceProfiles map[string]InferenceProfile

func init() {
	modelRegistry = make(map[string]modelInfo, len(allModels))
	inferenceProfiles = make(map[string]InferenceProfile)

	for _, m := range allModels {
		modelRegistry[m.ID] = modelInfo{
			ID:               m.ID,
			Name:             m.Name,
			InputPrice:       m.InputPrice,
			OutputPrice:      m.OutputPrice,
			CachedInputPrice: m.CachedInputPrice,
			CacheWritePrice:  m.CacheWritePrice,
		}
		if len(m.Prefixes) > 0 {
			inferenceProfiles[m.ID] = InferenceProfile{Prefixes: m.Prefixes}
		}
	}
}

// models returns all models in display order.
func models() []llm.Model {
	result := make([]llm.Model, 0, len(allModels))
	for _, m := range allModels {
		result = append(result, llm.Model{
			ID:       m.ID,
			Name:     m.Name,
			Provider: providerName,
		})
	}
	return result
}

// calculateCost computes the cost in USD for the given usage and model.
// Returns 0 if the model is unknown or has no pricing data.
func calculateCost(model string, usage *llm.Usage) float64 {
	if usage == nil {
		return 0
	}

	// Strip inference profile prefix (us., eu., global., etc.) to match registry
	modelID := model
	if idx := strings.Index(model, "."); idx != -1 {
		// Check if prefix is a region indicator (us, eu, global, apac, etc.)
		prefix := model[:idx]
		if prefix == PrefixUS || prefix == PrefixEU || prefix == PrefixGlobal || prefix == PrefixAPAC || prefix == "ap" {
			modelID = model[idx+1:]
		}
	}

	info, ok := modelRegistry[modelID]
	if !ok {
		return 0 // unknown model, can't calculate cost
	}

	// Regular input = total input minus cached (read) and written-to-cache tokens
	regularInput := usage.InputTokens - usage.CachedTokens - usage.CacheWriteTokens
	if regularInput < 0 {
		regularInput = 0
	}

	cost := (float64(regularInput) / 1_000_000) * info.InputPrice
	cost += (float64(usage.CachedTokens) / 1_000_000) * info.CachedInputPrice
	cost += (float64(usage.CacheWriteTokens) / 1_000_000) * info.CacheWritePrice
	cost += (float64(usage.OutputTokens) / 1_000_000) * info.OutputPrice

	return cost
}
