package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/codewandler/llm"
)

// IntentDef defines a single intent with a name, description, and optional
// few-shot examples. Examples give the model concrete guidance and improve
// accuracy over plain Classify for NLU tasks.
type IntentDef struct {
	// Name is the canonical identifier returned in IntentResult.
	Name string
	// Description explains when this intent applies.
	Description string
	// Examples are representative user utterances (few-shot guidance).
	// Optional but strongly recommended.
	Examples []string
}

// IntentParams configures an Intent operation.
type IntentParams struct {
	// Intents is the set of possible intents. At least one required.
	Intents []IntentDef
	// Model is optional; defaults to llm.ModelDefault.
	Model string
	// Hint is optional extra context appended to the system prompt,
	// e.g. "The user may write in informal language."
	Hint string
}

// IntentResult holds the output of an Intent operation.
type IntentResult struct {
	// Name matches the Name field of the matched IntentDef.
	Name string
}

// intentFactory implements Factory[IntentParams, string, IntentResult].
type intentFactory struct{}

// compile-time interface assertions
var _ Factory[IntentParams, string, IntentResult] = intentFactory{}
var _ Operation[string, IntentResult] = (*intentOp)(nil)

// Intent is the built-in factory for intent detection.
//
// Unlike Classify, each intent carries a description and optional few-shot
// examples, making it significantly more reliable for NLU tasks.
//
// Preset: Temperature=0, Thinking=off.
//
//	op := ops.Intent.New(provider, ops.IntentParams{
//	    Intents: []ops.IntentDef{
//	        {Name: "book_flight", Description: "User wants to book a flight",
//	         Examples: []string{"fly me to Paris", "book a ticket to Berlin"}},
//	        {Name: "cancel_order", Description: "User wants to cancel an order",
//	         Examples: []string{"cancel my order", "I want a refund"}},
//	    },
//	})
//	result, err := op.Run(ctx, "I need to get to Berlin next week.")
//	// result.Name == "book_flight"
var Intent intentFactory

func (intentFactory) New(provider llm.Provider, params IntentParams) Operation[string, IntentResult] {
	return &intentOp{runner: newRunner(provider, params.Model), params: params}
}

type intentOp struct {
	runner *opRunner
	params IntentParams
}

func (o *intentOp) Run(ctx context.Context, input string) (*IntentResult, error) {
	if len(o.params.Intents) == 0 {
		return nil, fmt.Errorf("ops intent: Intents must not be empty")
	}

	var sb strings.Builder
	sb.WriteString("Detect the intent of the input. Respond with only the intent name, nothing else.\n\nIntents:")
	for _, def := range o.params.Intents {
		sb.WriteString(fmt.Sprintf("\n- %s: %s", def.Name, def.Description))
		if len(def.Examples) > 0 {
			quoted := make([]string, len(def.Examples))
			for i, ex := range def.Examples {
				quoted[i] = fmt.Sprintf("%q", ex)
			}
			sb.WriteString(fmt.Sprintf("\n  Examples: %s", strings.Join(quoted, ", ")))
		}
	}
	if o.params.Hint != "" {
		sb.WriteString("\n\n" + o.params.Hint)
	}

	result, err := o.runner.run(ctx, o.runner.builder().
		Thinking(llm.ThinkingOff).
		Temperature(0).
		System(sb.String()).
		User(input))
	if err != nil {
		return nil, fmt.Errorf("ops intent: %w", err)
	}

	got := strings.TrimSpace(result.Text())
	for _, def := range o.params.Intents {
		if strings.EqualFold(got, def.Name) {
			return &IntentResult{Name: def.Name}, nil
		}
	}

	names := make([]string, len(o.params.Intents))
	for i, d := range o.params.Intents {
		names[i] = d.Name
	}
	return nil, fmt.Errorf("ops intent: model returned unknown intent %q (valid: %s)",
		got, strings.Join(names, ", "))
}
