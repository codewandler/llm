package cmds

import (
	"context"
	"fmt"
	"sort"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/cmd/llmcli/store"
	"github.com/codewandler/llm/provider/aggregate"
	"github.com/codewandler/llm/provider/anthropic/claude"
)

// createProvider builds the aggregate provider from stored credentials.
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
	keys, err := tokenStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no credentials found; run 'llmcli auth login claude' first")
	}

	sort.Strings(keys)

	cfg := aggregate.Config{
		Name:      "llmcli",
		Providers: make([]aggregate.ProviderInstanceConfig, 0, len(keys)),
		Aliases: map[string][]aggregate.AliasTarget{
			"fast":     make([]aggregate.AliasTarget, 0, len(keys)),
			"default":  make([]aggregate.AliasTarget, 0, len(keys)),
			"powerful": make([]aggregate.AliasTarget, 0, len(keys)),
		},
	}

	factories := make(map[string]aggregate.Factory)

	for _, key := range keys {
		factoryKey := "claude-" + key

		cfg.Providers = append(cfg.Providers, aggregate.ProviderInstanceConfig{
			Name: key,
			Type: factoryKey,
			ModelAliases: map[string]string{
				"sonnet": "claude-sonnet-4-6",
				"opus":   "claude-opus-4-6",
				"haiku":  "claude-haiku-4-5-20251001",
			},
		})

		cfg.Aliases["fast"] = append(cfg.Aliases["fast"],
			aggregate.AliasTarget{Provider: key, Model: "haiku"})
		cfg.Aliases["default"] = append(cfg.Aliases["default"],
			aggregate.AliasTarget{Provider: key, Model: "sonnet"})
		cfg.Aliases["powerful"] = append(cfg.Aliases["powerful"],
			aggregate.AliasTarget{Provider: key, Model: "opus"})

		factories[factoryKey] = claudeFactory(key, tokenStore)
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
