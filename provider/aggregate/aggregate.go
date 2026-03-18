package aggregate

import (
	"context"
	"fmt"

	"github.com/codewandler/llm"
)

const defaultName = "aggregate"

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
			fullID := fmt.Sprintf("%s/%s/%s", instName, provType, m.ID)

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
				Provider: fmt.Sprintf("%s/%s", instName, provType),
				Aliases:  m.Aliases,
			}
			modelIndex[fullID] = len(models)
			models = append(models, model)
		}
	}

	// Process local aliases from provider configs
	for instName, aliases := range localAliases {
		provType := providerTypes[instName]
		for alias, modelID := range aliases {
			fullID := fmt.Sprintf("%s/%s/%s", instName, provType, modelID)
			target := resolvedTarget{
				provider:     providers[instName],
				providerName: instName,
				providerType: provType,
				modelID:      modelID,
				fullID:       fullID,
			}

			// Add resolution entries
			prefixedAlias := fmt.Sprintf("%s/%s/%s", instName, provType, alias)
			aliasMap[alias] = append(aliasMap[alias], target)
			aliasMap[prefixedAlias] = append(aliasMap[prefixedAlias], target)

			// Also add short form: provType/alias
			shortAlias := fmt.Sprintf("%s/%s", provType, alias)
			aliasMap[shortAlias] = append(aliasMap[shortAlias], target)

			// Update model's aliases if it exists
			if idx, ok := modelIndex[fullID]; ok {
				models[idx].Aliases = mergeAliases(models[idx].Aliases, []string{alias, prefixedAlias, shortAlias})
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
				prefixedAlias := fmt.Sprintf("%s/%s/%s", rt.providerName, rt.providerType, alias)
				shortAlias := fmt.Sprintf("%s/%s", rt.providerType, alias)
				models[idx].Aliases = mergeAliases(models[idx].Aliases, []string{alias, prefixedAlias, shortAlias})
			}
		}
		if len(resolvedTargets) > 0 {
			aliasMap[alias] = resolvedTargets
			// Also add prefixed forms for single-target aliases
			if len(resolvedTargets) == 1 {
				rt := resolvedTargets[0]
				prefixedAlias := fmt.Sprintf("%s/%s/%s", rt.providerName, rt.providerType, alias)
				aliasMap[prefixedAlias] = resolvedTargets
			}
		}
	}

	// Add resolution entries for model IDs
	for instName := range providers {
		provType := providerTypes[instName]
		prov := providers[instName]
		for _, m := range prov.Models() {
			fullID := fmt.Sprintf("%s/%s/%s", instName, provType, m.ID)
			target := resolvedTarget{
				provider:     prov,
				providerName: instName,
				providerType: provType,
				modelID:      m.ID,
				fullID:       fullID,
			}

			// Full ID
			aliasMap[fullID] = append(aliasMap[fullID], target)

			// Short model ID (may be ambiguous)
			aliasMap[m.ID] = append(aliasMap[m.ID], target)

			// Provider-prefixed model ID
			prefixedID := fmt.Sprintf("%s/%s", provType, m.ID)
			aliasMap[prefixedID] = append(aliasMap[prefixedID], target)
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

	fullID := fmt.Sprintf("%s/%s/%s", t.Provider, provType, modelID)

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
func (p *Provider) CreateStream(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	targets, ok := p.aliasMap[opts.Model]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownModel, opts.Model)
	}

	var lastErr error
	for _, target := range targets {
		streamOpts := opts
		streamOpts.Model = target.modelID

		stream, err := target.provider.CreateStream(ctx, streamOpts)
		if err != nil {
			if isRetriableError(err) {
				lastErr = err
				continue
			}
			return nil, fmt.Errorf("%s: %w", target.providerName, err)
		}

		// Wrap stream to transform StreamEventStart with aggregate context
		return p.wrapStream(stream, opts.Model, target), nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all targets failed: %w", lastErr)
	}
	return nil, ErrNoProviders
}

// wrapStream transforms the underlying provider's stream to update StreamEventStart
// with aggregate-level model resolution info.
func (p *Provider) wrapStream(upstream <-chan llm.StreamEvent, requestedModel string, target resolvedTarget) <-chan llm.StreamEvent {
	out := make(chan llm.StreamEvent, 64)

	go func() {
		defer close(out)

		for evt := range upstream {
			// Transform StreamEventStart to include aggregate context
			if evt.Type == llm.StreamEventStart && evt.Start != nil {
				evt.Start = &llm.StreamStart{
					RequestedModel:   requestedModel,
					ResolvedModel:    target.fullID,
					ProviderModel:    evt.Start.ProviderModel,
					RequestID:        evt.Start.RequestID,
					TimeToFirstToken: evt.Start.TimeToFirstToken,
				}
			}
			out <- evt
		}
	}()

	return out
}
