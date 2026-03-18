package auto

import (
	"os"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/openrouter"
)

// detectProviders returns provider entries for all auto-detectable providers.
// Detection order determines failover priority.
func detectProviders() []providerEntry {
	var providers []providerEntry

	// 1. Claude local (highest priority for Claude models)
	if claude.LocalTokenProviderAvailable() {
		providers = append(providers, providerEntry{
			name:         "local",
			providerType: ProviderClaude,
			factory: func(opts ...llm.Option) llm.Provider {
				return claude.New(claude.WithLocalTokenProvider())
			},
			modelAliases: claudeModelAliases,
			hasAliases:   true,
		})
	}

	// 2. Bedrock (always included - fails at runtime if no AWS creds)
	providers = append(providers, providerEntry{
		name:         ProviderBedrock,
		providerType: ProviderBedrock,
		factory: func(opts ...llm.Option) llm.Provider {
			return bedrock.New()
		},
		modelAliases: nil,
		hasAliases:   true,
	})

	// 3. Direct Anthropic API
	if os.Getenv(EnvAnthropicKey) != "" {
		providers = append(providers, providerEntry{
			name:         ProviderAnthropic,
			providerType: ProviderAnthropic,
			factory: func(opts ...llm.Option) llm.Provider {
				return anthropic.New(llm.APIKeyFromEnv(EnvAnthropicKey))
			},
			modelAliases: anthropicModelAliases,
			hasAliases:   true,
		})
	}

	// 4. OpenAI
	if os.Getenv(EnvOpenAIKey) != "" || os.Getenv(EnvOpenAIKeyAlt) != "" {
		providers = append(providers, providerEntry{
			name:         ProviderOpenAI,
			providerType: ProviderOpenAI,
			factory: func(opts ...llm.Option) llm.Provider {
				return openai.New(opts...)
			},
			modelAliases: nil,
			hasAliases:   false,
		})
	}

	// 5. OpenRouter
	if os.Getenv(EnvOpenRouterKey) != "" {
		providers = append(providers, providerEntry{
			name:         ProviderOpenRouter,
			providerType: ProviderOpenRouter,
			factory: func(opts ...llm.Option) llm.Provider {
				return openrouter.New(llm.APIKeyFromEnv(EnvOpenRouterKey))
			},
			modelAliases: nil,
			hasAliases:   false,
		})
	}

	return providers
}
