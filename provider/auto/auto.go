package auto

import (
	"context"

	"github.com/codewandler/llm"
	providerregistry "github.com/codewandler/llm/internal/providerregistry"
)

const defaultName = "auto"

// New creates a Service configured from explicit provider requests and/or
// auto-detected providers. This is the preferred zero-config entry point.
func New(ctx context.Context, opts ...Option) (*llm.Service, error) {
	cfg := &config{
		name:           defaultName,
		autoDetect:     true,
		builtinAliases: true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	reg := providerregistry.New()
	serviceOpts := []llm.ServiceOption{
		llm.WithRetryPolicy(llm.DefaultRetryPolicy()),
		func(sc *llm.ServiceConfig) {
			sc.Registry = reg
			sc.HTTPClient = cfg.httpClient
			sc.LLMOptions = append(sc.LLMOptions, cfg.llmOpts...)
			if cfg.autoDetect {
				sc.AutoDetect = true
			}
			for providerType, disabled := range cfg.disabledTypes {
				if disabled {
					llm.WithoutProviderType(providerType)(sc)
				}
			}
		},
	}

	for _, storeEntry := range cfg.claudeStores {
		entries, err := providerregistry.RegisterClaudeAccounts(ctx, storeEntry.store)
		if err == nil {
			for _, entry := range entries {
				serviceOpts = append(serviceOpts, llm.WithDetectedProvider(entry))
			}
		}
	}

	for _, req := range cfg.detectedProviders {
		serviceOpts = append(serviceOpts, llm.WithDetectedProvider(req))
	}
	for _, p := range cfg.providers {
		serviceOpts = append(serviceOpts, llm.WithRegisteredProvider(p))
	}

	if cfg.builtinAliases {
		for alias, selector := range builtinIntentAliases() {
			serviceOpts = append(serviceOpts, llm.WithIntentAlias(alias, selector))
		}
		for _, req := range cfg.detectedProviders {
			for alias, selector := range buildBuiltinAliasTargets(req.Name, req.Type) {
				serviceOpts = append(serviceOpts, llm.WithIntentAlias(alias, selector))
			}
		}
	}
	for alias, targets := range cfg.globalAliases {
		if len(targets) == 0 {
			continue
		}
		serviceOpts = append(serviceOpts, llm.WithIntentAlias(alias, llm.IntentSelector{Model: targets[0]}))
	}

	return llm.New(serviceOpts...)
}
