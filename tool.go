package llm

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
)

// ToolDefinition describes a tool that the model can invoke.
// This is used when sending tools to a provider's API.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolDefinitionFor creates a ToolDefinition from a Go struct type using reflection.
// The struct's fields are converted to a JSON Schema that describes the tool's parameters.
//
// Field tags:
//   - `json:"fieldName"` - Sets the parameter name (required)
//   - `jsonschema:"description=..."` - Describes the parameter
//   - `jsonschema:"required"` - Marks the parameter as required
//   - `jsonschema:"enum=val1,enum=val2"` - Restricts to specific values
//
// Example:
//
//	type GetWeatherParams struct {
//	    Location string `json:"location" jsonschema:"description=City name,required"`
//	    Unit     string `json:"unit" jsonschema:"description=Temperature unit,enum=celsius,enum=fahrenheit"`
//	}
//
//	tool := ToolDefinitionFor[GetWeatherParams]("get_weather", "Get current weather")
func ToolDefinitionFor[T any](name, description string) ToolDefinition {
	r := jsonschema.Reflector{
		DoNotReference:             true, // Inline all types, no $defs
		Anonymous:                  true, // No $id field
		AllowAdditionalProperties:  true, // Remove additionalProperties: false
		RequiredFromJSONSchemaTags: true, // Use jsonschema:"required" instead of all fields
	}
	schema := r.Reflect(new(T))
	schema.Version = "" // Strip $schema URI

	// Marshal and unmarshal to get clean map[string]any
	raw, _ := json.Marshal(schema)
	var params map[string]any
	_ = json.Unmarshal(raw, &params)

	// Clean up any remaining metadata fields
	delete(params, "$schema")
	delete(params, "$id")

	return ToolDefinition{
		Name:        name,
		Description: description,
		Parameters:  params,
	}
}
