package llm

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/codewandler/llm/tool"
)

type Result interface {
	Response
	Next() Messages
}

type result struct {
	textBuf, reasoningBuf strings.Builder
	assistantMsg          AssistantMessage
	stopReason            StopReason
	usage                 *Usage
	toolCalls             []tool.Call
	toolResults           []tool.Result
	errors                []error
}

func (r *result) Message() AssistantMessage { return r.assistantMsg }
func (r *result) Text() string              { return r.textBuf.String() }
func (r *result) Reasoning() string         { return r.reasoningBuf.String() }
func (r *result) StopReason() StopReason    { return r.stopReason }
func (r *result) Usage() *Usage             { return r.usage }
func (r *result) Error() error              { return errors.Join(r.errors...) }
func (r *result) ToolCalls() []tool.Call    { return r.toolCalls }

func (r *result) addError(err error) {
	r.errors = append(r.errors, err)
}

func (r *result) applyDelta(ev *DeltaEvent) {
	switch ev.Kind {
	case DeltaKindText:
		r.textBuf.WriteString(ev.Text)
	case DeltaKindReasoning:
		r.reasoningBuf.WriteString(ev.Reasoning)
	case DeltaTypeTool:
		// TODO: write partial tool data
	}
}

func (r *result) applyUsage(u *Usage) {
	if r.usage == nil {
		r.usage = u
	} else {
		// TODO merge usage
	}
}

func (r *result) applyToolCall(tc tool.Call) {
	r.toolCalls = append(r.toolCalls, tc)
}

var _ Result = (*result)(nil)

func (r *result) Next() (next Messages) {
	if r.assistantMsg != nil {
		next.Add(r.assistantMsg)
	}
	for _, tr := range r.toolResults {
		next.Add(ToolResult(tr))
	}
	return next
}

type StreamProcessor struct {
	ctx           context.Context
	result        *result
	done          chan struct{}
	mu            sync.Mutex
	ch            <-chan Envelope
	dispatcher    tool.DispatcherType
	eventHandlers []EventHandler
	toolHandlers  tool.Handlers
	once          sync.Once
}

func ProcessChan(ctx context.Context, ch <-chan Envelope) *StreamProcessor {
	return &StreamProcessor{
		ctx:           ctx,
		ch:            ch,
		dispatcher:    tool.DispatchTypeSync,
		toolHandlers:  tool.NewHandlers(),
		eventHandlers: make([]EventHandler, 0),
		result:        &result{},
		done:          make(chan struct{}, 1),
	}
}

func (r *StreamProcessor) OnEvent(fn EventHandler) *StreamProcessor {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.eventHandlers = append(r.eventHandlers, fn)
	return r
}

// OnStart registers a callback that is called when the StreamEventStarted event
// arrives, carrying provider metadata (request ToolCallID, model, time-to-first-token).
func (r *StreamProcessor) OnStart(fn TypedEventHandler[StreamStartedEvent]) *StreamProcessor {
	return r.OnEvent(fn)
}

func (r *StreamProcessor) OnDelta(fn TypedEventHandler[DeltaEvent]) *StreamProcessor {
	return r.OnEvent(fn)
}

func (r *StreamProcessor) onDeltaKind(k DeltaKind, fn func(d DeltaEvent)) *StreamProcessor {
	return r.OnDelta(func(d DeltaEvent) {
		if d.Kind != k {
			return
		}
		fn(d)
	})
}

// OnTextDelta registers a callback that is called for each incremental text token.
// Panics in the callback are recovered and recorded on the StreamResult error.
func (r *StreamProcessor) OnTextDelta(fn func(delta string)) *StreamProcessor {
	return r.onDeltaKind(DeltaKindText, func(d DeltaEvent) {
		fn(d.Text)
	})
}

// OnReasoningDelta registers a callback that is called for each incremental
// reasoning/thinking token.
func (r *StreamProcessor) OnReasoningDelta(fn func(delta string)) *StreamProcessor {
	return r.onDeltaKind(DeltaKindReasoning, func(d DeltaEvent) {
		fn(d.Reasoning)
	})
}

// OnToolDelta registers a callback that is called for each partial tool-call
// argument fragment (DeltaTypeTool deltas).
func (r *StreamProcessor) OnToolDelta(fn func(d ToolDeltaPart)) *StreamProcessor {
	return r.onDeltaKind(DeltaTypeTool, func(d DeltaEvent) {
		fn(d.ToolDeltaPart)
	})
}

// HandleTool registers a Handler that is invoked when the model emits a
// completed tool call matching h.ToolName(). The handler's output is stored in
// StreamResult.ToolResults and included in the messages returned by Next/Apply.
//
// Pass a *BoundToolSpec (from llm.Handle) for typed, spec-aware handlers:
//
//	proc.HandleTool(llm.Handle(weatherSpec, func(ctx context.Context, in GetWeatherParams) (*GetWeatherResult, error) {
//	    return &GetWeatherResult{Temp: 22}, nil
//	}))
func (r *StreamProcessor) HandleTool(handlers ...tool.NamedHandler) *StreamProcessor {
	r.toolHandlers.Append(handlers...)

	return r
}

// WithAsyncToolDispatch switches tool handler dispatch to concurrent mode: all tool
// calls emitted in a single response are executed in parallel, one goroutine
// per call. Results are collected in emission order before the eventPub is
// considered complete.
func (r *StreamProcessor) WithAsyncToolDispatch() *StreamProcessor {
	r.dispatcher = tool.DispatchTypeAsync
	return r
}

// WithToolDispatcher sets the tool dispatcher explicitly.
func (r *StreamProcessor) WithToolDispatcher(d tool.DispatcherType) *StreamProcessor {
	r.dispatcher = d
	return r
}

// Result starts consuming the eventPub (at most once) and returns a channel
// that yields exactly one *StreamResult when the eventPub is fully processed.
// The channel is closed after the result is sent.
//
// Calling Result() multiple times is safe — the eventPub is only consumed once
// and the same channel is returned on subsequent calls.
func (r *StreamProcessor) Result() (Result, error) {
	r.once.Do(func() { go r.doProcess() })
	<-r.done
	return r.result, r.result.Error()
}

func (r *StreamProcessor) dispatchEvent(e Envelope) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ev := e.Data.(Event)
	for _, h := range r.eventHandlers {
		func(h EventHandler, ev Event) {
			var err error
			tool.SafeCall(func() {
				h.Handle(ev)
			}, &err)
			if err != nil {
				r.result.addError(err)
			}
		}(h, ev)
	}
}

// doProcess is the internal goroutine that drains the event channel.
func (r *StreamProcessor) doProcess() {

	defer func() {
		close(r.done)
	}()

	res := r.result

	for {
		select {
		case <-r.ctx.Done():
			res.stopReason = StopReasonCancelled
			res.addError(r.ctx.Err())
			return

		case ev, ok := <-r.ch:
			if !ok {
				if res.stopReason == "" {
					res.stopReason = StopReasonEndTurn
				}
				return
			}

			r.processEvent(ev)
		}
	}
}

func (r *StreamProcessor) processEvent(e Envelope) {
	ev := e.Data

	switch actual := ev.(type) {
	case *DeltaEvent:
		r.result.applyDelta(actual)
	case *ToolCallEvent:
		r.result.applyToolCall(actual.ToolCall)
	case *CompletedEvent:
		r.result.stopReason = actual.StopReason
	case *UsageUpdatedEvent:
		r.result.applyUsage(&actual.Usage)
	case *ErrorEvent:
		r.result.addError(actual.Error)
		r.result.stopReason = StopReasonError
	}

	r.dispatchEvent(e)
}
