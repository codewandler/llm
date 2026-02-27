package bedrock

import (
	"strings"

	"github.com/codewandler/llm"
)

// modelInfo contains metadata and pricing for a Bedrock model.
type modelInfo struct {
	ID          string  // Bedrock model ID
	Name        string  // Human-readable name
	InputPrice  float64 // USD per 1M input tokens (0 if unknown)
	OutputPrice float64 // USD per 1M output tokens (0 if unknown)
}

// modelRegistry maps model IDs to their info.
// Pricing data from Anthropic pricing page and AWS Bedrock pricing (us-east-1 region).
// Models with 0 pricing have unknown/unconfirmed pricing.
var modelRegistry = map[string]modelInfo{
	// --- Anthropic Claude models ---
	// Pricing from https://www.anthropic.com/pricing (API section)

	// Claude 4.x series
	"anthropic.claude-opus-4-6-v1":              {ID: "anthropic.claude-opus-4-6-v1", Name: "Claude Opus 4.6", InputPrice: 5.00, OutputPrice: 25.00},
	"anthropic.claude-sonnet-4-6":               {ID: "anthropic.claude-sonnet-4-6", Name: "Claude Sonnet 4.6", InputPrice: 3.00, OutputPrice: 15.00},
	"anthropic.claude-opus-4-5-20251101-v1:0":   {ID: "anthropic.claude-opus-4-5-20251101-v1:0", Name: "Claude Opus 4.5", InputPrice: 5.00, OutputPrice: 25.00},
	"anthropic.claude-sonnet-4-5-20250929-v1:0": {ID: "anthropic.claude-sonnet-4-5-20250929-v1:0", Name: "Claude Sonnet 4.5", InputPrice: 3.00, OutputPrice: 15.00},
	"anthropic.claude-haiku-4-5-20251001-v1:0":  {ID: "anthropic.claude-haiku-4-5-20251001-v1:0", Name: "Claude Haiku 4.5", InputPrice: 1.00, OutputPrice: 5.00},
	"anthropic.claude-opus-4-1-20250805-v1:0":   {ID: "anthropic.claude-opus-4-1-20250805-v1:0", Name: "Claude Opus 4.1", InputPrice: 15.00, OutputPrice: 75.00},
	"anthropic.claude-opus-4-20250514-v1:0":     {ID: "anthropic.claude-opus-4-20250514-v1:0", Name: "Claude Opus 4", InputPrice: 15.00, OutputPrice: 75.00},
	"anthropic.claude-sonnet-4-20250514-v1:0":   {ID: "anthropic.claude-sonnet-4-20250514-v1:0", Name: "Claude Sonnet 4", InputPrice: 3.00, OutputPrice: 15.00},

	// Claude 3.x series
	"anthropic.claude-3-7-sonnet-20250219-v1:0": {ID: "anthropic.claude-3-7-sonnet-20250219-v1:0", Name: "Claude 3.7 Sonnet", InputPrice: 3.00, OutputPrice: 15.00},
	"anthropic.claude-3-5-sonnet-20241022-v2:0": {ID: "anthropic.claude-3-5-sonnet-20241022-v2:0", Name: "Claude 3.5 Sonnet v2", InputPrice: 3.00, OutputPrice: 15.00},
	"anthropic.claude-3-5-sonnet-20240620-v1:0": {ID: "anthropic.claude-3-5-sonnet-20240620-v1:0", Name: "Claude 3.5 Sonnet", InputPrice: 3.00, OutputPrice: 15.00},
	"anthropic.claude-3-5-haiku-20241022-v1:0":  {ID: "anthropic.claude-3-5-haiku-20241022-v1:0", Name: "Claude 3.5 Haiku", InputPrice: 0.80, OutputPrice: 4.00},
	"anthropic.claude-3-opus-20240229-v1:0":     {ID: "anthropic.claude-3-opus-20240229-v1:0", Name: "Claude 3 Opus", InputPrice: 15.00, OutputPrice: 75.00},
	"anthropic.claude-3-sonnet-20240229-v1:0":   {ID: "anthropic.claude-3-sonnet-20240229-v1:0", Name: "Claude 3 Sonnet", InputPrice: 3.00, OutputPrice: 15.00},
	"anthropic.claude-3-haiku-20240307-v1:0":    {ID: "anthropic.claude-3-haiku-20240307-v1:0", Name: "Claude 3 Haiku", InputPrice: 0.25, OutputPrice: 1.25},

	// --- Meta Llama models ---
	// Pricing from AWS Bedrock pricing page

	// Llama 4 - pricing based on similar model class
	"meta.llama4-maverick-17b-instruct-v1:0": {ID: "meta.llama4-maverick-17b-instruct-v1:0", Name: "Llama 4 Maverick 17B", InputPrice: 0.22, OutputPrice: 0.88},
	"meta.llama4-scout-17b-instruct-v1:0":    {ID: "meta.llama4-scout-17b-instruct-v1:0", Name: "Llama 4 Scout 17B", InputPrice: 0.22, OutputPrice: 0.88},

	// Llama 3.3
	"meta.llama3-3-70b-instruct-v1:0": {ID: "meta.llama3-3-70b-instruct-v1:0", Name: "Llama 3.3 70B Instruct", InputPrice: 0.72, OutputPrice: 0.72},

	// Llama 3.2
	"meta.llama3-2-90b-instruct-v1:0": {ID: "meta.llama3-2-90b-instruct-v1:0", Name: "Llama 3.2 90B Instruct", InputPrice: 0.72, OutputPrice: 0.72},
	"meta.llama3-2-11b-instruct-v1:0": {ID: "meta.llama3-2-11b-instruct-v1:0", Name: "Llama 3.2 11B Instruct", InputPrice: 0.16, OutputPrice: 0.16},
	"meta.llama3-2-3b-instruct-v1:0":  {ID: "meta.llama3-2-3b-instruct-v1:0", Name: "Llama 3.2 3B Instruct", InputPrice: 0.15, OutputPrice: 0.15},
	"meta.llama3-2-1b-instruct-v1:0":  {ID: "meta.llama3-2-1b-instruct-v1:0", Name: "Llama 3.2 1B Instruct", InputPrice: 0.10, OutputPrice: 0.10},

	// Llama 3.1
	"meta.llama3-1-70b-instruct-v1:0": {ID: "meta.llama3-1-70b-instruct-v1:0", Name: "Llama 3.1 70B Instruct", InputPrice: 0.72, OutputPrice: 0.72},
	"meta.llama3-1-8b-instruct-v1:0":  {ID: "meta.llama3-1-8b-instruct-v1:0", Name: "Llama 3.1 8B Instruct", InputPrice: 0.22, OutputPrice: 0.22},

	// Llama 3
	"meta.llama3-70b-instruct-v1:0": {ID: "meta.llama3-70b-instruct-v1:0", Name: "Llama 3 70B Instruct", InputPrice: 2.65, OutputPrice: 3.50},
	"meta.llama3-8b-instruct-v1:0":  {ID: "meta.llama3-8b-instruct-v1:0", Name: "Llama 3 8B Instruct", InputPrice: 0.30, OutputPrice: 0.60},

	// --- Amazon Nova models ---
	// Pricing from AWS Bedrock pricing page
	"amazon.nova-premier-v1:0": {ID: "amazon.nova-premier-v1:0", Name: "Amazon Nova Premier", InputPrice: 2.50, OutputPrice: 10.00},
	"amazon.nova-pro-v1:0":     {ID: "amazon.nova-pro-v1:0", Name: "Amazon Nova Pro", InputPrice: 0.80, OutputPrice: 3.20},
	"amazon.nova-2-lite-v1:0":  {ID: "amazon.nova-2-lite-v1:0", Name: "Amazon Nova 2 Lite", InputPrice: 0.06, OutputPrice: 0.24},
	"amazon.nova-lite-v1:0":    {ID: "amazon.nova-lite-v1:0", Name: "Amazon Nova Lite", InputPrice: 0.06, OutputPrice: 0.24},
	"amazon.nova-micro-v1:0":   {ID: "amazon.nova-micro-v1:0", Name: "Amazon Nova Micro", InputPrice: 0.035, OutputPrice: 0.14},

	// --- Mistral models ---
	// Pricing from AWS Bedrock pricing page
	"mistral.mistral-large-3-675b-instruct": {ID: "mistral.mistral-large-3-675b-instruct", Name: "Mistral Large 3", InputPrice: 0.50, OutputPrice: 1.50},
	"mistral.pixtral-large-2502-v1:0":       {ID: "mistral.pixtral-large-2502-v1:0", Name: "Pixtral Large", InputPrice: 0.50, OutputPrice: 1.50},
	"mistral.devstral-2-123b":               {ID: "mistral.devstral-2-123b", Name: "Devstral 2", InputPrice: 0.40, OutputPrice: 2.00},
	"mistral.magistral-small-2509":          {ID: "mistral.magistral-small-2509", Name: "Magistral Small", InputPrice: 0.50, OutputPrice: 1.50},
	"mistral.ministral-3-14b-instruct":      {ID: "mistral.ministral-3-14b-instruct", Name: "Ministral 14B", InputPrice: 0.20, OutputPrice: 0.20},
	"mistral.ministral-3-8b-instruct":       {ID: "mistral.ministral-3-8b-instruct", Name: "Ministral 8B", InputPrice: 0.15, OutputPrice: 0.15},
	"mistral.ministral-3-3b-instruct":       {ID: "mistral.ministral-3-3b-instruct", Name: "Ministral 3B", InputPrice: 0.10, OutputPrice: 0.10},
	"mistral.voxtral-small-24b-2507":        {ID: "mistral.voxtral-small-24b-2507", Name: "Voxtral Small", InputPrice: 0.10, OutputPrice: 0.30},
	"mistral.voxtral-mini-3b-2507":          {ID: "mistral.voxtral-mini-3b-2507", Name: "Voxtral Mini", InputPrice: 0.04, OutputPrice: 0.04},
	"mistral.mistral-large-2402-v1:0":       {ID: "mistral.mistral-large-2402-v1:0", Name: "Mistral Large (24.02)", InputPrice: 4.00, OutputPrice: 12.00},
	"mistral.mistral-small-2402-v1:0":       {ID: "mistral.mistral-small-2402-v1:0", Name: "Mistral Small", InputPrice: 0.10, OutputPrice: 0.30},
	"mistral.mixtral-8x7b-instruct-v0:1":    {ID: "mistral.mixtral-8x7b-instruct-v0:1", Name: "Mixtral 8x7B Instruct", InputPrice: 0.45, OutputPrice: 0.70},
	"mistral.mistral-7b-instruct-v0:2":      {ID: "mistral.mistral-7b-instruct-v0:2", Name: "Mistral 7B Instruct", InputPrice: 0.15, OutputPrice: 0.20},

	// --- Cohere models ---
	"cohere.command-r-plus-v1:0": {ID: "cohere.command-r-plus-v1:0", Name: "Command R+", InputPrice: 2.50, OutputPrice: 10.00},
	"cohere.command-r-v1:0":      {ID: "cohere.command-r-v1:0", Name: "Command R", InputPrice: 0.15, OutputPrice: 0.60},
}

// modelOrder defines the display order for Models().
var modelOrder = []string{
	// Anthropic Claude (newest first)
	"anthropic.claude-opus-4-6-v1",
	"anthropic.claude-sonnet-4-6",
	"anthropic.claude-opus-4-5-20251101-v1:0",
	"anthropic.claude-sonnet-4-5-20250929-v1:0",
	"anthropic.claude-haiku-4-5-20251001-v1:0",
	"anthropic.claude-opus-4-1-20250805-v1:0",
	"anthropic.claude-opus-4-20250514-v1:0",
	"anthropic.claude-sonnet-4-20250514-v1:0",
	"anthropic.claude-3-7-sonnet-20250219-v1:0",
	"anthropic.claude-3-5-sonnet-20241022-v2:0",
	"anthropic.claude-3-5-sonnet-20240620-v1:0",
	"anthropic.claude-3-5-haiku-20241022-v1:0",
	"anthropic.claude-3-opus-20240229-v1:0",
	"anthropic.claude-3-sonnet-20240229-v1:0",
	"anthropic.claude-3-haiku-20240307-v1:0",

	// Meta Llama (newest first)
	"meta.llama4-maverick-17b-instruct-v1:0",
	"meta.llama4-scout-17b-instruct-v1:0",
	"meta.llama3-3-70b-instruct-v1:0",
	"meta.llama3-2-90b-instruct-v1:0",
	"meta.llama3-2-11b-instruct-v1:0",
	"meta.llama3-2-3b-instruct-v1:0",
	"meta.llama3-2-1b-instruct-v1:0",
	"meta.llama3-1-70b-instruct-v1:0",
	"meta.llama3-1-8b-instruct-v1:0",
	"meta.llama3-70b-instruct-v1:0",
	"meta.llama3-8b-instruct-v1:0",

	// Amazon Nova
	"amazon.nova-premier-v1:0",
	"amazon.nova-pro-v1:0",
	"amazon.nova-2-lite-v1:0",
	"amazon.nova-lite-v1:0",
	"amazon.nova-micro-v1:0",

	// Mistral
	"mistral.mistral-large-3-675b-instruct",
	"mistral.pixtral-large-2502-v1:0",
	"mistral.devstral-2-123b",
	"mistral.magistral-small-2509",
	"mistral.ministral-3-14b-instruct",
	"mistral.ministral-3-8b-instruct",
	"mistral.ministral-3-3b-instruct",
	"mistral.voxtral-small-24b-2507",
	"mistral.voxtral-mini-3b-2507",
	"mistral.mistral-large-2402-v1:0",
	"mistral.mistral-small-2402-v1:0",
	"mistral.mixtral-8x7b-instruct-v0:1",
	"mistral.mistral-7b-instruct-v0:2",

	// Cohere
	"cohere.command-r-plus-v1:0",
	"cohere.command-r-v1:0",
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
		// Check if prefix is a region indicator (us, eu, global, etc.)
		prefix := model[:idx]
		if prefix == "us" || prefix == "eu" || prefix == "global" || prefix == "ap" {
			modelID = model[idx+1:]
		}
	}

	info, ok := modelRegistry[modelID]
	if !ok {
		return 0 // unknown model, can't calculate cost
	}

	cost := (float64(usage.InputTokens) / 1_000_000) * info.InputPrice
	cost += (float64(usage.OutputTokens) / 1_000_000) * info.OutputPrice

	return cost
}
