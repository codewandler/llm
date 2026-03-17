package aggregate

import (
	"context"
	"errors"
	"fmt"

	"github.com/codewandler/llm"
)

const defaultName = "aggregate"

// Factory creates a provider instance from options.
type Factory func(opts ...llm.Option) llm.Provider

// Provider is an aggregate provider that routes requests to configured providers.
type Provider struct {
	name         string
	providers    map[string]llm.Provider      // instance name -> provider
	aliases      map[string][]AliasTarget     // aggregate alias -> ordered targets
	localAliases map[string]map[string]string // provider name -> local alias -> modelID
	models       []llm.Model                  // cached model list for Models()
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
	localAliases := make(map[string]map[string]string)

	// Create provider instances
	for _, pcfg := range cfg.Providers {
		factory, ok := factories[pcfg.Type]
		if !ok {
			return nil, fmt.Errorf("unknown provider type: %s", pcfg.Type)
		}

		prov := factory(pcfg.Options...)
		providers[pcfg.Name] = prov

		// Store local aliases
		if len(pcfg.ModelAliases) > 0 {
			localAliases[pcfg.Name] = pcfg.ModelAliases
		}
	}

	// Build model list from aliases
	models := make([]llm.Model, 0, len(cfg.Aliases))
	for alias := range cfg.Aliases {
		models = append(models, llm.Model{
			ID:       alias,
			Name:     alias,
			Provider: name,
		})
	}

	return &Provider{
		name:         name,
		providers:    providers,
		aliases:      cfg.Aliases,
		localAliases: localAliases,
		models:       models,
	}, nil
}

// Name returns the aggregate provider name.
func (p *Provider) Name() string {
	return p.name
}

// Models returns all aliased models.
func (p *Provider) Models() []llm.Model {
	return p.models
}

// CreateStream creates a stream by routing to the appropriate provider.
// It tries each target in order until one succeeds or all fail.
func (p *Provider) CreateStream(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	targets, err := p.resolveAllTargets(opts.Model)
	if err != nil {
		return nil, err
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
			return nil, fmt.Errorf("%s: %w", target.name, err)
		}

		return stream, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all targets failed: %w", lastErr)
	}
	return nil, errors.New("no targets configured")
}
