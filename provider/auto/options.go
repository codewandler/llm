package auto

import (
	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/aggregate"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/openrouter"
)

// providerEntry holds configuration for a single provider instance.
type providerEntry struct {
	name         string
	providerType string
	factory      aggregate.Factory
	modelAliases map[string]string
	hasAliases   bool // whether to add global aliases (fast/default/powerful)
}

// claudeStoreEntry marks that accounts should be enumerated from a TokenStore.
type claudeStoreEntry struct {
	store claude.TokenStore
}

// config holds the auto provider configuration.
type config struct {
	name          string
	providers     []providerEntry
	claudeStores  []claudeStoreEntry // stores to enumerate accounts from
	autoDetect    bool
	globalAliases map[string][]string // user-defined global aliases: alias -> []targets
}

// Option configures the auto provider.
type Option func(*config)

// WithName sets the aggregate provider name.
func WithName(name string) Option {
	return func(c *config) {
		c.name = name
	}
}

// WithoutAutoDetect disables auto-detection of providers.
// Use this when you want to explicitly configure all providers.
func WithoutAutoDetect() Option {
	return func(c *config) {
		c.autoDetect = false
	}
}

// WithClaude adds all Claude OAuth accounts from a TokenStore.
// Each account key becomes a separate provider instance with name equal to the key.
func WithClaude(store claude.TokenStore) Option {
	return func(c *config) {
		c.claudeStores = append(c.claudeStores, claudeStoreEntry{store: store})
	}
}

// WithClaudeAccount adds a specific Claude OAuth account.
func WithClaudeAccount(name string, store claude.TokenStore) Option {
	return func(c *config) {
		c.providers = append(c.providers, providerEntry{
			name:         name,
			providerType: ProviderClaude,
			factory: func(opts ...llm.Option) llm.Provider {
				return claude.New(claude.WithManagedTokenProvider(name, store, nil))
			},
			modelAliases: claudeModelAliases,
			hasAliases:   true,
		})
	}
}

// WithClaudeLocal adds the local Claude credentials (~/.claude).
func WithClaudeLocal() Option {
	return func(c *config) {
		c.providers = append(c.providers, providerEntry{
			name:         ProviderClaude,
			providerType: ProviderClaude,
			factory: func(opts ...llm.Option) llm.Provider {
				return claude.New(claude.WithLocalTokenProvider())
			},
			modelAliases: claudeModelAliases,
			hasAliases:   true,
		})
	}
}

// WithBedrock adds AWS Bedrock provider.
func WithBedrock() Option {
	return func(c *config) {
		c.providers = append(c.providers, providerEntry{
			name:         ProviderBedrock,
			providerType: ProviderBedrock,
			factory: func(opts ...llm.Option) llm.Provider {
				return bedrock.New()
			},
			modelAliases: nil,
			hasAliases:   true,
		})
	}
}

// WithOpenAI adds OpenAI provider.
// Requires OPENAI_API_KEY or OPENAI_KEY environment variable.
func WithOpenAI() Option {
	return func(c *config) {
		c.providers = append(c.providers, providerEntry{
			name:         ProviderOpenAI,
			providerType: ProviderOpenAI,
			factory: func(opts ...llm.Option) llm.Provider {
				return openai.New(opts...)
			},
			modelAliases: openaiModelAliases,
			hasAliases:   false, // OpenAI doesn't participate in fast/default/powerful aliases
		})
	}
}

// WithOpenRouter adds OpenRouter provider.
// Requires OPENROUTER_API_KEY environment variable.
func WithOpenRouter() Option {
	return func(c *config) {
		c.providers = append(c.providers, providerEntry{
			name:         ProviderOpenRouter,
			providerType: ProviderOpenRouter,
			factory: func(opts ...llm.Option) llm.Provider {
				return openrouter.New(llm.APIKeyFromEnv(EnvOpenRouterKey))
			},
			modelAliases: nil,
			hasAliases:   false,
		})
	}
}

// WithAnthropic adds direct Anthropic API provider.
// Requires ANTHROPIC_API_KEY environment variable.
func WithAnthropic() Option {
	return func(c *config) {
		c.providers = append(c.providers, providerEntry{
			name:         ProviderAnthropic,
			providerType: ProviderAnthropic,
			factory: func(opts ...llm.Option) llm.Provider {
				return anthropic.New(llm.APIKeyFromEnv(EnvAnthropicKey))
			},
			modelAliases: anthropicModelAliases,
			hasAliases:   true,
		})
	}
}

// WithGlobalAlias adds a user-defined global alias that resolves to one or more targets.
// Targets should be provider-prefixed model references (e.g., "openai/o3", "openrouter/openai/o3").
// Multiple targets enable failover - if the first target fails, the next is tried.
//
// Example:
//
//	auto.WithGlobalAlias("o3", "openai/o3", "openrouter/openai/o3")
func WithGlobalAlias(alias string, targets ...string) Option {
	return func(c *config) {
		if c.globalAliases == nil {
			c.globalAliases = make(map[string][]string)
		}
		c.globalAliases[alias] = targets
	}
}

// WithGlobalAliases adds multiple user-defined global aliases.
// Each key is an alias name, and the value is a slice of targets.
//
// Example:
//
//	auto.WithGlobalAliases(map[string][]string{
//	    "o3":    {"openai/o3", "openrouter/openai/o3"},
//	    "codex": {"openai/codex"},
//	})
func WithGlobalAliases(aliases map[string][]string) Option {
	return func(c *config) {
		if c.globalAliases == nil {
			c.globalAliases = make(map[string][]string)
		}
		for alias, targets := range aliases {
			c.globalAliases[alias] = targets
		}
	}
}
