package ops

import (
	"context"
	"fmt"

	"github.com/codewandler/llm"
)

// GenerateParams configures a Generate operation.
type GenerateParams struct {
	// Model is optional; defaults to llm.ModelDefault.
	Model string
	// SystemPrompt is sent as a system message when non-empty.
	SystemPrompt string
	// Temperature controls output randomness (valid range: 0.0–2.0).
	// nil (the default) applies a sensible default of 0.7 for creative/conversational tasks.
	// Use ops.Temp(t) to set an explicit value, including 0 for deterministic output.
	Temperature *float64
}

// Temp returns a pointer to t for use with GenerateParams.Temperature.
// This avoids taking the address of a float64 literal at the call site:
//
//	ops.GenerateParams{Temperature: ops.Temp(0.0)} // deterministic
//	ops.GenerateParams{Temperature: ops.Temp(1.5)} // creative
func Temp(t float64) *float64 { return &t }

// GenerateResult holds the output of a Generate operation.
type GenerateResult struct {
	Text string
}

// generateFactory implements Factory[GenerateParams, string, GenerateResult].
type generateFactory struct{}

// compile-time interface assertions
var _ Factory[GenerateParams, string, GenerateResult] = generateFactory{}
var _ Operation[string, GenerateResult] = (*generateOp)(nil)

// Generate is the built-in factory for free-form text generation.
//
// Preset: Temperature=0.7 (when unset), Thinking=auto.
//
//	op := ops.Generate.New(provider, ops.GenerateParams{SystemPrompt: "Be concise."})
//	result, err := op.Run(ctx, "Write a haiku about Go channels.")
var Generate generateFactory

func (generateFactory) New(provider llm.Provider, params GenerateParams) Operation[string, GenerateResult] {
	return &generateOp{
		runner: newRunner(provider, params.Model),
		params: params,
	}
}

type generateOp struct {
	runner *opRunner
	params GenerateParams
}

func (o *generateOp) Run(ctx context.Context, input string) (*GenerateResult, error) {
	temp := 0.7
	if o.params.Temperature != nil {
		temp = *o.params.Temperature
	}

	b := o.runner.builder().Temperature(temp)
	if o.params.SystemPrompt != "" {
		b = b.System(o.params.SystemPrompt)
	}

	result, err := o.runner.run(ctx, b.User(input))
	if err != nil {
		return nil, fmt.Errorf("ops generate: %w", err)
	}
	return &GenerateResult{Text: result.Text()}, nil
}
