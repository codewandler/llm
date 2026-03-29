package llm

import (
	"context"
	"fmt"
	"strings"
)

const (
	ModelDefault  = "default"
	ModelFast     = "fast"
	ModelPowerful = "powerful"
)

type (
	// ModelResolver resolves a model alias or ToolCallID to its full Model representation.
	ModelResolver interface {
		// Resolve returns the Model for the given model ToolCallID or alias.
		// Returns an error if the model is not recognized.
		Resolve(modelID string) (Model, error)
	}

	// ModelFetcher is an optional interface providers can implement to list
	// models dynamically from their API instead of returning a static list.
	ModelFetcher interface {
		FetchModels(ctx context.Context) ([]Model, error)
	}

	ModelsProvider interface {
		Models() Models
	}
)

type ModelResolveFunc func(modelID string) (Model, error)

func (f ModelResolveFunc) Resolve(modelID string) (Model, error) { return f(modelID) }

// Model represents an LLM model.
type Model struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Provider string   `json:"provider"`
	Aliases  []string `json:"aliases,omitempty"`
}

type Models []Model

func (m Models) Resolve(modelID string) (Model, error) {
	// match exact
	for _, model := range m {
		if model.ID == modelID || strings.Contains(strings.ToLower(model.Name), strings.ToLower(modelID)) {
			return model, nil
		}
	}

	// alias
	for _, model := range m {
		for _, alias := range model.Aliases {
			if alias == modelID {
				return model, nil
			}
		}
	}

	return Model{}, fmt.Errorf("model %q not found", m)
}

func (m Models) Models() Models { return m }

func (m Models) ByID(id string) (Model, bool) {
	for _, m := range m {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

func (m Models) ByAlias(alias string) (Model, bool) {
	for _, m := range m {
		for _, a := range m.Aliases {
			if a == alias {
				return m, true
			}
		}
	}
	return Model{}, false
}

type AliasResolvingProvider struct {
	ModelResolver
	Provider
}

func ProviderWithAliasResolver(p Provider) *AliasResolvingProvider {
	return &AliasResolvingProvider{
		Provider: p,
		ModelResolver: ModelResolveFunc(func(modelID string) (Model, error) {
			found, ok := p.Models().ByAlias(modelID)
			if !ok {
				return Model{}, NewErrUnknownModel(p.Name(), modelID)
			}
			return found, nil
		}),
	}
}
