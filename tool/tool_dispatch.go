package tool

import (
	"context"
	"fmt"
)

type Dispatcher interface {
	Dispatch(ctx context.Context, toolCalls ...Call) (results []Result, err error)
}

// --- DispatcherType ---

// DispatcherType controls how tool calls are executed when multiple tools are
// emitted in a single response.
type DispatcherType int

const (
	// DispatchTypeSync executes tool handlers one at a time in emission order.
	// This is the default.
	DispatchTypeSync DispatcherType = iota

	// DispatchTypeAsync executes all tool handlers concurrently, one goroutine
	// per tool call. Results are collected in emission order.
	DispatchTypeAsync
)

type syncDispatcher struct {
	h Handler
}

func NewSyncDispatcher(h Handler) Dispatcher {
	return &syncDispatcher{h}
}

func (d *syncDispatcher) Dispatch(ctx context.Context, toolCalls ...Call) ([]Result, error) {
	var results []Result
	for _, tc := range toolCalls {
		res, err := dispatchOne(ctx, d.h, tc)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return results, nil
}

func dispatchOne(ctx context.Context, h Handler, tc Call) (Result, error) {
	x, err := h.Handle(ctx, tc)
	if err != nil {
		return nil, fmt.Errorf("tool handler %s panic: %v", tc.ToolName(), err)
	}

	var tr Result
	if _, ok := x.(Result); ok {
		tr = x.(Result)
	} else {
		tr = NewResult(tc.ToolCallID(), x, false)
	}
	return tr, nil
}
