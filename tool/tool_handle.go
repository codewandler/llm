package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

var (
	ErrNoHandler = fmt.Errorf("no handler for tool")
)

// --- Handler ---

type Handler interface {
	Handle(ctx context.Context, call Call) (any, error)
}

type NamedHandler interface {
	Handler
	ToolName() string
}

type Handlers map[string]Handler

func (m Handlers) Append(handlers ...NamedHandler) {
	for _, h := range handlers {
		m[h.ToolName()] = h
	}
}

func (m Handlers) Handle(ctx context.Context, call Call) (res any, err error) {
	h, ok := m[call.ToolName()]
	if !ok {
		return res, fmt.Errorf("%w: %s", ErrNoHandler, call.ToolName())
	}
	return h.Handle(ctx, call)
}

func NewHandlers(handlers ...NamedHandler) Handlers {
	m := make(Handlers)
	for _, h := range handlers {
		m[h.ToolName()] = h
	}
	return m
}

// --- BoundToolSpec ---

// BoundToolSpec pairs a Spec[In] with a handler function, satisfying both
// Handler (for StreamProcessor.HandleTool) and toolRegistration (for NewToolSet).
// Create one with the package-level Handle function.
type BoundToolSpec[In, Out any] struct {
	spec *Spec[In]
	fn   func(ctx context.Context, in In) (*Out, error)
}

// namedToolHandler is a lightweight Handler with an explicit name and no spec.
type namedToolHandler[In, Out any] struct {
	name string
	fn   func(ctx context.Context, in In) (*Out, error)
}

// NewHandler creates a named Handler from a strongly-typed function
// without requiring a Spec. Use this when you don't need schema
// validation or when the spec is defined elsewhere.
//
// Example:
//
//	proc.HandleTool(llm.NewHandler("get_weather", func(ctx context.Context, in GetWeatherParams) (*GetWeatherResult, error) {
//	    return &GetWeatherResult{Temp: 22}, nil
//	}))
func NewHandler[In, Out any](name string, fn func(ctx context.Context, in In) (*Out, error)) NamedHandler {
	return &namedToolHandler[In, Out]{name: name, fn: fn}
}

func (h *namedToolHandler[In, Out]) ToolName() string { return h.name }

func (h *namedToolHandler[In, Out]) Handle(ctx context.Context, call Call) (any, error) {
	return execTypedHandler(ctx, h.name, call, h.fn)
}

// Handle binds a handler function to a Spec, producing a *BoundToolSpec
// that satisfies both Handler and toolRegistration.
//
// Because Go methods cannot introduce new type parameters, this is a
// package-level generic function rather than a method on Spec.
//
// Example:
//
//	weatherSpec := llm.NewSpec[GetWeatherParams]("get_weather", "Get weather")
//
//	// Register with StreamProcessor:
//	llm.ProcessChan(ctx, ch).
//	    HandleTool(llm.Handle(weatherSpec, func(ctx context.Context, in GetWeatherParams) (*GetWeatherResult, error) {
//	        return &GetWeatherResult{Temp: 22}, nil
//	    }))
//
//	// Or pass directly to NewToolSet — BoundToolSpec satisfies toolRegistration too:
//	tools := llm.NewToolSet(
//	    llm.Handle(weatherSpec, weatherFn),
//	    llm.Handle(searchSpec,  searchFn),
//	)
func Handle[In, Out any](spec *Spec[In], fn func(ctx context.Context, in In) (*Out, error)) *BoundToolSpec[In, Out] {
	return &BoundToolSpec[In, Out]{spec: spec, fn: fn}
}

// ToolName implements Handler — returns the spec's tool name.
func (b *BoundToolSpec[In, Out]) ToolName() string { return b.spec.name }

// Handle implements Handler — validates, unmarshal, calls fn, marshals toolResult.
func (b *BoundToolSpec[In, Out]) Handle(ctx context.Context, call Call) (any, error) {
	return execTypedHandler(ctx, b.spec.name, call, b.fn)
}

// Definition implements toolRegistration — delegates to the embedded spec.
func (b *BoundToolSpec[In, Out]) Definition() Definition { return b.spec.Definition() }

// parse implements toolRegistration — delegates to the embedded spec.
func (b *BoundToolSpec[In, Out]) parse(raw Call) (ParsedToolCall, error) {
	return b.spec.parse(raw)
}

// --- helpers ---

// execTypedHandler is the shared marshal→unmarshal→call→marshal pipeline used
// by both namedToolHandler and BoundToolSpec.
func execTypedHandler[In, Out any](ctx context.Context, name string, call Call, fn func(context.Context, In) (*Out, error)) (string, error) {
	raw, err := json.Marshal(call.ToolArgs())
	if err != nil {
		return "", fmt.Errorf("tool %s: marshal args: %w", name, err)
	}
	var in In
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("tool %s: parse args: %w", name, err)
	}
	out, err := fn(ctx, in)
	if err != nil {
		return "", err
	}
	result, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("tool %s: marshal toolResult: %w", name, err)
	}
	return string(result), nil
}

func SafeCall(fn func(), errPtr *error) {
	defer func() {
		if p := recover(); p != nil {
			if *errPtr == nil {
				*errPtr = fmt.Errorf("callback panic: %v", p)
			}
		}
	}()
	fn()
}

// SafeHandle calls h.Handle inside a panic-recovery wrapper.
func SafeHandle(ctx context.Context, h Handler, call Call) (output any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("tool handler %s panic: %v", call.ToolName(), p)
		}
	}()
	return h.Handle(ctx, call)
}
