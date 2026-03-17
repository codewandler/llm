package aggregate

import "github.com/codewandler/llm"

// ProviderInstanceConfig configures a single provider instance.
type ProviderInstanceConfig struct {
	Name         string            // Unique instance name
	Type         string            // Provider type key (passed to factory)
	Options      []llm.Option      // Options passed to factory
	ModelAliases map[string]string // Local aliases: "sonnet" -> "claude-sonnet-4-5"
}

// AliasTarget points to a specific model on a provider instance.
type AliasTarget struct {
	Provider string // Provider instance name
	Model    string // Model ID or local alias for that provider
}

// Config is the complete aggregate configuration.
type Config struct {
	Name      string                   // Aggregate provider name (defaults to "aggregate")
	Providers []ProviderInstanceConfig // Named provider instances
	Aliases   map[string][]AliasTarget // Global alias -> ordered targets
}
