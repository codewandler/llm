package bedrock

import "github.com/codewandler/llm"

// modelInfo contains metadata and pricing for a Bedrock model.
type modelInfo struct {
	ID          string  // Bedrock model ID
	Name        string  // Human-readable name
	InputPrice  float64 // USD per 1M input tokens
	OutputPrice float64 // USD per 1M output tokens
}

// modelRegistry maps model IDs to their info.
// Pricing data from AWS Bedrock pricing page.
var modelRegistry = map[string]modelInfo{
	// Anthropic Claude models
	"anthropic.claude-sonnet-4-5-20250929-v1:0": {ID: "anthropic.claude-sonnet-4-5-20250929-v1:0", Name: "Claude Sonnet 4.5", InputPrice: 3.00, OutputPrice: 15.00},
	"anthropic.claude-sonnet-4-20250514-v1:0":   {ID: "anthropic.claude-sonnet-4-20250514-v1:0", Name: "Claude Sonnet 4", InputPrice: 3.00, OutputPrice: 15.00},
	"anthropic.claude-3-5-sonnet-20241022-v2:0": {ID: "anthropic.claude-3-5-sonnet-20241022-v2:0", Name: "Claude 3.5 Sonnet v2", InputPrice: 3.00, OutputPrice: 15.00},
	"anthropic.claude-3-5-sonnet-20240620-v1:0": {ID: "anthropic.claude-3-5-sonnet-20240620-v1:0", Name: "Claude 3.5 Sonnet", InputPrice: 3.00, OutputPrice: 15.00},
	"anthropic.claude-3-5-haiku-20241022-v1:0":  {ID: "anthropic.claude-3-5-haiku-20241022-v1:0", Name: "Claude 3.5 Haiku", InputPrice: 0.80, OutputPrice: 4.00},
	"anthropic.claude-3-opus-20240229-v1:0":     {ID: "anthropic.claude-3-opus-20240229-v1:0", Name: "Claude 3 Opus", InputPrice: 15.00, OutputPrice: 75.00},
	"anthropic.claude-3-sonnet-20240229-v1:0":   {ID: "anthropic.claude-3-sonnet-20240229-v1:0", Name: "Claude 3 Sonnet", InputPrice: 3.00, OutputPrice: 15.00},
	"anthropic.claude-3-haiku-20240307-v1:0":    {ID: "anthropic.claude-3-haiku-20240307-v1:0", Name: "Claude 3 Haiku", InputPrice: 0.25, OutputPrice: 1.25},

	// Meta Llama models
	"meta.llama3-3-70b-instruct-v1:0":  {ID: "meta.llama3-3-70b-instruct-v1:0", Name: "Llama 3.3 70B Instruct", InputPrice: 0.72, OutputPrice: 0.72},
	"meta.llama3-2-90b-instruct-v1:0":  {ID: "meta.llama3-2-90b-instruct-v1:0", Name: "Llama 3.2 90B Instruct", InputPrice: 0.72, OutputPrice: 0.72},
	"meta.llama3-2-11b-instruct-v1:0":  {ID: "meta.llama3-2-11b-instruct-v1:0", Name: "Llama 3.2 11B Instruct", InputPrice: 0.16, OutputPrice: 0.16},
	"meta.llama3-2-3b-instruct-v1:0":   {ID: "meta.llama3-2-3b-instruct-v1:0", Name: "Llama 3.2 3B Instruct", InputPrice: 0.15, OutputPrice: 0.15},
	"meta.llama3-2-1b-instruct-v1:0":   {ID: "meta.llama3-2-1b-instruct-v1:0", Name: "Llama 3.2 1B Instruct", InputPrice: 0.10, OutputPrice: 0.10},
	"meta.llama3-1-405b-instruct-v1:0": {ID: "meta.llama3-1-405b-instruct-v1:0", Name: "Llama 3.1 405B Instruct", InputPrice: 2.40, OutputPrice: 2.40},
	"meta.llama3-1-70b-instruct-v1:0":  {ID: "meta.llama3-1-70b-instruct-v1:0", Name: "Llama 3.1 70B Instruct", InputPrice: 0.72, OutputPrice: 0.72},
	"meta.llama3-1-8b-instruct-v1:0":   {ID: "meta.llama3-1-8b-instruct-v1:0", Name: "Llama 3.1 8B Instruct", InputPrice: 0.22, OutputPrice: 0.22},

	// Amazon Nova models
	"amazon.nova-pro-v1:0":   {ID: "amazon.nova-pro-v1:0", Name: "Amazon Nova Pro", InputPrice: 0.80, OutputPrice: 3.20},
	"amazon.nova-lite-v1:0":  {ID: "amazon.nova-lite-v1:0", Name: "Amazon Nova Lite", InputPrice: 0.06, OutputPrice: 0.24},
	"amazon.nova-micro-v1:0": {ID: "amazon.nova-micro-v1:0", Name: "Amazon Nova Micro", InputPrice: 0.035, OutputPrice: 0.14},

	// Mistral models
	"mistral.mistral-large-2407-v1:0":    {ID: "mistral.mistral-large-2407-v1:0", Name: "Mistral Large (24.07)", InputPrice: 2.00, OutputPrice: 6.00},
	"mistral.mistral-large-2402-v1:0":    {ID: "mistral.mistral-large-2402-v1:0", Name: "Mistral Large (24.02)", InputPrice: 4.00, OutputPrice: 12.00},
	"mistral.mistral-small-2402-v1:0":    {ID: "mistral.mistral-small-2402-v1:0", Name: "Mistral Small", InputPrice: 0.10, OutputPrice: 0.30},
	"mistral.mixtral-8x7b-instruct-v0:1": {ID: "mistral.mixtral-8x7b-instruct-v0:1", Name: "Mixtral 8x7B Instruct", InputPrice: 0.45, OutputPrice: 0.70},

	// Cohere models
	"cohere.command-r-plus-v1:0": {ID: "cohere.command-r-plus-v1:0", Name: "Command R+", InputPrice: 2.50, OutputPrice: 10.00},
	"cohere.command-r-v1:0":      {ID: "cohere.command-r-v1:0", Name: "Command R", InputPrice: 0.15, OutputPrice: 0.60},
}

// modelOrder defines the display order for Models().
var modelOrder = []string{
	// Anthropic Claude (newest first)
	"anthropic.claude-sonnet-4-5-20250929-v1:0",
	"anthropic.claude-sonnet-4-20250514-v1:0",
	"anthropic.claude-3-5-sonnet-20241022-v2:0",
	"anthropic.claude-3-5-haiku-20241022-v1:0",
	"anthropic.claude-3-opus-20240229-v1:0",

	// Meta Llama
	"meta.llama3-3-70b-instruct-v1:0",
	"meta.llama3-2-90b-instruct-v1:0",
	"meta.llama3-1-405b-instruct-v1:0",

	// Amazon Nova
	"amazon.nova-pro-v1:0",
	"amazon.nova-lite-v1:0",
	"amazon.nova-micro-v1:0",

	// Mistral
	"mistral.mistral-large-2407-v1:0",
	"mistral.mistral-small-2402-v1:0",

	// Cohere
	"cohere.command-r-plus-v1:0",
	"cohere.command-r-v1:0",
}

// calculateCost computes the cost in USD for the given usage and model.
// Returns 0 if the model is unknown.
func calculateCost(model string, usage *llm.Usage) float64 {
	if usage == nil {
		return 0
	}

	info, ok := modelRegistry[model]
	if !ok {
		return 0 // unknown model, can't calculate cost
	}

	cost := (float64(usage.InputTokens) / 1_000_000) * info.InputPrice
	cost += (float64(usage.OutputTokens) / 1_000_000) * info.OutputPrice

	return cost
}
