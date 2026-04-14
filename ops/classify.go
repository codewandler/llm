package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/codewandler/llm"
)

// ClassifyParams configures a Classify operation.
type ClassifyParams struct {
	// Labels is the set of valid output categories. At least one required.
	Labels []string
	// Model is optional; defaults to llm.ModelDefault.
	Model string
	// Hint is optional extra context appended to the system prompt,
	// e.g. "The text may use informal abbreviations."
	Hint string
}

// ClassifyResult holds the output of a Classify operation.
type ClassifyResult struct {
	// Label is the canonical label from ClassifyParams.Labels (case-normalised
	// to the original casing in Labels).
	Label string
}

// classifyFactory implements Factory[ClassifyParams, string, ClassifyResult].
type classifyFactory struct{}

// compile-time interface assertions
var _ Factory[ClassifyParams, string, ClassifyResult] = classifyFactory{}
var _ Operation[string, ClassifyResult] = (*classifyOp)(nil)

// Classify is the built-in factory for text classification.
//
// Preset: Temperature=0, Thinking=off.
//
//	op := ops.Classify.New(provider, ops.ClassifyParams{
//	    Labels: []string{"positive", "negative", "neutral"},
//	})
//	result, err := op.Run(ctx, "This is great!")
//	// result.Label == "positive"
var Classify classifyFactory

func (classifyFactory) New(provider llm.Provider, params ClassifyParams) Operation[string, ClassifyResult] {
	return &classifyOp{runner: newRunner(provider, params.Model), params: params}
}

type classifyOp struct {
	runner *opRunner
	params ClassifyParams
}

func (o *classifyOp) Run(ctx context.Context, input string) (*ClassifyResult, error) {
	if len(o.params.Labels) == 0 {
		return nil, fmt.Errorf("ops classify: Labels must not be empty")
	}

	system := fmt.Sprintf(
		"Classify the input into exactly one of these categories: %s\nRespond with only the category name, nothing else.",
		strings.Join(o.params.Labels, ", "),
	)
	if o.params.Hint != "" {
		system += "\n\n" + o.params.Hint
	}

	result, err := o.runner.run(ctx, o.runner.builder().
		Thinking(llm.ThinkingOff).
		Temperature(0).
		System(system).
		User(input))
	if err != nil {
		return nil, fmt.Errorf("ops classify: %w", err)
	}

	got := strings.TrimSpace(result.Text())
	for _, l := range o.params.Labels {
		if strings.EqualFold(got, l) {
			return &ClassifyResult{Label: l}, nil
		}
	}
	return nil, fmt.Errorf("ops classify: model returned unknown label %q (valid: %s)",
		got, strings.Join(o.params.Labels, ", "))
}
