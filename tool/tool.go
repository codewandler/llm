package tool

import (
	"encoding/json"
	"fmt"

	jsv "github.com/santhosh-tekuri/jsonschema/v6"
)

type (
	Args = map[string]any

	Call interface {
		ToolName() string

		ToolCallID() string
		ToolArgs() Args

		MarshalJSON() ([]byte, error)
		UnmarshalJSON([]byte) error

		Validate() error
	}

	Result interface {
		ToolCallID() string

		ToolOutput() any
		IsError() bool

		MarshalJSON() ([]byte, error)
		UnmarshalJSON([]byte) error
	}

	Exchange interface {
		Call
		Result
	}
)

// toolRegistration is the internal interface that allows heterogeneous Spec[T]
// types to be stored in a Set.
type toolRegistration interface {
	Definition() Definition
	parse(raw Call) (ParsedToolCall, error)
}

// Ensure Spec implements toolRegistration
var _ toolRegistration = (*Spec[struct{}])(nil)

// Spec is a type-safe tool specification that pairs a tool name/description
// with a Go struct that defines the parameter schema.
// It includes a compiled JSON Schema for runtime validation.
type Spec[T any] struct {
	name        string
	description string
	definition  Definition
	schema      *jsv.Schema // compiled schema for validation
}

// NewSpec creates a typed tool specification from a parameter struct.
// The struct's fields define the JSON Schema for the tool's parameters.
// Field tags are the same as DefinitionFor: json, jsonschema.
//
// Example:
//
//	type GetWeatherParams struct {
//	    Location string `json:"location" jsonschema:"description=City name,required"`
//	}
//	spec := NewSpec[GetWeatherParams]("get_weather", "Get current weather")
func NewSpec[T any](name, description string) *Spec[T] {
	def := DefinitionFor[T](name, description)

	// Compile schema for validation
	c := jsv.NewCompiler()
	var schema *jsv.Schema
	if err := c.AddResource("schema.json", def.Parameters); err == nil {
		schema, _ = c.Compile("schema.json")
	}
	// If compilation fails (shouldn't happen for normal structs), schema is nil
	// and validation will be skipped during Parse

	return &Spec[T]{
		name:        name,
		description: description,
		definition:  def,
		schema:      schema,
	}
}

// Definition returns the Definition for sending to providers.
func (s *Spec[T]) Definition() Definition { return s.definition }

// parse validates and parses a raw Call into a TypedToolCall[T].
// This is called by Set.Parse().
func (s *Spec[T]) parse(raw Call) (ParsedToolCall, error) {
	// Validate arguments against schema if available
	if s.schema != nil {
		if err := s.schema.Validate(raw.ToolArgs()); err != nil {
			return nil, fmt.Errorf("validate %s arguments: %w", s.name, err)
		}
	}

	// Marshal arguments to JSON, then unmarshal into T
	data, err := json.Marshal(raw.ToolArgs())
	if err != nil {
		return nil, fmt.Errorf("marshal %s arguments: %w", s.name, err)
	}

	var params T
	if err := json.Unmarshal(data, &params); err != nil {
		return nil, fmt.Errorf("parse %s arguments: %w", s.name, err)
	}

	return &TypedToolCall[T]{
		ID:     raw.ToolCallID(),
		Name:   raw.ToolName(),
		Params: params,
	}, nil
}
