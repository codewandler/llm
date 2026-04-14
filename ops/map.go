package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
)

const mapToolName = "extract"

// MapMode selects the internal extraction strategy.
type MapMode string

const (
	// MapModeTool (default) defines the output type as a tool schema and
	// forces the model to call it. More reliable across providers.
	MapModeTool MapMode = "tool"
	// MapModeJSON uses OutputFormat=JSON with a schema-describing system prompt.
	// Simpler but the model can deviate from the schema.
	MapModeJSON MapMode = "json"
)

// MapParams configures a Map operation.
type MapParams struct {
	// Model is optional; defaults to llm.ModelDefault.
	Model string
	// Hint is appended to the system prompt to provide extra context,
	// e.g. "The document is in German." or "Focus on line-item amounts only."
	Hint string
	// Mode selects the extraction strategy. Defaults to MapModeTool.
	Mode MapMode
}

// MapFactory[O] implements Factory[MapParams, string, O].
// Use when you need to store a Factory in a typed variable:
//
//	var f ops.Factory[ops.MapParams, string, InvoiceData] = ops.MapFactory[InvoiceData]{}
//	op := f.New(provider, params)
type MapFactory[O any] struct{}

// compile-time interface assertions
var _ Factory[MapParams, string, struct{}] = MapFactory[struct{}]{}
var _ Operation[string, struct{}] = (*mapOp[struct{}])(nil)

func (MapFactory[O]) New(provider llm.Provider, params MapParams) Operation[string, O] {
	return newMapOp[O](provider, params)
}

// NewMap is a convenience constructor (avoids explicit struct instantiation):
//
//	op := ops.NewMap[InvoiceData](provider, ops.MapParams{Hint: "Extract invoice fields."})
func NewMap[O any](provider llm.Provider, params MapParams) Operation[string, O] {
	return newMapOp[O](provider, params)
}

func newMapOp[O any](provider llm.Provider, params MapParams) Operation[string, O] {
	return &mapOp[O]{
		runner: newRunner(provider, params.Model),
		params: params,
	}
}

type mapOp[O any] struct {
	runner *opRunner
	params MapParams
}

func (o *mapOp[O]) Run(ctx context.Context, input string) (*O, error) {
	if o.params.Mode == MapModeJSON {
		return o.runJSON(ctx, input)
	}
	return o.runTool(ctx, input)
}

// runTool uses a tool-forced output schema.
// The tool description carries the extraction instruction; the system message
// carries only the optional hint to avoid duplication.
func (o *mapOp[O]) runTool(ctx context.Context, input string) (*O, error) {
	def := tool.DefinitionFor[O](mapToolName, "Extract the requested structured data from the input.")

	b := o.runner.builder().
		Thinking(llm.ThinkingOff).
		Temperature(0).
		Tools(def).
		ToolChoice(llm.ToolChoiceTool{Name: mapToolName})
	if o.params.Hint != "" {
		b = b.System(o.params.Hint)
	}

	result, err := o.runner.run(ctx, b.User(input))
	if err != nil {
		return nil, fmt.Errorf("ops map: %w", err)
	}

	calls := result.ToolCalls()
	if len(calls) == 0 {
		return nil, fmt.Errorf("ops map: model did not return structured output")
	}

	data, err := json.Marshal(calls[0].ToolArgs())
	if err != nil {
		return nil, fmt.Errorf("ops map: marshal args: %w", err)
	}

	var out O
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("ops map: unmarshal: %w", err)
	}
	return &out, nil
}

// runJSON uses JSON output mode with a schema-describing system prompt.
func (o *mapOp[O]) runJSON(ctx context.Context, input string) (*O, error) {
	def := tool.DefinitionFor[O](mapToolName, "")
	schemaBytes, err := json.MarshalIndent(def.Parameters, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("ops map: build schema: %w", err)
	}

	parts := []string{"Extract the requested structured data from the input."}
	if o.params.Hint != "" {
		parts = append(parts, o.params.Hint)
	}
	parts = append(parts, fmt.Sprintf(
		"Respond with a JSON object matching this schema:\n```json\n%s\n```",
		string(schemaBytes),
	))

	result, err := o.runner.run(ctx, o.runner.builder().
		Thinking(llm.ThinkingOff).
		Temperature(0).
		OutputFormat(llm.OutputFormatJSON).
		System(strings.Join(parts, "\n\n")).
		User(input))
	if err != nil {
		return nil, fmt.Errorf("ops map: %w", err)
	}

	var out O
	if err := json.Unmarshal([]byte(result.Text()), &out); err != nil {
		return nil, fmt.Errorf("ops map: unmarshal: %w", err)
	}
	return &out, nil
}
