package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/codewandler/llm/usage"
)

const (
	ModelDefault  = "default"
	ModelFast     = "fast"
	ModelPowerful = "powerful"
)

type (
	// ModelFetcher is an optional interface providers can implement to list
	// models dynamically from their API instead of returning a static list.
	ModelFetcher interface {
		FetchModels(ctx context.Context) ([]Model, error)
	}

	ModelsProvider interface {
		Models() Models
	}
)

// Model represents an LLM model.
type Model struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Provider string         `json:"provider"`
	Aliases  []string       `json:"aliases,omitempty"`
	Pricing  *usage.Pricing `json:"pricing,omitempty"` // nil unless fetched dynamically
}

type Models []Model

func (m Models) Resolve(modelID string) (Model, error) {
	if modelID == "" {
		return Model{}, fmt.Errorf("model ID is required")
	}

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

	return Model{}, fmt.Errorf("model %q not found", modelID)
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
