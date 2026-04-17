package auto

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/router"
)

const defaultName = "auto"

// New creates an aggregate provider with auto-detected or explicitly configured providers.
//
// Without options, it auto-detects available providers in priority order:
//   - Claude local (~/.claude credentials)
//   - Anthropic direct API (if ANTHROPIC_API_KEY is set)
//   - AWS Bedrock (if AWS_ACCESS_KEY_ID, AWS_PROFILE, or container credentials are set)
//   - OpenAI (if OPENAI_API_KEY or OPENAI_KEY is set)
//   - OpenRouter (if OPENROUTER_API_KEY is set)
//   - MiniMax (if MINIMAX_API_KEY is set)
//   - Ollama (if OLLAMA_HOST is set)
//   - ChatGPT/Codex (if ~/.codex/auth.json is present)
//
// With explicit options, you can configure specific providers:
//
//	auto.New(ctx,
//	    auto.WithName("myapp"),
//	    auto.WithClaude(tokenStore),  // Claude accounts from store
//	    auto.WithClaudeLocal(),       // Claude local credentials
//	    auto.WithBedrock(),           // AWS Bedrock
//	    auto.WithOllama(),            // Ollama (default or OLLAMA_HOST port)
//	    auto.WithCodexLocal(),        // ChatGPT/Codex via ~/.codex/auth.json
//	)
func New(ctx context.Context, opts ...Option) (*router.Provider, error) {
	cfg := &config{
		name:           defaultName,
		autoDetect:     true,
		builtinAliases: true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Collect all provider entries
	var allProviders []providerEntry

	// Add providers from Claude stores (enumerate accounts)
	for _, storeEntry := range cfg.claudeStores {
		entries := enumerateClaudeAccounts(ctx, storeEntry.store, cfg.httpClient, cfg.llmOpts)
		allProviders = append(allProviders, entries...)
	}

	// Add explicitly configured providers
	allProviders = append(allProviders, cfg.providers...)

	// Auto-detect available providers (unless disabled)
	if cfg.autoDetect {
		detected := detectProviders(cfg.httpClient, cfg.llmOpts, cfg.disabledTypes)
		allProviders = append(allProviders, detected...)
	}

	// If still no providers, return error
	if len(allProviders) == 0 {
		return nil, router.ErrNoProviders
	}

	// Build aggregate config
	aggCfg := router.Config{
		Name:      cfg.name,
		Providers: make([]router.ProviderInstanceConfig, 0, len(allProviders)),
		Aliases:   map[string][]router.AliasTarget{},
	}
	factories := make(map[string]router.Factory)

	// Track used instance names to avoid duplicates
	usedNames := make(map[string]int)

	for _, entry := range allProviders {
		// Deduplicate instance names
		instanceName := entry.name
		if count, exists := usedNames[instanceName]; exists {
			usedNames[instanceName]++
			instanceName = instanceName + "-" + string(rune('0'+count+1))
		} else {
			usedNames[instanceName] = 0
		}

		// Factory key format:
		// - For single-instance providers (name == type): just use the type (e.g., "bedrock")
		// - For multi-instance providers (name != type): use "type-name" (e.g., "claude-work")
		var factoryKey string
		if instanceName == entry.providerType {
			factoryKey = entry.providerType
		} else {
			factoryKey = entry.providerType + "-" + instanceName
		}

		aggCfg.Providers = append(aggCfg.Providers, router.ProviderInstanceConfig{
			Name:         instanceName,
			Type:         factoryKey, // Used for factory lookup and in model paths
			ModelAliases: entry.modelAliases,
			Options:      cfg.llmOpts,
		})

		factories[factoryKey] = entry.factory

		// Add global alias targets for providers that support them
		if cfg.builtinAliases && entry.hasBuiltinAliases {
			targets := buildBuiltinAliasTargets(instanceName, entry.providerType)
			for alias, target := range targets {
				aggCfg.Aliases[alias] = append(aggCfg.Aliases[alias], target)
			}
		}
	}

	// Add user-defined global aliases
	for alias, targets := range cfg.globalAliases {
		for _, target := range targets {
			aliasTarget := parseAliasTarget(target)
			aggCfg.Aliases[alias] = append(aggCfg.Aliases[alias], aliasTarget)
		}
	}

	return router.New(aggCfg, factories)
}

// parseAliasTarget parses a string target like "openai/o3" or "work/claude/sonnet"
// into an AliasTarget. The first component is the provider instance name,
// and the rest is the model reference.
func parseAliasTarget(target string) router.AliasTarget {
	parts := strings.SplitN(target, "/", 2)
	if len(parts) == 1 {
		// Just a model ID, no provider prefix - use as-is
		return router.AliasTarget{Provider: "", Model: parts[0]}
	}
	return router.AliasTarget{Provider: parts[0], Model: parts[1]}
}

// enumerateClaudeAccounts lists all accounts from a TokenStore and creates provider entries.
func enumerateClaudeAccounts(ctx context.Context, store claude.TokenStore, httpClient *http.Client, llmOpts []llm.Option) []providerEntry {
	keys, err := store.List(ctx)
	if err != nil {
		return nil
	}

	sort.Strings(keys)

	var entries []providerEntry
	for _, key := range keys {
		// Capture key in closure
		accountKey := key
		entries = append(entries, providerEntry{
			name:         accountKey,
			providerType: ProviderClaude,
			factory: func(opts ...llm.Option) llm.Provider {
				shared := append([]llm.Option{}, llmOpts...)
				shared = append(shared, opts...)
				if httpClient != nil {
					shared = append(shared, llm.WithHTTPClient(httpClient))
				}
				claudeOpts := []claude.Option{claude.WithManagedTokenProvider(accountKey, store, nil), claude.WithLLMOptions(shared...)}
				return claude.New(claudeOpts...)
			},
			modelAliases:      modelAliasesForProvider(ProviderClaude),
			hasBuiltinAliases: true,
		})
	}

	return entries
}
