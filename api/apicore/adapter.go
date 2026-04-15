// api/apicore/adapter.go
package apicore

// AdapterConfig holds identity settings shared by all protocol adapters.
type AdapterConfig struct {
	// ProviderName is used in errors, usage records, and ModelResolvedEvent.Resolver.
	// Example: "anthropic", "openai", "openrouter".
	ProviderName string

	// UpstreamProvider is used in StreamStartedEvent.Provider.
	// Falls back to ProviderName when empty.
	// Relevant for routing providers where billing ≠ upstream backend.
	UpstreamProvider string
}

// Provider returns the effective provider name for errors and records.
func (c AdapterConfig) Provider() string {
	if c.ProviderName != "" {
		return c.ProviderName
	}
	return "unknown"
}

// Upstream returns the effective upstream provider for StreamStartedEvent.
func (c AdapterConfig) Upstream() string {
	if c.UpstreamProvider != "" {
		return c.UpstreamProvider
	}
	return c.Provider()
}

// AdapterOption configures AdapterConfig.
type AdapterOption func(*AdapterConfig)

func WithProviderName(name string) AdapterOption {
	return func(c *AdapterConfig) { c.ProviderName = name }
}

func WithUpstreamProvider(name string) AdapterOption {
	return func(c *AdapterConfig) { c.UpstreamProvider = name }
}

// ApplyAdapterOptions builds an AdapterConfig from options.
func ApplyAdapterOptions(opts ...AdapterOption) AdapterConfig {
	cfg := AdapterConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
