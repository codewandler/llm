package auto

import (
	"net/http"
	"os"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/minimax"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/openrouter"
)

// detectProviders returns provider entries for all auto-detectable providers.
// Detection order determines failover priority.
func detectProviders(httpClient *http.Client, llmOpts []llm.Option) []providerEntry {
	var providers []providerEntry

	// 1. Claude local (highest priority for Claude models)
	if claude.LocalTokenProviderAvailable() {
		providers = append(providers, providerEntry{
			name:         "local",
			providerType: ProviderClaude,
			factory: func(opts ...llm.Option) llm.Provider {
				claudeOpts := []claude.Option{claude.WithLocalTokenProvider()}
				if httpClient != nil {
					claudeOpts = append(claudeOpts, claude.WithLLMOptions(llm.WithHTTPClient(httpClient)))
				}
				return claude.New(claudeOpts...)
			},
			modelAliases: anthropic.ModelAliases,
			hasAliases:   true,
		})
	}

	// 2. Direct Anthropic API
	if os.Getenv(EnvAnthropicKey) != "" {
		providers = append(providers, providerEntry{
			name:         ProviderAnthropic,
			providerType: ProviderAnthropic,
			factory: func(opts ...llm.Option) llm.Provider {
				anthropicOpts := []llm.Option{llm.APIKeyFromEnv(EnvAnthropicKey)}
				if httpClient != nil {
					anthropicOpts = append(anthropicOpts, llm.WithHTTPClient(httpClient))
				}
				return anthropic.New(anthropicOpts...)
			},
			modelAliases: anthropic.ModelAliases,
			hasAliases:   true,
		})
	}

	// 3. Bedrock — after all Claude-family providers since it also serves Claude
	// models but with higher latency and different pricing.
	// Only included when AWS credentials are present in the environment.
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" || os.Getenv("AWS_PROFILE") != "" || os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI") != "" {
		providers = append(providers, providerEntry{
			name:         ProviderBedrock,
			providerType: ProviderBedrock,
			factory: func(opts ...llm.Option) llm.Provider {
				bedrockOpts := []bedrock.Option{}
				if httpClient != nil {
					bedrockOpts = append(bedrockOpts, bedrock.WithLLMOptions(llm.WithHTTPClient(httpClient)))
				}
				if len(llmOpts) > 0 {
					bedrockOpts = append(bedrockOpts, bedrock.WithLLMOptions(llmOpts...))
				}
				return bedrock.New(bedrockOpts...)
			},
			modelAliases: bedrock.ModelAliases,
			hasAliases:   true,
		})
	}

	// 4. OpenAI
	if os.Getenv(EnvOpenAIKey) != "" || os.Getenv(EnvOpenAIKeyAlt) != "" {
		providers = append(providers, providerEntry{
			name:         ProviderOpenAI,
			providerType: ProviderOpenAI,
			factory: func(opts ...llm.Option) llm.Provider {
				if httpClient != nil {
					opts = append(opts, llm.WithHTTPClient(httpClient))
				}
				return openai.New(opts...)
			},
			modelAliases: openai.ModelAliases,
			hasAliases:   true,
		})
	}

	// 5. OpenRouter
	if os.Getenv(EnvOpenRouterKey) != "" {
		providers = append(providers, providerEntry{
			name:         ProviderOpenRouter,
			providerType: ProviderOpenRouter,
			factory: func(opts ...llm.Option) llm.Provider {
				routerOpts := []llm.Option{llm.APIKeyFromEnv(EnvOpenRouterKey)}
				if httpClient != nil {
					routerOpts = append(routerOpts, llm.WithHTTPClient(httpClient))
				}
				return openrouter.New(routerOpts...)
			},
			modelAliases: nil,
			hasAliases:   false,
		})
	}

	// 6. MiniMax
	if os.Getenv(EnvMiniMaxKey) != "" {
		providers = append(providers, providerEntry{
			name:         ProviderMiniMax,
			providerType: ProviderMiniMax,
			factory: func(opts ...llm.Option) llm.Provider {
				return minimax.New(minimax.WithLLMOpts(opts...))
			},
			modelAliases: minimax.ModelAliases,
			hasAliases:   true,
		})
	}

	return providers
}
