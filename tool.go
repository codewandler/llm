package llm

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/invopop/jsonschema"
	jsv "github.com/santhosh-tekuri/jsonschema/v6"
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

// --- Type-Safe Tool Dispatch ---

// ToolSpec[T] is a type-safe tool specification that pairs a tool name/description
// with a Go struct that defines the parameter schema.
// It includes a compiled JSON Schema for runtime validation.
type ToolSpec[T any] struct {
	name        string
	description string
	definition  ToolDefinition
	schema      *jsv.Schema // compiled schema for validation
}

// NewToolSpec creates a typed tool specification from a parameter struct.
// The struct's fields define the JSON Schema for the tool's parameters.
// Field tags are the same as ToolDefinitionFor: json, jsonschema.
//
// Example:
//
//	type GetWeatherParams struct {
//	    Location string `json:"location" jsonschema:"description=City name,required"`
//	}
//	spec := NewToolSpec[GetWeatherParams]("get_weather", "Get current weather")
func NewToolSpec[T any](name, description string) *ToolSpec[T] {
	def := ToolDefinitionFor[T](name, description)

	// Compile schema for validation
	c := jsv.NewCompiler()
	var schema *jsv.Schema
	if err := c.AddResource("schema.json", def.Parameters); err == nil {
		schema, _ = c.Compile("schema.json")
	}
	// If compilation fails (shouldn't happen for normal structs), schema is nil
	// and validation will be skipped during Parse

	return &ToolSpec[T]{
		name:        name,
		description: description,
		definition:  def,
		schema:      schema,
	}
}

// Definition returns the ToolDefinition for sending to providers.
func (s *ToolSpec[T]) Definition() ToolDefinition {
	return s.definition
}

// parse validates and parses a raw ToolCall into a TypedToolCall[T].
// This is called by ToolSet.Parse().
func (s *ToolSpec[T]) parse(raw ToolCall) (ParsedToolCall, error) {
	// Validate arguments against schema if available
	if s.schema != nil {
		if err := s.schema.Validate(raw.Arguments); err != nil {
			return nil, fmt.Errorf("validate %s arguments: %w", s.name, err)
		}
	}

	// Marshal arguments to JSON, then unmarshal into T
	data, err := json.Marshal(raw.Arguments)
	if err != nil {
		return nil, fmt.Errorf("marshal %s arguments: %w", s.name, err)
	}

	var params T
	if err := json.Unmarshal(data, &params); err != nil {
		return nil, fmt.Errorf("parse %s arguments: %w", s.name, err)
	}

	return &TypedToolCall[T]{
		ID:     raw.ID,
		Name:   raw.Name,
		Params: params,
	}, nil
}

// toolRegistration is the internal interface that allows heterogeneous ToolSpec[T]
// types to be stored in a ToolSet.
type toolRegistration interface {
	Definition() ToolDefinition
	parse(raw ToolCall) (ParsedToolCall, error)
}

// Ensure ToolSpec implements toolRegistration
var _ toolRegistration = (*ToolSpec[struct{}])(nil)

// --- ToolSet ---

// ToolSet manages a collection of tool specifications.
// It provides tool definitions for sending to providers and parses raw tool calls
// into strongly-typed results with validation.
type ToolSet struct {
	tools []toolRegistration          // ordered for Definitions()
	index map[string]toolRegistration // keyed by name for Parse()
}

// NewToolSet creates a ToolSet from one or more tool specs.
//
// Example:
//
//	tools := NewToolSet(
//	    NewToolSpec[GetWeatherParams]("get_weather", "Get weather"),
//	    NewToolSpec[SearchParams]("search", "Search the web"),
//	)
func NewToolSet(tools ...toolRegistration) *ToolSet {
	index := make(map[string]toolRegistration, len(tools))
	for _, tool := range tools {
		def := tool.Definition()
		index[def.Name] = tool
	}
	return &ToolSet{
		tools: tools,
		index: index,
	}
}

// Definitions returns all tool definitions for sending to providers.
func (ts *ToolSet) Definitions() []ToolDefinition {
	defs := make([]ToolDefinition, len(ts.tools))
	for i, tool := range ts.tools {
		defs[i] = tool.Definition()
	}
	return defs
}

// Parse converts raw ToolCalls (from stream events) into typed ParsedToolCalls.
// Each tool call's arguments are validated against its JSON Schema before parsing.
//
// Successfully parsed calls are always returned. Errors from unknown tool names
// or validation/parse failures are collected and returned as a joined error.
// The error is non-fatal - you get all successfully parsed calls.
//
// Example:
//
//	calls, err := tools.Parse(rawToolCalls)
//	if err != nil {
//	    log.Printf("parse warnings: %v", err)
//	}
//	for _, call := range calls {
//	    switch c := call.(type) {
//	    case *TypedToolCall[GetWeatherParams]:
//	        fmt.Println(c.Params.Location)
//	    }
//	}
func (ts *ToolSet) Parse(calls []ToolCall) ([]ParsedToolCall, error) {
	var result []ParsedToolCall
	var errs []error

	for _, call := range calls {
		reg, ok := ts.index[call.Name]
		if !ok {
			errs = append(errs, fmt.Errorf("unknown tool: %s", call.Name))
			continue
		}
		parsed, err := reg.parse(call)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		result = append(result, parsed)
	}

	return result, errors.Join(errs...)
}

// --- Parsed Results ---

// ParsedToolCall is the interface for parsed tool call results.
// Use a type switch on the concrete *TypedToolCall[T] to access typed params.
//
// Example:
//
//	switch c := call.(type) {
//	case *TypedToolCall[GetWeatherParams]:
//	    fmt.Println(c.Params.Location)  // strongly typed
//	case *TypedToolCall[SearchParams]:
//	    fmt.Println(c.Params.Query)
//	}
type ParsedToolCall interface {
	ToolName() string
	ToolCallID() string
}

// TypedToolCall[T] holds a parsed tool call with strongly-typed parameters.
type TypedToolCall[T any] struct {
	ID     string // Original tool call ID (for sending results back)
	Name   string // Tool name
	Params T      // Parsed, validated parameters
}

// ToolName returns the tool name.
func (c *TypedToolCall[T]) ToolName() string {
	return c.Name
}

// ToolCallID returns the tool call ID.
func (c *TypedToolCall[T]) ToolCallID() string {
	return c.ID
}
