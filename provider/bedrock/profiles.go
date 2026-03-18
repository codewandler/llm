package bedrock

// InferenceProfile defines available region prefixes for a model.
type InferenceProfile struct {
	Prefixes []string // Available: "eu", "us", "apac", "global"
}

// inferenceProfiles maps base model IDs to their available inference profiles.
//
// To update this list, run the following commands and merge the results:
//
//	aws bedrock list-inference-profiles --region us-east-1 --output json | \
//	  jq -r '.inferenceProfileSummaries[].inferenceProfileId'
//	aws bedrock list-inference-profiles --region eu-central-1 --output json | \
//	  jq -r '.inferenceProfileSummaries[].inferenceProfileId'
//	aws bedrock list-inference-profiles --region ap-northeast-1 --output json | \
//	  jq -r '.inferenceProfileSummaries[].inferenceProfileId'
//
// Last updated: 2026-03-18
var inferenceProfiles = map[string]InferenceProfile{
	// ========================================
	// Anthropic Claude 4.6
	// ========================================
	"anthropic.claude-opus-4-6-v1": {Prefixes: []string{"eu", "us", "global"}},
	"anthropic.claude-sonnet-4-6":  {Prefixes: []string{"eu", "us", "global"}},

	// ========================================
	// Anthropic Claude 4.5
	// ========================================
	"anthropic.claude-opus-4-5-20251101-v1:0":   {Prefixes: []string{"eu", "us", "global"}},
	"anthropic.claude-sonnet-4-5-20250929-v1:0": {Prefixes: []string{"eu", "us", "global"}},
	"anthropic.claude-haiku-4-5-20251001-v1:0":  {Prefixes: []string{"eu", "us", "global"}},

	// ========================================
	// Anthropic Claude 3.7
	// ========================================
	"anthropic.claude-3-7-sonnet-20250219-v1:0": {Prefixes: []string{"eu", "us", "apac"}},

	// ========================================
	// Anthropic Claude 3.5
	// ========================================
	"anthropic.claude-3-5-sonnet-20240620-v1:0": {Prefixes: []string{"eu", "us", "apac"}},

	// ========================================
	// Anthropic Claude 3.0
	// ========================================
	"anthropic.claude-3-haiku-20240307-v1:0": {Prefixes: []string{"eu", "us", "apac"}},

	// ========================================
	// Meta Llama 4
	// ========================================
	"meta.llama4-maverick-17b-instruct-v1:0": {Prefixes: []string{"us"}},
	"meta.llama4-scout-17b-instruct-v1:0":    {Prefixes: []string{"us"}},

	// ========================================
	// Meta Llama 3.3
	// ========================================
	"meta.llama3-3-70b-instruct-v1:0": {Prefixes: []string{"us"}},

	// ========================================
	// Meta Llama 3.2
	// ========================================
	"meta.llama3-2-90b-instruct-v1:0": {Prefixes: []string{"us"}},
	"meta.llama3-2-11b-instruct-v1:0": {Prefixes: []string{"us"}},
	"meta.llama3-2-3b-instruct-v1:0":  {Prefixes: []string{"eu", "us"}},
	"meta.llama3-2-1b-instruct-v1:0":  {Prefixes: []string{"eu", "us"}},

	// ========================================
	// Meta Llama 3.1
	// ========================================
	"meta.llama3-1-70b-instruct-v1:0": {Prefixes: []string{"us"}},
	"meta.llama3-1-8b-instruct-v1:0":  {Prefixes: []string{"us"}},

	// ========================================
	// Amazon Nova
	// ========================================
	"amazon.nova-premier-v1:0": {Prefixes: []string{"us"}},
	"amazon.nova-pro-v1:0":     {Prefixes: []string{"eu", "us", "apac"}},
	"amazon.nova-lite-v1:0":    {Prefixes: []string{"eu", "us", "apac"}},
	"amazon.nova-micro-v1:0":   {Prefixes: []string{"eu", "us", "apac"}},
	"amazon.nova-2-lite-v1:0":  {Prefixes: []string{"eu", "us", "global"}},

	// ========================================
	// Mistral
	// ========================================
	"mistral.pixtral-large-2502-v1:0": {Prefixes: []string{"eu", "us"}},

	// ========================================
	// DeepSeek
	// ========================================
	"deepseek.r1-v1:0": {Prefixes: []string{"us"}},

	// ========================================
	// Cohere
	// ========================================
	"cohere.embed-v4:0": {Prefixes: []string{"eu", "us", "global"}},

	// ========================================
	// TwelveLabs
	// ========================================
	"twelvelabs.pegasus-1-2-v1:0":       {Prefixes: []string{"eu", "us", "global"}},
	"twelvelabs.marengo-embed-2-7-v1:0": {Prefixes: []string{"us"}},
	"twelvelabs.marengo-embed-3-0-v1:0": {Prefixes: []string{"us"}},

	// ========================================
	// Writer
	// ========================================
	"writer.palmyra-x4-v1:0": {Prefixes: []string{"us"}},
	"writer.palmyra-x5-v1:0": {Prefixes: []string{"us"}},
}

// regionPrefixes maps AWS region prefixes to inference profile prefixes.
var regionPrefixes = map[string]string{
	"eu-": "eu",
	"us-": "us",
	"ap-": "apac",
}

// defaultPrefix is used when region doesn't match known patterns.
const defaultPrefix = "global"

// validPrefixes lists all valid inference profile prefixes.
var validPrefixes = []string{"eu.", "us.", "apac.", "global."}

// computeRegionPrefix determines the inference profile prefix for an AWS region.
// Examples: "us-east-1" -> "us", "eu-central-1" -> "eu", "ap-northeast-1" -> "apac"
func computeRegionPrefix(region string) string {
	for regionPrefix, profilePrefix := range regionPrefixes {
		if len(region) >= len(regionPrefix) && region[:len(regionPrefix)] == regionPrefix {
			return profilePrefix
		}
	}
	return defaultPrefix
}

// hasRegionPrefix checks if a model ID already has a region prefix.
func hasRegionPrefix(model string) bool {
	for _, prefix := range validPrefixes {
		if len(model) > len(prefix) && model[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// containsPrefix checks if a prefix is in the list.
func containsPrefix(prefixes []string, prefix string) bool {
	for _, p := range prefixes {
		if p == prefix {
			return true
		}
	}
	return false
}
