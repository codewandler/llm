package llm

// ToolDefinition describes a tool that the model can invoke.
// This is used when sending tools to a provider's API.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}
