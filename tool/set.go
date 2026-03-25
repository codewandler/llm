package tool

import (
	"errors"
	"fmt"
)

// --- Set ---

// Set manages a collection of tool specifications.
// It provides tool definitions for sending to providers and parses raw tool calls
// into strongly-typed results with validation.
type Set struct {
	tools []toolRegistration          // ordered for Definitions()
	index map[string]toolRegistration // keyed by name for Parse()
}

// NewToolSet creates a Set from one or more tool specs.
//
// Example:
//
//	tools := NewToolSet(
//	    NewSpec[GetWeatherParams]("get_weather", "Get weather"),
//	    NewSpec[SearchParams]("search", "Search the web"),
//	)
func NewToolSet(tools ...toolRegistration) *Set {
	index := make(map[string]toolRegistration, len(tools))
	for _, tool := range tools {
		def := tool.Definition()
		index[def.Name] = tool
	}
	return &Set{
		tools: tools,
		index: index,
	}
}

// Definitions returns all tool definitions for sending to providers.
func (ts *Set) Definitions() []Definition {
	defs := make([]Definition, len(ts.tools))
	for i, tool := range ts.tools {
		defs[i] = tool.Definition()
	}
	return defs
}

// Parse converts raw ToolCalls (from eventPub events) into typed ParsedToolCalls.
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
func (ts *Set) Parse(calls []Call) ([]ParsedToolCall, error) {
	var result []ParsedToolCall
	var errs []error

	for _, call := range calls {
		reg, ok := ts.index[call.ToolName()]
		if !ok {
			errs = append(errs, fmt.Errorf("unknown tool: %s", call.ToolName()))
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
