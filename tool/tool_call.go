package tool

import (
	"encoding/json"
	"errors"
)

type toolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args Args   `json:"args"`
}

func NewToolCall(id, name string, args Args) Call {
	return &toolCall{
		ID:   id,
		Name: name,
		Args: args,
	}
}

func (tc *toolCall) ToolCallID() string { return tc.ID }
func (tc *toolCall) ToolName() string   { return tc.Name }
func (tc *toolCall) ToolArgs() Args     { return tc.Args }
func (tc *toolCall) Validate() error {
	if tc.ID == "" {
		return errors.New("tool call: id is required")
	}
	if tc.Name == "" {
		return errors.New("tool call: name is required")
	}
	return nil
}

func (tc *toolCall) MarshalJSON() ([]byte, error) {
	return json.Marshal(toolCall{tc.ID, tc.Name, tc.Args})
}

func (tc *toolCall) UnmarshalJSON(data []byte) error {
	var w toolCall
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	tc.ID = w.ID
	tc.Name = w.Name
	tc.Args = w.Args

	return nil
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

// TypedToolCall holds a parsed tool call with strongly-typed parameters.
type TypedToolCall[T any] struct {
	ID     string // Original tool call ID (for sending results back)
	Name   string // Tool name
	Params T      // Parsed, validated parameters
}

// ToolName returns the tool name.
func (c *TypedToolCall[T]) ToolName() string { return c.Name }

// ToolCallID returns the tool call ID.
func (c *TypedToolCall[T]) ToolCallID() string { return c.ID }
