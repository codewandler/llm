package router

import (
	"context"
	"fmt"

	"github.com/codewandler/llm"
)

const defaultName = "router"

// buildModelPath constructs a model path. When instance name equals provider type
// (singleton provider), uses short form "type/model". Otherwise uses full form
// "instance/type/model" for multi-instance disambiguation.
func buildModelPath(instName, provType, modelID string) string {
	if instName == provType {
		return fmt.Sprintf("%s/%s", provType, modelID)
	}
	return fmt.Sprintf("%s/%s/%s", instName, provType, modelID)
}

// buildProviderPath constructs a provider path. When instance name equals provider
// type (singleton), returns just the type. Otherwise returns "instance/type".
func buildProviderPath(instName, provType string) string {
	if instName == provType {
		return provType
	}
	return fmt.Sprintf("%s/%s", instName, provType)
}

// Factory creates a provider instance from options.
type Factory func(opts ...llm.Option) llm.Provider

// Provider is an aggregate provider that routes requests to configured providers.
type Provider struct {
	name          string
	providers     map[string]llm.Provider      // instance name -> provider
	providerTypes map[string]string            // instance name -> provider type
	localAliases  map[string]map[string]string // instance name -> local alias -> modelID
	aliasMap      map[string][]resolvedTarget  // all aliases/IDs -> ordered targets
	models        []llm.Model                  // cached for Models()
	modelIndex    map[string]int               // fullID -> index in models
}

// New creates an aggregate provider from configuration.
// factories maps provider type keys to constructor functions.
func New(cfg Config, factories map[string]Factory) (*Provider, error) {
	if len(cfg.Providers) == 0 {
		return nil, ErrNoProviders
	}

	name := cfg.Name
	if name == "" {
		name = defaultName
	}

	providers := make(map[string]llm.Provider)
	providerTypes := make(map[string]string)
	localAliases := make(map[string]map[string]string)

	// Create provider instances
	for _, pcfg := range cfg.Providers {
		factory, ok := factories[pcfg.Type]
		if !ok {
			return nil, fmt.Errorf("unknown provider type: %s", pcfg.Type)
		}

		prov := factory(pcfg.Options...)
		providers[pcfg.Name] = prov
		providerTypes[pcfg.Name] = pcfg.Type

		// Store local aliases
		if len(pcfg.ModelAliases) > 0 {
			localAliases[pcfg.Name] = pcfg.ModelAliases
		}
	}

	// Build models and alias map
	models := make([]llm.Model, 0)
	modelIndex := make(map[string]int)
	aliasMap := make(map[string][]resolvedTarget)

	// First, collect all underlying models
	for instName, prov := range providers {
		provType := providerTypes[instName]
		for _, m := range prov.Models() {
			fullID := buildModelPath(instName, provType, m.ID)

			// Check if we already have this fullID
			if idx, exists := modelIndex[fullID]; exists {
				// Merge aliases into existing model
				models[idx].Aliases = mergeAliases(models[idx].Aliases, m.Aliases)
				continue
			}

			// Add new model entry
			model := llm.Model{
				ID:       fullID,
				Name:     m.Name,
				Provider: buildProviderPath(instName, provType),
				Aliases:  m.Aliases,
			}
			modelIndex[fullID] = len(models)
			models = append(models, model)
		}
	}

	// Process local aliases from provider configs.
	// Local aliases are only accessible with provider prefix (e.g., "openai/mini").
	// Global aliases must be explicitly configured via cfg.Aliases.
	for instName, aliases := range localAliases {
		provType := providerTypes[instName]
		for alias, modelID := range aliases {
			fullID := buildModelPath(instName, provType, modelID)
			target := resolvedTarget{
				provider:     providers[instName],
				providerName: instName,
				providerType: provType,
				modelID:      modelID,
				fullID:       fullID,
			}

			// Add prefixed alias entry only (no bare alias in global namespace)
			prefixedAlias := buildModelPath(instName, provType, alias)
			aliasMap[prefixedAlias] = append(aliasMap[prefixedAlias], target)

			// Also add short form for multi-instance: provType/alias (only if different from prefixed)
			shortAlias := fmt.Sprintf("%s/%s", provType, alias)
			if shortAlias != prefixedAlias {
				aliasMap[shortAlias] = append(aliasMap[shortAlias], target)
			}

			// Update model's aliases if it exists
			if idx, ok := modelIndex[fullID]; ok {
				aliasesToAdd := []string{prefixedAlias}
				if shortAlias != prefixedAlias {
					aliasesToAdd = append(aliasesToAdd, shortAlias)
				}
				models[idx].Aliases = mergeAliases(models[idx].Aliases, aliasesToAdd)
			}
		}
	}

	// Process global aliases from config
	for alias, targets := range cfg.Aliases {
		var resolvedTargets []resolvedTarget
		for _, t := range targets {
			rt, err := resolveTargetSimple(t, providers, providerTypes, localAliases)
			if err != nil {
				continue // skip invalid targets
			}
			resolvedTargets = append(resolvedTargets, rt)

			// Update model's aliases
			if idx, ok := modelIndex[rt.fullID]; ok {
				prefixedAlias := buildModelPath(rt.providerName, rt.providerType, alias)
				aliasesToAdd := []string{alias, prefixedAlias}
				// Add short form only for multi-instance
				shortAlias := fmt.Sprintf("%s/%s", rt.providerType, alias)
				if shortAlias != prefixedAlias {
					aliasesToAdd = append(aliasesToAdd, shortAlias)
				}
				models[idx].Aliases = mergeAliases(models[idx].Aliases, aliasesToAdd)
			}
		}
		if len(resolvedTargets) > 0 {
			aliasMap[alias] = resolvedTargets
			// Always register the prefixed form for each individual target so
			// provider-scoped aliases like "bedrock/fast" work even when multiple
			// providers share the same global alias.
			for _, rt := range resolvedTargets {
				prefixedAlias := buildModelPath(rt.providerName, rt.providerType, alias)
				if _, exists := aliasMap[prefixedAlias]; !exists {
					aliasMap[prefixedAlias] = []resolvedTarget{rt}
				}
			}
		}
	}

	// Add resolution entries for model IDs
	for instName := range providers {
		provType := providerTypes[instName]
		prov := providers[instName]
		for _, m := range prov.Models() {
			fullID := buildModelPath(instName, provType, m.ID)
			target := resolvedTarget{
				provider:     prov,
				providerName: instName,
				providerType: provType,
				modelID:      m.ID,
				fullID:       fullID,
			}

			// Full ID (which is already the short form for singletons)
			aliasMap[fullID] = append(aliasMap[fullID], target)

			// Short model ID (may be ambiguous)
			aliasMap[m.ID] = append(aliasMap[m.ID], target)

			// Provider-prefixed model ID (only add if different from fullID)
			prefixedID := fmt.Sprintf("%s/%s", provType, m.ID)
			if prefixedID != fullID {
				aliasMap[prefixedID] = append(aliasMap[prefixedID], target)
			}
		}
	}

	return &Provider{
		name:          name,
		providers:     providers,
		providerTypes: providerTypes,
		localAliases:  localAliases,
		aliasMap:      aliasMap,
		models:        models,
		modelIndex:    modelIndex,
	}, nil
}

// resolveTargetSimple resolves an AliasTarget without requiring a Provider receiver.
func resolveTargetSimple(t AliasTarget, providers map[string]llm.Provider, providerTypes map[string]string, localAliases map[string]map[string]string) (resolvedTarget, error) {
	prov, ok := providers[t.Provider]
	if !ok {
		return resolvedTarget{}, fmt.Errorf("%w: %s", ErrProviderNotFound, t.Provider)
	}

	provType := providerTypes[t.Provider]
	modelID := t.Model

	// Resolve local alias
	if aliases, ok := localAliases[t.Provider]; ok {
		if resolved, ok := aliases[modelID]; ok {
			modelID = resolved
		}
	}

	fullID := buildModelPath(t.Provider, provType, modelID)

	return resolvedTarget{
		provider:     prov,
		providerName: t.Provider,
		providerType: provType,
		modelID:      modelID,
		fullID:       fullID,
	}, nil
}

// mergeAliases combines two alias slices, deduplicating.
func mergeAliases(a, b []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, alias := range a {
		if !seen[alias] {
			seen[alias] = true
			result = append(result, alias)
		}
	}
	for _, alias := range b {
		if !seen[alias] {
			seen[alias] = true
			result = append(result, alias)
		}
	}
	return result
}

// Name returns the aggregate provider name.
func (p *Provider) Name() string {
	return p.name
}

// Models returns all models with their aliases.
func (p *Provider) Models() []llm.Model {
	return p.models
}

// Resolve implements llm.Resolver.
func (p *Provider) Resolve(modelID string) (llm.Model, error) {
	targets, ok := p.aliasMap[modelID]
	if !ok || len(targets) == 0 {
		return llm.Model{}, fmt.Errorf("%w: %s", ErrUnknownModel, modelID)
	}

	// Use first target's fullID to look up the model
	fullID := targets[0].fullID
	idx, ok := p.modelIndex[fullID]
	if !ok {
		return llm.Model{}, fmt.Errorf("%w: %s", ErrUnknownModel, modelID)
	}

	return p.models[idx], nil
}

// CreateStream creates a stream by routing to the appropriate provider.
// It tries each target in order until one succeeds or all fail.
func (p *Provider) CreateStream(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
	if err := opts.Validate(); err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameRouter, err)
	}

	targets, ok := p.aliasMap[opts.Model]
	if !ok {
		return nil, llm.NewErrUnknownModel(llm.ProviderNameRouter, opts.Model)
	}

	var triedErrors []error
	for _, target := range targets {
		streamOpts := opts
		streamOpts.Model = target.modelID

		stream, err := target.provider.CreateStream(ctx, streamOpts)
		if err != nil {
			pe := llm.AsProviderError(target.providerName, err)
			if isRetriableError(pe) {
				triedErrors = append(triedErrors, pe)
				continue
			}
			return nil, pe
		}

		out := llm.NewEventStream()
		out.Routed(llm.Routed{
			Provider:       target.providerName,
			ModelRequested: opts.Model,
			ModelResolved:  target.fullID,
			Errors:         triedErrors,
		})
		go func() {
			defer out.Close()
			for evt := range stream {
				if evt.Type == llm.StreamEventCreated {
					continue
				}
				out.Send(evt)
			}
		}()
		return out.C(), nil
	}

	return nil, llm.NewErrNoProviders(llm.ProviderNameRouter)
}
