package llm

import (
	"context"
	"fmt"
	"strings"
)

// Registry holds all registered providers and resolves model references.
type Registry struct {
	models    []Model
	providers map[string]Provider
}

// NewRegistry creates an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider), models: make([]Model, 0)}
}

// Register adds a provider to the registry.
func (r *Registry) Register(p Provider) {
	r.providers[p.Name()] = p
	for _, m := range p.Models() {
		r.models = append(r.models, Model{
			ID:       p.Name() + "/" + m.ID,
			Name:     m.Name,
			Provider: p.Name(),
		})
	}
}

// Provider returns the provider with the given name, or an error.
func (r *Registry) Provider(name string) (Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q: %w", name, ErrNotFound)
	}
	return p, nil
}

// ResolveModel parses a "provider/model" string and returns the provider and model ID.
func (r *Registry) ResolveModel(ref string) (Provider, string, error) {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("invalid model reference %q, expected provider/model: %w", ref, ErrBadRequest)
	}
	p, err := r.Provider(parts[0])
	if err != nil {
		return nil, "", err
	}
	return p, parts[1], nil
}

// AllModels returns all models from all registered providers.
func (r *Registry) AllModels() []Model { return r.models }

// FetchModels returns models for a specific provider. If the provider implements
// ModelFetcher, it fetches models dynamically from the API. Otherwise it falls
// back to the static Models() list.
func (r *Registry) FetchModels(ctx context.Context, name string) ([]Model, error) {
	p, err := r.Provider(name)
	if err != nil {
		return nil, err
	}
	if fetcher, ok := p.(ModelFetcher); ok {
		return fetcher.FetchModels(ctx)
	}
	return p.Models(), nil
}

// CreateStream is a convenience that resolves a model ref and delegates to the provider.
func (r *Registry) CreateStream(ctx context.Context, opts StreamOptions) (<-chan StreamEvent, error) {
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}
	p, modelID, err := r.ResolveModel(opts.Model)
	if err != nil {
		return nil, err
	}
	opts.Model = modelID
	return p.CreateStream(ctx, opts)
}
