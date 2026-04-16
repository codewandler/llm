package auto

import (
	"net/http"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/codex"
	"github.com/codewandler/llm/provider/dockermr"
	"github.com/codewandler/llm/provider/ollama"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/openrouter"
	"github.com/codewandler/llm/provider/router"
)

// providerEntry holds configuration for a single provider instance.
type providerEntry struct {
	name              string
	providerType      string
	factory           router.Factory
	modelAliases      map[string]string
	hasBuiltinAliases bool // whether this provider participates in built-in top-level aliases
}

// claudeStoreEntry marks that accounts should be enumerated from a TokenStore.
type claudeStoreEntry struct {
	store claude.TokenStore
}

// config holds the auto provider configuration.
type config struct {
	name           string
	providers      []providerEntry
	claudeStores   []claudeStoreEntry // stores to enumerate accounts from
	autoDetect     bool
	builtinAliases bool                // whether built-in top-level aliases are enabled
	disabledTypes  map[string]bool     // provider types excluded from auto-detection
	globalAliases  map[string][]string // user-defined top-level aliases: alias -> []targets
	httpClient     *http.Client        // optional shared HTTP client for all providers
	llmOpts        []llm.Option        // optional shared llm.Options for all providers (e.g. logger)
}

// Option configures the auto provider.
type Option func(*config)

// WithHTTPClient sets a custom HTTP client used by all providers created by
// this auto provider. Useful for injecting a logging or tracing transport.
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) {
		c.httpClient = client
	}
}

// WithLLMOptions sets shared llm.Option values applied to all providers that
// support them (e.g. llm.WithLogger for Bedrock eventstream logging).
func WithLLMOptions(opts ...llm.Option) Option {
	return func(c *config) {
		c.llmOpts = append(c.llmOpts, opts...)
	}
}

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

// WithoutBuiltinAliases disables the built-in top-level aliases
// fast/default/powerful. Provider-scoped aliases such as openai/mini or
// codex/codex remain available.
func WithoutBuiltinAliases() Option {
	return func(c *config) {
		c.builtinAliases = false
	}
}

// WithoutProvider excludes a provider type from auto-detection.
// Auto-detection remains enabled for all other provider types.
// Use the Provider* constants (e.g. ProviderBedrock, ProviderOpenAI).
//
// Example:
//
//	auto.New(ctx, auto.WithoutProvider(auto.ProviderBedrock))
func WithoutProvider(providerType string) Option {
	return func(c *config) {
		if c.disabledTypes == nil {
			c.disabledTypes = make(map[string]bool)
		}
		c.disabledTypes[providerType] = true
	}
}

// WithoutBedrock is a convenience shorthand for WithoutProvider(ProviderBedrock).
// It prevents AWS Bedrock from being auto-detected even when AWS credentials
// are present in the environment.
func WithoutBedrock() Option {
	return WithoutProvider(ProviderBedrock)
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
		httpClient := c.httpClient
		c.providers = append(c.providers, providerEntry{
			name:         name,
			providerType: ProviderClaude,
			factory: func(opts ...llm.Option) llm.Provider {
				claudeOpts := []claude.Option{claude.WithManagedTokenProvider(name, store, nil)}
				if httpClient != nil {
					claudeOpts = append(claudeOpts, claude.WithLLMOptions(llm.WithHTTPClient(httpClient)))
				}
				return claude.New(claudeOpts...)
			},
			modelAliases:      modelAliasesForProvider(ProviderClaude),
			hasBuiltinAliases: true,
		})
	}
}

// WithClaudeLocal adds the local Claude credentials (~/.claude).
func WithClaudeLocal() Option {
	return func(c *config) {
		httpClient := c.httpClient
		c.providers = append(c.providers, providerEntry{
			name:         ProviderClaude,
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
}

// WithCodexLocal adds the Codex provider using local Codex CLI credentials
// (~/.codex/auth.json). The OAuth access token is refreshed automatically
// when it approaches expiry, so no OPENAI_API_KEY is needed.
//
// The provider is registered under the "codex" prefix (e.g. "codex/gpt-5.4")
// so it stays distinct from the regular OpenAI API provider ("openai/...") when
// both are active.
//
// Requests are routed to https://chatgpt.com/backend-api (not api.openai.com)
// because the ChatGPT Plus OAuth token lacks the api.responses.write scope
// required by the standard developer API.
//
// Returns a no-op option if the credentials file is absent or unreadable.
func WithCodexLocal() Option {
	return func(c *config) {
		auth, err := codex.LoadAuth()
		if err != nil {
			// Credentials not available — silently skip.
			return
		}
		c.providers = append(c.providers, providerEntry{
			name:         ProviderCodex,
			providerType: ProviderCodex,
			factory: func(opts ...llm.Option) llm.Provider {
				if c.httpClient != nil {
					opts = append(opts, llm.WithHTTPClient(c.httpClient))
				}
				return codex.New(auth, opts...)
			},
			modelAliases:      modelAliasesForProvider(ProviderCodex),
			hasBuiltinAliases: true,
		})
	}
}

// WithBedrock adds AWS Bedrock provider.
func WithBedrock() Option {
	return func(c *config) {
		httpClient := c.httpClient
		c.providers = append(c.providers, providerEntry{
			name:         ProviderBedrock,
			providerType: ProviderBedrock,
			factory: func(opts ...llm.Option) llm.Provider {
				var bedrockOpts []bedrock.Option
				if httpClient != nil {
					bedrockOpts = append(bedrockOpts, bedrock.WithLLMOptions(llm.WithHTTPClient(httpClient)))
				}
				return bedrock.New(bedrockOpts...)
			},
			modelAliases:      modelAliasesForProvider(ProviderBedrock),
			hasBuiltinAliases: true,
		})
	}
}

// WithOpenAI adds OpenAI provider.
// Requires OPENAI_API_KEY or OPENAI_KEY environment variable.
func WithOpenAI() Option {
	return func(c *config) {
		httpClient := c.httpClient
		c.providers = append(c.providers, providerEntry{
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
}

// WithOpenRouter adds OpenRouter provider.
// Requires OPENROUTER_API_KEY environment variable.
func WithOpenRouter() Option {
	return func(c *config) {
		httpClient := c.httpClient
		c.providers = append(c.providers, providerEntry{
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
}

// WithAnthropic adds direct Anthropic API provider.
// Requires ANTHROPIC_API_KEY environment variable.
func WithAnthropic() Option {
	return func(c *config) {
		httpClient := c.httpClient
		c.providers = append(c.providers, providerEntry{
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
}

// WithGlobalAlias adds a user-defined top-level alias that resolves to one or more targets.
// Use this to add custom aliases on top of the built-in fast/default/powerful
// set, for example a project-local shortcut that points to one or more
// provider-scoped models.
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

// WithGlobalAliases adds multiple user-defined top-level aliases.
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

// WithOllama adds the Ollama local provider.
// The base URL is read from OLLAMA_HOST if set, otherwise http://localhost:11434.
//
// Auto-detection already covers Ollama running on the default port, so the
// primary use cases for this explicit option are:
//   - Forcing a specific URL without setting OLLAMA_HOST in the environment.
//   - Ensuring Ollama is present even when WithoutAutoDetect() is in effect.
//   - Providing fine-grained control when OLLAMA_HOST is set and you want to
//     avoid the auto-detected instance (use WithoutAutoDetect or WithoutOllama).
//
// If OLLAMA_HOST is set and auto-detection is active, calling WithOllama() will
// register a second Ollama instance alongside the auto-detected one. Pass
// WithoutAutoDetect() or WithoutOllama() to avoid duplication.
func WithOllama() Option {
	return func(c *config) {
		baseURL := ollama.BaseURL()
		httpClient := c.httpClient
		c.providers = append(c.providers, providerEntry{
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
}

// WithoutOllama is a convenience shorthand for WithoutProvider(ProviderOllama).
// It prevents Ollama from being auto-detected even when OLLAMA_HOST is set
// or the local server is reachable on the default port.
func WithoutOllama() Option {
	return WithoutProvider(ProviderOllama)
}

// WithoutCodex is a convenience shorthand for WithoutProvider(ProviderCodex).
// It prevents Codex from being auto-detected even when ~/.codex/auth.json is
// present.
func WithoutCodex() Option {
	return WithoutProvider(ProviderCodex)
}

// WithDockerModelRunner adds the Docker Model Runner provider explicitly,
// bypassing auto-detection. This is useful when DMR is running on a
// non-default address (e.g. inside a Docker container) or when
// WithoutAutoDetect() is in effect.
//
// The variadic opts are forwarded to dockermr.New(), so any llm.Option is
// accepted — for example llm.WithBaseURL(dockermr.ContainerBaseURL) to target
// the Docker Desktop container endpoint.
//
// Example:
//
//	// Default address (localhost:12434):
//	r, _ := auto.New(ctx, auto.WithDockerModelRunner())
//
//	// Inside a Docker Desktop container:
//	r, _ := auto.New(ctx,
//	    auto.WithDockerModelRunner(llm.WithBaseURL(dockermr.ContainerBaseURL)),
//	)
func WithDockerModelRunner(opts ...llm.Option) Option {
	return func(c *config) {
		httpClient := c.httpClient
		c.providers = append(c.providers, providerEntry{
			name:         ProviderDockerMR,
			providerType: ProviderDockerMR,
			factory: func(extraOpts ...llm.Option) llm.Provider {
				// Caller-supplied opts take precedence; shared httpClient is
				// appended last so it does not override an explicit WithHTTPClient
				// passed by the caller.
				allOpts := append(opts, extraOpts...)
				if httpClient != nil {
					allOpts = append(allOpts, llm.WithHTTPClient(httpClient))
				}
				return dockermr.New(allOpts...)
			},
			modelAliases:      nil,
			hasBuiltinAliases: false,
		})
	}
}

// WithoutDockerMR is a convenience shorthand for WithoutProvider(ProviderDockerMR).
// It prevents Docker Model Runner from being auto-detected even when
// localhost:12434 is reachable. Useful in CI environments where the port may
// be open for unrelated reasons.
func WithoutDockerMR() Option {
	return WithoutProvider(ProviderDockerMR)
}
