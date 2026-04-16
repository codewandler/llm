package auto

import (
	"net/http"
	"os"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/dockermr"
	"github.com/codewandler/llm/provider/minimax"
	"github.com/codewandler/llm/provider/ollama"
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
			modelAliases:      modelAliasesForProvider(ProviderClaude),
			hasBuiltinAliases: true,
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
			modelAliases:      modelAliasesForProvider(ProviderAnthropic),
			hasBuiltinAliases: true,
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
			modelAliases:      modelAliasesForProvider(ProviderBedrock),
			hasBuiltinAliases: true,
		})
	}

	// 4. OpenAI via environment variable.
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
			modelAliases:      modelAliasesForProvider(ProviderOpenAI),
			hasBuiltinAliases: true,
		})
	}

	// 5. OpenRouter
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
			modelAliases:      modelAliasesForProvider(ProviderOpenRouter),
			hasBuiltinAliases: false,
		})
	}

	// 6. MiniMax
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
			modelAliases:      modelAliasesForProvider(ProviderMiniMax),
			hasBuiltinAliases: true,
		})
	}

	// 7. Ollama — detected when OLLAMA_HOST is set or localhost:11434 is reachable.
	if !disabled[ProviderOllama] && ollama.Available() {
		baseURL := ollama.BaseURL()
		providers = append(providers, providerEntry{
			name:         ProviderOllama,
			providerType: ProviderOllama,
			factory: func(opts ...llm.Option) llm.Provider {
				opts = append(opts, llm.WithBaseURL(baseURL))
				if httpClient != nil {
					opts = append(opts, llm.WithHTTPClient(httpClient))
				}
				return ollama.New(opts...)
			},
			modelAliases:      nil,
			hasBuiltinAliases: false,
		})
	}

	// 8. ChatGPT/Codex — detected when ~/.codex/auth.json is present and readable.
	// auth is loaded eagerly so the factory closure captures a valid *CodexAuth,
	// mirroring the WithCodexLocal() pattern.
	if !disabled[ProviderChatGPT] && openai.CodexLocalAvailable() {
		if auth, err := openai.LoadCodexAuth(); err == nil {
			providers = append(providers, providerEntry{
				name:         ProviderChatGPT,
				providerType: ProviderChatGPT,
				factory: func(opts ...llm.Option) llm.Provider {
					// opts (llmOpts) are intentionally not forwarded: CodexAuth
					// manages its own OAuth transport and does not accept llm.Option.
					var base http.RoundTripper
					if httpClient != nil {
						base = httpClient.Transport
					}
					return auth.NewProvider(base)
				},
				modelAliases:      modelAliasesForProvider(ProviderChatGPT),
				hasBuiltinAliases: true,
			})
		}
	}

	// 9. Docker Model Runner — detected when localhost:12434 responds to a
	// model listing request. This is the last provider in the detection order
	// because local inference is a fallback for offline/cost-sensitive use;
	// cloud providers (Claude, Bedrock, OpenAI, etc.) are preferred when available.
	if !disabled[ProviderDockerMR] {
		// Resolve the shared transport so the probe respects any proxy or TLS
		// settings without inheriting the full http.Client timeout.
		var sharedTransport http.RoundTripper
		if httpClient != nil {
			sharedTransport = httpClient.Transport
		}
		if dockermr.Available(sharedTransport) {
			providers = append(providers, providerEntry{
				name:         ProviderDockerMR,
				providerType: ProviderDockerMR,
				factory: func(opts ...llm.Option) llm.Provider {
					if httpClient != nil {
						opts = append(opts, llm.WithHTTPClient(httpClient))
					}
					return dockermr.New(opts...)
				},
				modelAliases:      nil,
				hasBuiltinAliases: false,
			})
		}
	}

	return providers
}
