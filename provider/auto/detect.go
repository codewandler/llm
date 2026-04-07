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
// Provider types listed in disabled are skipped.
func detectProviders(httpClient *http.Client, llmOpts []llm.Option, disabled map[string]bool) []providerEntry {
	var providers []providerEntry

	// 1. Claude local (highest priority for Claude models)
	if !disabled[ProviderClaude] && claude.LocalTokenProviderAvailable() {
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
	if !disabled[ProviderAnthropic] && os.Getenv(EnvAnthropicKey) != "" {
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
	if !disabled[ProviderBedrock] && (os.Getenv("AWS_ACCESS_KEY_ID") != "" || os.Getenv("AWS_PROFILE") != "" || os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI") != "") {
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

	// 4. OpenAI — Codex local credentials (~/.codex/auth.json).
	// Checked before the env-var path because it carries a refresh token and
	// therefore degrades more gracefully than a static API key.
	if !disabled[ProviderOpenAI] && openai.CodexLocalAvailable() {
		codexAuth, _ := openai.LoadCodexAuth() // safe: CodexLocalAvailable already verified
		providers = append(providers, providerEntry{
			name:         "codex-local",
			providerType: ProviderOpenAI,
			factory: func(opts ...llm.Option) llm.Provider {
				// Route through the Codex backend transport (chatgpt.com/backend-api).
				// Preserve any custom httpClient transport for proxy/timeout settings.
				var base http.RoundTripper
				if httpClient != nil {
					base = httpClient.Transport
				}
				return codexAuth.NewProvider(base)
			},
			modelAliases: openai.ModelAliases,
			hasAliases:   true,
		})
	}

	// 5. OpenAI via environment variable.
	if !disabled[ProviderOpenAI] && (os.Getenv(EnvOpenAIKey) != "" || os.Getenv(EnvOpenAIKeyAlt) != "") {
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

	// 6. OpenRouter
	if !disabled[ProviderOpenRouter] && os.Getenv(EnvOpenRouterKey) != "" {
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

	// 7. MiniMax
	if !disabled[ProviderMiniMax] && os.Getenv(EnvMiniMaxKey) != "" {
		providers = append(providers, providerEntry{
			name:         ProviderMiniMax,
			providerType: ProviderMiniMax,
			factory: func(opts ...llm.Option) llm.Provider {
				minimaxOpts := []minimax.Option{minimax.WithLLMOpts(opts...)}
				if httpClient != nil {
					minimaxOpts = append(minimaxOpts, minimax.WithLLMOpts(llm.WithHTTPClient(httpClient)))
				}
				if len(llmOpts) > 0 {
					minimaxOpts = append(minimaxOpts, minimax.WithLLMOpts(llmOpts...))
				}
				return minimax.New(minimaxOpts...)
			},
			modelAliases: minimax.ModelAliases,
			hasAliases:   true,
		})
	}

	return providers
}
