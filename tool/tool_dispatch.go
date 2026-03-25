package tool

import (
	"context"
	"errors"
	"sync"
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
	x, err := SafeHandle(ctx, h, tc)
	if err != nil {
		return NewResult(tc.ToolCallID(), err.Error(), true), nil
	}

	var tr Result
	if _, ok := x.(Result); ok {
		tr = x.(Result)
	} else {
		tr = NewResult(tc.ToolCallID(), x, false)
	}
	return tr, nil
}

type AsyncDispatcher struct {
	Handlers Handlers
}

func (d *AsyncDispatcher) Dispatch(ctx context.Context, toolCalls ...Call) ([]Result, error) {
	results := make([]Result, len(toolCalls))
	errCh := make(chan error, len(toolCalls))
	var wg sync.WaitGroup

	for i, tc := range toolCalls {
		wg.Add(1)
		go func(i int, tc Call) {
			defer wg.Done()
			res, err := SafeHandle(ctx, d.Handlers, tc)
			if err != nil {
				results[i] = NewResult(tc.ToolCallID(), err.Error(), true)
				return
			}
			results[i] = NewResult(tc.ToolCallID(), res, false)
		}(i, tc)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	return results, errors.Join(errs...)
}
