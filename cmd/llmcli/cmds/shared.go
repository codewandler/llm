package cmds

import (
	"context"
	"os"
	"sort"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/cmd/llmcli/store"
	"github.com/codewandler/llm/provider/aggregate"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/openrouter"
)

// createProvider builds the aggregate provider from available credentials.
func createProvider(ctx context.Context) (*aggregate.Provider, error) {
	tokenStore, err := getTokenStore()
	if err != nil {
		return nil, err
	}
	return buildAggregateProvider(ctx, tokenStore)
}

func getTokenStore() (*store.FileTokenStore, error) {
	dir, err := store.DefaultDir()
	if err != nil {
		return nil, err
	}
	return store.NewFileTokenStore(dir)
}

func buildAggregateProvider(ctx context.Context, tokenStore *store.FileTokenStore) (*aggregate.Provider, error) {
	cfg := aggregate.Config{
		Name:      "llmcli",
		Providers: []aggregate.ProviderInstanceConfig{},
		Aliases: map[string][]aggregate.AliasTarget{
			"fast":     {},
			"default":  {},
			"powerful": {},
		},
	}
	factories := make(map[string]aggregate.Factory)

	// Claude model aliases (shared across Claude OAuth and Claude local)
	claudeModelAliases := map[string]string{
		"sonnet": "claude-sonnet-4-6",
		"opus":   "claude-opus-4-6",
		"haiku":  "claude-haiku-4-5-20251001",
	}

	// 1. Add Claude OAuth accounts from FileTokenStore (with aliases)
	keys, _ := tokenStore.List(ctx)
	sort.Strings(keys)
	for _, key := range keys {
		factoryKey := "claude-" + key

		cfg.Providers = append(cfg.Providers, aggregate.ProviderInstanceConfig{
			Name:         key,
			Type:         factoryKey,
			ModelAliases: claudeModelAliases,
		})

		cfg.Aliases["fast"] = append(cfg.Aliases["fast"],
			aggregate.AliasTarget{Provider: key, Model: "haiku"})
		cfg.Aliases["default"] = append(cfg.Aliases["default"],
			aggregate.AliasTarget{Provider: key, Model: "sonnet"})
		cfg.Aliases["powerful"] = append(cfg.Aliases["powerful"],
			aggregate.AliasTarget{Provider: key, Model: "opus"})

		factories[factoryKey] = claudeFactory(key, tokenStore)
	}

	// 2. Add Claude local if available (with aliases)
	if claude.LocalTokenProviderAvailable() {
		cfg.Providers = append(cfg.Providers, aggregate.ProviderInstanceConfig{
			Name:         "local",
			Type:         "claude-local",
			ModelAliases: claudeModelAliases,
		})
		factories["claude-local"] = func(opts ...llm.Option) llm.Provider {
			return claude.New(claude.WithLocalTokenProvider())
		}

		cfg.Aliases["fast"] = append(cfg.Aliases["fast"],
			aggregate.AliasTarget{Provider: "local", Model: "haiku"})
		cfg.Aliases["default"] = append(cfg.Aliases["default"],
			aggregate.AliasTarget{Provider: "local", Model: "sonnet"})
		cfg.Aliases["powerful"] = append(cfg.Aliases["powerful"],
			aggregate.AliasTarget{Provider: "local", Model: "opus"})
	}

	// 3. Add Bedrock (always, with aliases - will fail at CreateStream if no AWS creds)
	cfg.Providers = append(cfg.Providers, aggregate.ProviderInstanceConfig{
		Name: "bedrock",
		Type: "bedrock",
	})
	factories["bedrock"] = func(opts ...llm.Option) llm.Provider {
		return bedrock.New()
	}
	cfg.Aliases["fast"] = append(cfg.Aliases["fast"],
		aggregate.AliasTarget{Provider: "bedrock", Model: "anthropic.claude-3-5-haiku-20241022-v1:0"})
	cfg.Aliases["default"] = append(cfg.Aliases["default"],
		aggregate.AliasTarget{Provider: "bedrock", Model: "anthropic.claude-sonnet-4-20250514-v1:0"})
	cfg.Aliases["powerful"] = append(cfg.Aliases["powerful"],
		aggregate.AliasTarget{Provider: "bedrock", Model: "anthropic.claude-opus-4-20250514-v1:0"})

	// 4. Add OpenAI if API key is set (NO aliases)
	if os.Getenv("OPENAI_API_KEY") != "" || os.Getenv("OPENAI_KEY") != "" {
		cfg.Providers = append(cfg.Providers, aggregate.ProviderInstanceConfig{
			Name: "openai",
			Type: "openai",
		})
		factories["openai"] = func(opts ...llm.Option) llm.Provider {
			return openai.New(opts...)
		}
	}

	// 5. Add OpenRouter if API key is set (NO aliases)
	if os.Getenv("OPENROUTER_API_KEY") != "" {
		cfg.Providers = append(cfg.Providers, aggregate.ProviderInstanceConfig{
			Name: "openrouter",
			Type: "openrouter",
		})
		factories["openrouter"] = func(opts ...llm.Option) llm.Provider {
			return openrouter.New(llm.APIKeyFromEnv("OPENROUTER_API_KEY"))
		}
	}

	return aggregate.New(cfg, factories)
}

func claudeFactory(key string, tokenStore claude.TokenStore) aggregate.Factory {
	return func(opts ...llm.Option) llm.Provider {
		return claude.New(
			claude.WithManagedTokenProvider(key, tokenStore, nil),
		)
	}
}
