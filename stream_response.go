package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// --- StopReason ---

// StopReason describes why the model stopped generating.
type StopReason string

const (
	// StopReasonEndTurn is natural completion — the model finished its response.
	StopReasonEndTurn StopReason = "end_turn"
	// StopReasonToolUse means the model emitted one or more tool calls.
	StopReasonToolUse StopReason = "tool_use"
	// StopReasonMaxTokens means the output length limit was reached.
	StopReasonMaxTokens StopReason = "max_tokens"
	// StopReasonContentFilter means output was blocked by the provider.
	StopReasonContentFilter StopReason = "content_filter"
	// StopReasonCancelled means the context was cancelled before the stream ended.
	StopReasonCancelled StopReason = "cancelled"
	// StopReasonError means the stream ended with a StreamEventError.
	StopReasonError StopReason = "error"
)

// --- ToolHandler ---

// ToolHandler is a self-describing executor for a single tool.
// It knows its own name (used for registration) and can execute a raw ToolCall.
type ToolHandler interface {
	// ToolName returns the name this handler is registered for.
	ToolName() string
	// Handle executes the tool call and returns its output as a string.
	// The string is stored verbatim as the ToolCallResult content.
	Handle(ctx context.Context, call ToolCall) (string, error)
}

// --- BoundToolSpec ---

// BoundToolSpec pairs a ToolSpec[In] with a handler function, satisfying both
// ToolHandler (for StreamResponse.HandleTool) and toolRegistration (for NewToolSet).
// Create one with the package-level Handle function.
type BoundToolSpec[In, Out any] struct {
	spec *ToolSpec[In]
	fn   func(ctx context.Context, in In) (*Out, error)
}

// namedToolHandler is a lightweight ToolHandler with an explicit name and no spec.
type namedToolHandler[In, Out any] struct {
	name string
	fn   func(ctx context.Context, in In) (*Out, error)
}

// NewToolHandler creates a named ToolHandler from a strongly-typed function
// without requiring a ToolSpec. Use this when you don't need schema
// validation or when the spec is defined elsewhere.
//
// Example:
//
//	proc.HandleTool(llm.NewToolHandler("get_weather", func(ctx context.Context, in GetWeatherParams) (*GetWeatherResult, error) {
//	    return &GetWeatherResult{Temp: 22}, nil
//	}))
func NewToolHandler[In, Out any](name string, fn func(ctx context.Context, in In) (*Out, error)) ToolHandler {
	return &namedToolHandler[In, Out]{name: name, fn: fn}
}

func (h *namedToolHandler[In, Out]) ToolName() string { return h.name }

func (h *namedToolHandler[In, Out]) Handle(ctx context.Context, call ToolCall) (string, error) {
	return execTypedHandler(ctx, h.name, call, h.fn)
}

// Handle binds a handler function to a ToolSpec, producing a *BoundToolSpec
// that satisfies both ToolHandler and toolRegistration.
//
// Because Go methods cannot introduce new type parameters, this is a
// package-level generic function rather than a method on ToolSpec.
//
// Example:
//
//	weatherSpec := llm.NewToolSpec[GetWeatherParams]("get_weather", "Get weather")
//
//	// Register with StreamResponse:
//	llm.Process(ctx, ch).
//	    HandleTool(llm.Handle(weatherSpec, func(ctx context.Context, in GetWeatherParams) (*GetWeatherResult, error) {
//	        return &GetWeatherResult{Temp: 22}, nil
//	    }))
//
//	// Or pass directly to NewToolSet — BoundToolSpec satisfies toolRegistration too:
//	tools := llm.NewToolSet(
//	    llm.Handle(weatherSpec, weatherFn),
//	    llm.Handle(searchSpec,  searchFn),
//	)
func Handle[In, Out any](spec *ToolSpec[In], fn func(ctx context.Context, in In) (*Out, error)) *BoundToolSpec[In, Out] {
	return &BoundToolSpec[In, Out]{spec: spec, fn: fn}
}

// ToolName implements ToolHandler — returns the spec's tool name.
func (b *BoundToolSpec[In, Out]) ToolName() string { return b.spec.name }

// Handle implements ToolHandler — validates, unmarshals, calls fn, marshals result.
func (b *BoundToolSpec[In, Out]) Handle(ctx context.Context, call ToolCall) (string, error) {
	return execTypedHandler(ctx, b.spec.name, call, b.fn)
}

// Definition implements toolRegistration — delegates to the embedded spec.
func (b *BoundToolSpec[In, Out]) Definition() ToolDefinition { return b.spec.Definition() }

// parse implements toolRegistration — delegates to the embedded spec.
func (b *BoundToolSpec[In, Out]) parse(raw ToolCall) (ParsedToolCall, error) {
	return b.spec.parse(raw)
}

// --- ToolDispatcher ---

// ToolDispatcher controls how tool calls are executed when multiple tools are
// emitted in a single response.
type ToolDispatcher int

const (
	// ToolDispatchSync executes tool handlers one at a time in emission order.
	// This is the default.
	ToolDispatchSync ToolDispatcher = iota

	// ToolDispatchAsync executes all tool handlers concurrently, one goroutine
	// per tool call. Results are collected in emission order.
	ToolDispatchAsync
)

// --- StreamResult ---

// StreamResult is the final accumulated result of a processed stream.
// It is delivered exactly once on the channel returned by StreamResponse.Result().
type StreamResult struct {
	// Text is the concatenation of all DeltaTypeText deltas.
	Text string

	// Reasoning is the concatenation of all DeltaTypeReasoning deltas.
	Reasoning string

	// ToolCalls contains every tool call emitted by the model, in order.
	ToolCalls []ToolCall

	// ToolResults holds the output of every executed tool handler, in the same
	// order as ToolCalls. Entries are present only when a ToolHandler was
	// registered for the tool name; unhandled tools have no entry here.
	ToolResults []ToolCallResult

	// Usage holds token counts and cost. Nil if the provider did not report usage.
	Usage *Usage

	// Start holds the stream metadata emitted by StreamEventStart.
	// Nil if the provider did not emit a start event.
	Start *StreamStart

	// StopReason describes why the stream ended.
	StopReason StopReason

	err error
}

// Error returns any stream-level error (e.g. provider error, context cancellation).
func (r *StreamResult) Error() error { return r.err }

// Message builds an AssistantMsg from the accumulated result.
// Use this to append the assistant turn to a conversation history.
func (r *StreamResult) Message() *AssistantMsg {
	return &AssistantMsg{
		Content:   r.Text,
		ToolCalls: r.ToolCalls,
	}
}

// Next returns the messages that should be appended to the conversation history
// after this turn: the AssistantMsg followed by one ToolCallResult message for
// each executed tool handler. If no tool handlers ran, only the AssistantMsg
// is returned.
//
// This is the primary convenience for agentic loops:
//
//	msgs.Append(result.Next()...)
func (r *StreamResult) Next() []Message {
	out := make([]Message, 0, 1+len(r.ToolResults))
	out = append(out, r.Message())
	for i := range r.ToolResults {
		out = append(out, &r.ToolResults[i])
	}
	return out
}

// Apply appends the assistant message and any tool results to msgs.
// Equivalent to msgs.Append(result.Next()...).
func (r *StreamResult) Apply(msgs *Messages) {
	msgs.Append(r.Next()...)
}

// --- StreamResponse ---

// StreamResponse is a client-side, stateful stream processor.
// Create one with Process, register callbacks and tool handlers fluently,
// then call Result() to start consuming the stream.
//
// Example:
//
//	weatherSpec := llm.NewToolSpec[GetWeatherParams]("get_weather", "Get weather")
//
//	ch, err := provider.CreateStream(ctx, opts)
//	if err != nil { ... }
//
//	result := <-llm.Process(ctx, ch).
//	    OnText(func(s string) { fmt.Print(s) }).
//	    HandleTool(llm.Handle(weatherSpec, func(ctx context.Context, in GetWeatherParams) (*GetWeatherResult, error) {
//	        return &GetWeatherResult{Temp: 22}, nil
//	    })).
//	    Result()
//
//	if result.Error() != nil { ... }
//	result.Apply(&msgs)
type StreamResponse struct {
	ctx          context.Context
	ch           <-chan StreamEvent
	dispatcher   ToolDispatcher
	onStart      func(*StreamStart)
	onText       func(string)
	onReasoning  func(string)
	onToolDelta  func(*Delta)
	toolHandlers map[string]ToolHandler // keyed by tool name
	resultCh     chan *StreamResult
	once         sync.Once
}

// Process creates a new StreamResponse that will consume ch.
// Call fluent methods to register callbacks and tool handlers, then call
// Result() to begin processing.
func Process(ctx context.Context, ch <-chan StreamEvent) *StreamResponse {
	return &StreamResponse{
		ctx:          ctx,
		ch:           ch,
		dispatcher:   ToolDispatchSync,
		toolHandlers: make(map[string]ToolHandler),
		resultCh:     make(chan *StreamResult, 1),
	}
}

// OnStart registers a callback that is called when the StreamEventStart event
// arrives, carrying provider metadata (request ID, model, time-to-first-token).
func (r *StreamResponse) OnStart(fn func(*StreamStart)) *StreamResponse {
	r.onStart = fn
	return r
}

// OnText registers a callback that is called for each incremental text token.
// Panics in the callback are recovered and recorded on the StreamResult error.
func (r *StreamResponse) OnText(fn func(chunk string)) *StreamResponse {
	r.onText = fn
	return r
}

// OnReasoning registers a callback that is called for each incremental
// reasoning/thinking token.
func (r *StreamResponse) OnReasoning(fn func(chunk string)) *StreamResponse {
	r.onReasoning = fn
	return r
}

// OnToolDelta registers a callback that is called for each partial tool-call
// argument fragment (DeltaTypeTool deltas).
func (r *StreamResponse) OnToolDelta(fn func(d *Delta)) *StreamResponse {
	r.onToolDelta = fn
	return r
}

// HandleTool registers a ToolHandler that is invoked when the model emits a
// completed tool call matching h.ToolName(). The handler's output is stored in
// StreamResult.ToolResults and included in the messages returned by Next/Apply.
//
// Pass a *BoundToolSpec (from llm.Handle) for typed, spec-aware handlers:
//
//	proc.HandleTool(llm.Handle(weatherSpec, func(ctx context.Context, in GetWeatherParams) (*GetWeatherResult, error) {
//	    return &GetWeatherResult{Temp: 22}, nil
//	}))
func (r *StreamResponse) HandleTool(handlers ...ToolHandler) *StreamResponse {
	for _, h := range handlers {
		r.toolHandlers[h.ToolName()] = h
	}

	return r
}

// DispatchAsync switches tool handler dispatch to concurrent mode: all tool
// calls emitted in a single response are executed in parallel, one goroutine
// per call. Results are collected in emission order before the stream is
// considered complete.
func (r *StreamResponse) DispatchAsync() *StreamResponse {
	r.dispatcher = ToolDispatchAsync
	return r
}

// WithToolDispatcher sets the tool dispatcher explicitly.
func (r *StreamResponse) WithToolDispatcher(d ToolDispatcher) *StreamResponse {
	r.dispatcher = d
	return r
}

// Result starts consuming the stream (at most once) and returns a channel
// that yields exactly one *StreamResult when the stream is fully processed.
// The channel is closed after the result is sent.
//
// Calling Result() multiple times is safe — the stream is only consumed once
// and the same channel is returned on subsequent calls.
func (r *StreamResponse) Result() <-chan *StreamResult {
	r.once.Do(func() { go r.run() })
	return r.resultCh
}

// run is the internal goroutine that drains the event channel.
func (r *StreamResponse) run() {
	res := &StreamResult{}

	defer func() {
		r.resultCh <- res
		close(r.resultCh)
	}()

	var textBuf, reasoningBuf string

	for {
		select {
		case <-r.ctx.Done():
			res.Text = textBuf
			res.Reasoning = reasoningBuf
			res.StopReason = StopReasonCancelled
			res.err = r.ctx.Err()
			return

		case ev, ok := <-r.ch:
			if !ok {
				// Channel closed without a done event.
				res.Text = textBuf
				res.Reasoning = reasoningBuf
				if res.StopReason == "" {
					res.StopReason = StopReasonEndTurn
				}
				return
			}

			switch ev.Type {
			case StreamEventStart:
				res.Start = ev.Start
				safecall(func() {
					if r.onStart != nil {
						r.onStart(ev.Start)
					}
				}, &res.err)

			case StreamEventDelta:
				if ev.Delta == nil {
					continue
				}
				switch ev.Delta.Type {
				case DeltaTypeText:
					textBuf += ev.Delta.Text
					safecall(func() {
						if r.onText != nil {
							r.onText(ev.Delta.Text)
						}
					}, &res.err)

				case DeltaTypeReasoning:
					reasoningBuf += ev.Delta.Reasoning
					safecall(func() {
						if r.onReasoning != nil {
							r.onReasoning(ev.Delta.Reasoning)
						}
					}, &res.err)

				case DeltaTypeTool:
					safecall(func() {
						if r.onToolDelta != nil {
							r.onToolDelta(ev.Delta)
						}
					}, &res.err)
				}

			case StreamEventToolCall:
				if ev.ToolCall == nil {
					continue
				}
				res.ToolCalls = append(res.ToolCalls, *ev.ToolCall)

			case StreamEventDone:
				res.Text = textBuf
				res.Reasoning = reasoningBuf
				res.Usage = ev.Usage

				// Infer stop reason: if tools were emitted it's tool_use,
				// otherwise end_turn.
				if len(res.ToolCalls) > 0 {
					res.StopReason = StopReasonToolUse
				} else {
					res.StopReason = StopReasonEndTurn
				}

				// Execute tool handlers.
				r.dispatchTools(res)
				return

			case StreamEventError:
				res.Text = textBuf
				res.Reasoning = reasoningBuf
				res.StopReason = StopReasonError
				if ev.Error != nil {
					res.err = ev.Error
				}
				return
			}
		}
	}
}

// dispatchTools executes handlers for every tool call in res.ToolCalls.
// Results are appended to res.ToolResults in emission order.
// Every tool call produces exactly one ToolCallResult — unhandled tools
// get an error result so the conversation history remains valid.
func (r *StreamResponse) dispatchTools(res *StreamResult) {
	if len(res.ToolCalls) == 0 {
		return
	}

	switch r.dispatcher {
	case ToolDispatchAsync:
		r.dispatchAsync(res)
	default:
		r.dispatchSync(res)
	}
}

// dispatchSync executes handlers one at a time in emission order.
func (r *StreamResponse) dispatchSync(res *StreamResult) {
	for _, call := range res.ToolCalls {
		h, ok := r.toolHandlers[call.Name]
		if !ok {
			res.ToolResults = append(res.ToolResults, ToolCallResult{
				ToolCallID: call.ID,
				Output:     fmt.Sprintf("no handler registered for tool %q", call.Name),
				IsError:    true,
			})
			continue
		}
		output, err := safeHandle(r.ctx, h, call)
		res.ToolResults = append(res.ToolResults, ToolCallResult{
			ToolCallID: call.ID,
			Output:     output,
			IsError:    err != nil,
		})
		if err != nil && res.err == nil {
			res.err = err
		}
	}
}

// dispatchAsync executes all handlers concurrently and collects results in
// emission order.
func (r *StreamResponse) dispatchAsync(res *StreamResult) {
	type slot struct {
		result ToolCallResult
		err    error
	}

	slots := make([]slot, len(res.ToolCalls))
	var wg sync.WaitGroup
	wg.Add(len(res.ToolCalls))

	for i, call := range res.ToolCalls {
		i, call := i, call
		go func() {
			defer wg.Done()
			h, ok := r.toolHandlers[call.Name]
			if !ok {
				slots[i] = slot{result: ToolCallResult{
					ToolCallID: call.ID,
					Output:     fmt.Sprintf("no handler registered for tool %q", call.Name),
					IsError:    true,
				}}
				return
			}
			output, err := safeHandle(r.ctx, h, call)
			slots[i] = slot{
				result: ToolCallResult{
					ToolCallID: call.ID,
					Output:     output,
					IsError:    err != nil,
				},
				err: err,
			}
		}()
	}
	wg.Wait()

	for _, s := range slots {
		res.ToolResults = append(res.ToolResults, s.result)
		if s.err != nil && res.err == nil {
			res.err = s.err
		}
	}
}

// --- helpers ---

// execTypedHandler is the shared marshal→unmarshal→call→marshal pipeline used
// by both namedToolHandler and BoundToolSpec.
func execTypedHandler[In, Out any](ctx context.Context, name string, call ToolCall, fn func(context.Context, In) (*Out, error)) (string, error) {
	raw, err := json.Marshal(call.Arguments)
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
		return "", fmt.Errorf("tool %s: marshal result: %w", name, err)
	}
	return string(result), nil
}

// safecall runs fn and, if it panics, converts the panic to an error.
// If errp already holds an error the panic error is discarded (first error wins).
func safecall(fn func(), errp *error) {
	defer func() {
		if p := recover(); p != nil {
			if *errp == nil {
				*errp = fmt.Errorf("callback panic: %v", p)
			}
		}
	}()
	fn()
}

// safeHandle calls h.Handle inside a panic-recovery wrapper.
func safeHandle(ctx context.Context, h ToolHandler, call ToolCall) (output string, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("tool handler %s panic: %v", call.Name, p)
		}
	}()
	return h.Handle(ctx, call)
}
