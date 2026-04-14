package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

type Result interface {
	Response
	Next() msg.Messages
	UsageRecords() []usage.Record   // provider-reported, in arrival order
	TokenEstimates() []usage.Record // pre-request estimates, in order
	Drift() *usage.Drift            // nil if no estimate received
}

type result struct {
	textBuffer        strings.Builder
	thinkingBuffer    strings.Builder
	thinkingSignature string
	stopReason        StopReason
	usageRecords      []usage.Record
	estimateRecs      []usage.Record
	toolCalls         []tool.Call
	toolResults       []tool.Result
	errors            []error
}

func (r *result) MarshalJSON() ([]byte, error) {
	type resultJSON struct {
		StopReason   StopReason     `json:"stop_reason"`
		Messages     []msg.Message  `json:"messages"`
		ToolCalls    []tool.Call    `json:"tool_calls"`
		ToolResults  []tool.Result  `json:"tool_results"`
		UsageRecords []usage.Record `json:"usage_records,omitempty"`
		Error        string         `json:"error,omitempty"`
	}
	var err = r.Error()
	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}
	return json.Marshal(resultJSON{
		Messages:     r.Next(),
		ToolCalls:    r.ToolCalls(),
		UsageRecords: r.UsageRecords(),
		StopReason:   r.StopReason(),
		ToolResults:  r.ToolResults(),
		Error:        errMsg,
	})
}

func newResult() *result {
	return &result{
		toolCalls:   make([]tool.Call, 0),
		toolResults: make([]tool.Result, 0),
		errors:      make([]error, 0),
	}
}

// Message returns the current assistant message.
func (r *result) Message() msg.Message {
	m := msg.Assistant()
	if r.thinkingBuffer.Len() > 0 {
		m.Part(msg.Thinking(r.thinkingBuffer.String(), r.thinkingSignature))
	}
	if r.textBuffer.Len() > 0 {
		m.Part(msg.Text(r.textBuffer.String()))
	}
	for _, tc := range r.toolCalls {
		m.Part(msg.ToolCall{
			ID:   tc.ToolCallID(),
			Name: tc.ToolName(),
			Args: tc.ToolArgs(),
		})
	}

	return m.Build()
}

func (r *result) ToolMessage() (msg.Message, bool) {
	if len(r.toolResults) == 0 {
		return msg.Message{}, false
	}
	results := make(msg.ToolResults, 0)
	for _, tr := range r.toolResults {
		data, _ := json.Marshal(tr.ToolOutput())
		results = append(results, msg.ToolResult{
			ToolCallID: tr.ToolCallID(),
			IsError:    tr.IsError(),
			ToolOutput: string(data),
		})
	}
	return msg.Tool().Results(results).Build(), true
}

func (r *result) ToolResults() []tool.Result { return r.toolResults }
func (r *result) Text() string               { return r.textBuffer.String() }
func (r *result) Thought() string            { return r.thinkingBuffer.String() }
func (r *result) StopReason() StopReason     { return r.stopReason }
func (r *result) Error() error               { return errors.Join(r.errors...) }
func (r *result) ToolCalls() []tool.Call     { return r.toolCalls }

func (r *result) UsageRecords() []usage.Record {
	return r.usageRecords
}

func (r *result) TokenEstimates() []usage.Record {
	return r.estimateRecs
}

func (r *result) Drift() *usage.Drift {
	if len(r.estimateRecs) == 0 || len(r.usageRecords) == 0 {
		return nil
	}
	// Find the first unlabeled (primary) estimate — the same logic as
	// Tracker.Drift so the two always agree.
	var primary *usage.Record
	for i := range r.estimateRecs {
		if r.estimateRecs[i].Dims.Labels == nil {
			primary = &r.estimateRecs[i]
			break
		}
	}
	if primary == nil {
		return nil
	}
	return usage.ComputeDrift(primary, &r.usageRecords[0])
}

func (r *result) addError(err error) {
	r.errors = append(r.errors, err)
}

func (r *result) applyDelta(ev *DeltaEvent) {
	switch ev.Kind {
	case DeltaKindText:
		r.textBuffer.WriteString(ev.Text)
	case DeltaKindThinking:
		r.thinkingBuffer.WriteString(ev.Thinking)
	case DeltaKindTool:
		// TODO: write partial tool data
	}
}

func (r *result) applyUsage(rec usage.Record) {
	r.usageRecords = append(r.usageRecords, rec)
}

func (r *result) applyEstimate(rec usage.Record) {
	r.estimateRecs = append(r.estimateRecs, rec)
}

func (r *result) applyToolCall(tc tool.Call) {
	r.toolCalls = append(r.toolCalls, tc)
}

var _ Result = (*result)(nil)

func (r *result) Next() msg.Messages {
	next := make(msg.Messages, 0, 2)
	next = next.Append(r.Message())
	if toolMsg, ok := r.ToolMessage(); ok {
		next = next.Append(toolMsg)
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

func NewEventProcessor(ctx context.Context, ch <-chan Envelope) *StreamProcessor {
	return &StreamProcessor{
		ctx:           ctx,
		ch:            ch,
		dispatcher:    tool.DispatchTypeSync,
		toolHandlers:  tool.NewHandlers(),
		eventHandlers: make([]EventHandler, 0),
		result:        newResult(),
		done:          make(chan struct{}, 1),
	}
}

func ProcessEvents(ctx context.Context, ch <-chan Envelope) Result {
	return NewEventProcessor(ctx, ch).Result()
}

func (r *StreamProcessor) OnEvent(fn EventHandler) *StreamProcessor {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.eventHandlers = append(r.eventHandlers, fn)
	return r
}

// OnStart registers a callback that is called when the StreamEventStarted event
// arrives, carrying provider metadata (request ToolCallID, model, time-to-first-token).
func (r *StreamProcessor) OnStart(fn TypedEventHandler[*StreamStartedEvent]) *StreamProcessor {
	return r.OnEvent(fn)
}

func (r *StreamProcessor) OnDelta(fn TypedEventHandler[*DeltaEvent]) *StreamProcessor {
	return r.OnEvent(fn)
}

func (r *StreamProcessor) onDeltaKind(k DeltaKind, fn func(d DeltaEvent)) *StreamProcessor {
	return r.OnDelta(func(d *DeltaEvent) {
		if d.Kind != k {
			return
		}
		fn(*d)
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
	return r.onDeltaKind(DeltaKindThinking, func(d DeltaEvent) {
		fn(d.Thinking)
	})
}

// OnToolDelta registers a callback that is called for each partial tool-call
// argument fragment (DeltaKindTool deltas).
func (r *StreamProcessor) OnToolDelta(fn func(d ToolDeltaPart)) *StreamProcessor {
	return r.onDeltaKind(DeltaKindTool, func(d DeltaEvent) {
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
func (r *StreamProcessor) Result() Result {
	r.once.Do(func() { go r.doProcess() })
	<-r.done
	return r.result
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
				if res.stopReason == StopReasonToolUse {
					r.dispatchToolCalls()
				}
				if res.stopReason == "" {
					res.stopReason = StopReasonEndTurn
				}
				return
			}

			r.processEvent(ev)
		}
	}
}

func (r *StreamProcessor) dispatchToolCalls() {
	if len(r.result.toolCalls) == 0 {
		return
	}

	var results []tool.Result
	var err error

	if len(r.toolHandlers) > 0 {
		var dispatcher tool.Dispatcher
		if r.dispatcher == tool.DispatchTypeAsync {
			dispatcher = &tool.AsyncDispatcher{Handlers: r.toolHandlers}
		} else {
			dispatcher = tool.NewSyncDispatcher(r.toolHandlers)
		}

		results, err = dispatcher.Dispatch(r.ctx, r.result.toolCalls...)
		if err != nil {
			r.result.addError(err)
		}
	}

	if len(results) == 0 && len(r.result.toolCalls) > 0 {
		results = make([]tool.Result, len(r.result.toolCalls))
		for i, tc := range r.result.toolCalls {
			results[i] = tool.NewResult(tc.ToolCallID(), fmt.Sprintf("no handler for tool: %s", tc.ToolName()), true)
		}
	}

	for i, tc := range r.result.toolCalls {
		if i < len(results) && results[i] == nil {
			results[i] = tool.NewResult(tc.ToolCallID(), fmt.Sprintf("no handler for tool: %s", tc.ToolName()), true)
		}
	}

	r.result.toolResults = append(r.result.toolResults, results...)
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
		r.result.applyUsage(actual.Record)
	case *TokenEstimateEvent:
		r.result.applyEstimate(actual.Estimate)
	case *ErrorEvent:
		r.result.addError(actual.Error)
		r.result.stopReason = StopReasonError
	case *ContentPartEvent:
		switch actual.Part.Type {
		case msg.PartTypeThinking:
			r.result.thinkingSignature = actual.Part.Thinking.Signature
		}

	}

	r.dispatchEvent(e)
}
