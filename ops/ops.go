// Package ops provides parameterised, use-case-oriented LLM operations.
//
// Three generic type parameters drive every operation:
//
//	F — factory params: static configuration decided at wiring time
//	I — runtime input type
//	O — runtime output type
//
// Pattern:
//
//	op := ops.Classify.New(provider, ops.ClassifyParams{Labels: []string{"pos", "neg"}})
//	result, err := op.Run(ctx, "This is great!")
//	// result.Label == "pos"
package ops

import (
	"context"

	"github.com/codewandler/llm"
)

// Factory[F, I, O] creates Operations parameterised by F.
type Factory[F, I, O any] interface {
	New(provider llm.Provider, params F) Operation[I, O]
}

// Operation[I, O] executes a single LLM-backed operation and blocks until
// the result is available.
type Operation[I, O any] interface {
	Run(ctx context.Context, input I) (*O, error)
}

// factoryFunc is the concrete type returned by NewFactory.
type factoryFunc[F, I, O any] struct {
	fn func(llm.Provider, F) Operation[I, O]
}

func (f factoryFunc[F, I, O]) New(provider llm.Provider, params F) Operation[I, O] {
	return f.fn(provider, params)
}

// NewFactory wraps a function into a Factory — the primary helper for users
// building custom operations:
//
//	var Summarize = ops.NewFactory(func(p llm.Provider, params SummarizeParams) ops.Operation[string, SummaryResult] {
//	    return ops.NewMap[SummaryResult](p, ops.MapParams{Hint: "Summarize concisely."})
//	})
func NewFactory[F, I, O any](fn func(llm.Provider, F) Operation[I, O]) Factory[F, I, O] {
	return factoryFunc[F, I, O]{fn: fn}
}

// OperationFunc adapts a plain function to the Operation interface.
//
//	op := ops.OperationFunc[string, MyResult](func(ctx context.Context, input string) (*MyResult, error) {
//	    return &MyResult{Value: process(input)}, nil
//	})
type OperationFunc[I, O any] func(context.Context, I) (*O, error)

func (f OperationFunc[I, O]) Run(ctx context.Context, input I) (*O, error) {
	return f(ctx, input)
}

// opRunner is shared internal machinery for all built-in operations.
// It holds the bound provider and resolved model name.
type opRunner struct {
	provider llm.Provider
	model    string
}

func newRunner(provider llm.Provider, model string) *opRunner {
	if model == "" {
		model = llm.ModelDefault
	}
	return &opRunner{provider: provider, model: model}
}

// builder returns a RequestBuilder pre-configured with the bound model.
// Each op layers its presets and messages on this, then passes it directly
// to run() — no manual Build() call needed.
func (r *opRunner) builder() *llm.RequestBuilder {
	return llm.NewRequestBuilder().Model(r.model)
}

// run passes b directly to CreateStream (which calls BuildRequest internally),
// drains the stream via NewEventProcessor, and returns the accumulated Result.
// Validation errors from Build surface here via CreateStream.
func (r *opRunner) run(ctx context.Context, b llm.Buildable) (llm.Result, error) {
	ch, err := r.provider.CreateStream(ctx, b)
	if err != nil {
		return nil, err
	}
	res := llm.NewEventProcessor(ctx, ch).Result()
	return res, res.Error()
}
