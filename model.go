package llm

// Model represents an LLM model.
type Model struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Provider string   `json:"provider"`
	Aliases  []string `json:"aliases,omitempty"`
}

// Resolver resolves a model alias or ToolCallID to its full Model representation.
type Resolver interface {
	// Resolve returns the Model for the given model ToolCallID or alias.
	// Returns an error if the model is not recognized.
	Resolve(modelID string) (Model, error)
}
