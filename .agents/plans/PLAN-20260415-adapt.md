# PLAN: api/adapt — LLM Domain Bridge

> **Design ref**: `.agents/plans/DESIGN-api-extraction.md`
> **Depends on**:
>   - `PLAN-20260415-messages.md` (must be complete first)
>   - `PLAN-20260415-completions.md` (must be complete first)
>   - `PLAN-20260415-responses.md` (must be complete first)
> **Estimated total**: ~50 min
>
> **Purpose**: `api/adapt` is the **only** package in `api/` that imports
> `github.com/codewandler/llm`. It bridges wire types ↔ `llm.Publisher`.
> All three protocol adapters live here: `messages_api.go`,
> `completions_api.go`, `responses_api.go`.

---

## Dependency graph

```
provider/anthropic
provider/minimax        ──► adapt.MessagesAdapter ──► api/messages
provider/openrouter(m)  /
                       /
provider/openai (cc)   ──► adapt.CompletionsAdapter ──► api/completions
provider/ollama        /
                      /
provider/openai (r)   ──► adapt.ResponsesAdapter ──► api/responses
provider/openrouter(r) /
```

`api/adapt` imports:
- `github.com/codewandler/llm` (Publisher, Request, events)
- `github.com/codewandler/llm/api/apicore`
- `github.com/codewandler/llm/api/messages`
- `github.com/codewandler/llm/api/completions`
- `github.com/codewandler/llm/api/responses`
- `github.com/codewandler/llm/tool`
- `github.com/codewandler/llm/usage`
- `github.com/codewandler/llm/msg`
- `github.com/codewandler/llm/sortmap`

`api/messages`, `api/completions`, `api/responses` import **none** of the above.

---

## Task 1: Create messages_api.go — convert + streamer + adapter

**Files created**: `api/adapt/messages_api.go`
**Estimated time**: 5 min

```go
// api/adapt/messages_api.go
package adapt

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/sortmap"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

// ── Streamer interface ────────────────────────────────────────────────────────

// MessagesStreamer is satisfied by *messages.Client and by test fakes.
type MessagesStreamer interface {
	Stream(ctx context.Context, req *messages.Request) (*apicore.StreamHandle, error)
}

// ── Adapter ───────────────────────────────────────────────────────────────────

// MessagesAdapter bridges the Anthropic Messages API to llm.Publisher.
type MessagesAdapter struct {
	sender   MessagesStreamer
	cfg      apicore.AdapterConfig
	convOpts []MessagesConvertOption
}

// NewMessagesAdapter creates a MessagesAdapter.
// base: identity options (provider name, upstream provider name).
// convOpts: conversion options (thinking mode, effort, cache, etc.).
func NewMessagesAdapter(
	sender MessagesStreamer,
	base []apicore.AdapterOption,
	convOpts ...MessagesConvertOption,
) *MessagesAdapter {
	return &MessagesAdapter{
		sender:   sender,
		cfg:      apicore.ApplyAdapterOptions(base...),
		convOpts: convOpts,
	}
}

// StreamTo converts req, streams it, and publishes events to pub.
// Blocks until the stream ends. Always calls pub.Close().
func (a *MessagesAdapter) StreamTo(ctx context.Context, req llm.Request, pub llm.Publisher) error {
	defer pub.Close()

	wireReq, err := MessagesRequestFromLLM(req, a.convOpts...)
	if err != nil {
		return fmt.Errorf("convert request: %w", err)
	}

	handle, err := a.sender.Stream(ctx, wireReq)
	if err != nil {
		return err
	}

	pub.Publish(&llm.RequestEvent{
		OriginalRequest: req,
		ProviderRequest: llm.ProviderRequestFromHTTP(handle.Request, nil),
		ResolvedApiType: llm.ApiTypeAnthropicMessages,
	})

	rateLimits := llm.ParseRateLimits(headersToMap(handle.Headers,
		messages.HeaderRateLimitReqLimit, messages.HeaderRateLimitReqRemaining,
		messages.HeaderRateLimitReqReset, messages.HeaderRateLimitTokLimit,
		messages.HeaderRateLimitTokRemaining, messages.HeaderRateLimitTokReset,
		messages.HeaderRateLimitInTokLimit, messages.HeaderRateLimitInTokRemaining,
		messages.HeaderRateLimitInTokReset, messages.HeaderRateLimitOutTokLimit,
		messages.HeaderRateLimitOutTokRemaining, messages.HeaderRateLimitOutTokReset,
		messages.HeaderRequestID,
	))

	var (
		requestID    string
		model        string
		inputTokens  int
		cacheCreate  int
		cacheRead    int
		outputTokens int
		stopReason   llm.StopReason
	)

	for result := range handle.Events {
		if result.Err != nil {
			pub.Error(result.Err)
			return result.Err
		}

		switch evt := result.Event.(type) {
		case *messages.MessageStartEvent:
			requestID   = evt.Message.ID
			model       = evt.Message.Model
			inputTokens = evt.Message.Usage.InputTokens
			cacheCreate = evt.Message.Usage.CacheCreationInputTokens
			cacheRead   = evt.Message.Usage.CacheReadInputTokens

			if model != "" && model != req.Model {
				pub.ModelResolved(a.cfg.ProviderName, req.Model, model)
			}
			var extra map[string]any
			if rateLimits != nil {
				extra = map[string]any{"rate_limits": rateLimits}
			}
			pub.Started(llm.StreamStartedEvent{
				RequestID: requestID,
				Model:     model,
				Provider:  a.cfg.Upstream(),
				Extra:     extra,
			})

		case *messages.ContentBlockDeltaEvent:
			idx := uint32(evt.Index)
			switch evt.Delta.Type {
			case "text_delta":
				d := llm.TextDelta(evt.Delta.Text)
				d.Index = &idx
				pub.Delta(d)
			case "thinking_delta":
				d := llm.ThinkingDelta(evt.Delta.Thinking)
				d.Index = &idx
				pub.Delta(d)
			}

		case *messages.TextCompleteEvent:
			pub.ContentBlock(llm.ContentPartEvent{
				Part:  msg.Text(evt.Text),
				Index: evt.Index,
			})

		case *messages.ThinkingCompleteEvent:
			pub.ContentBlock(llm.ContentPartEvent{
				Part:  msg.Thinking(evt.Thinking, evt.Signature),
				Index: evt.Index,
			})

		case *messages.ToolCompleteEvent:
			pub.ToolCall(tool.NewToolCall(evt.ID, evt.Name, evt.Args))

		case *messages.MessageDeltaEvent:
			outputTokens = evt.Usage.OutputTokens
			stopReason   = messagesStopReason(evt.Delta.StopReason)

		case *messages.MessageStopEvent:
			tokens := usage.TokenItems{
				{Kind: usage.KindInput,      Count: inputTokens},
				{Kind: usage.KindCacheRead,  Count: cacheRead},
				{Kind: usage.KindCacheWrite, Count: cacheCreate},
				{Kind: usage.KindOutput,     Count: outputTokens},
			}.NonZero()

			var extras map[string]any
			if rateLimits != nil {
				extras = map[string]any{"rate_limits": rateLimits}
			}
			rec := usage.Record{
				Dims: usage.Dims{
					Provider:  a.cfg.Provider(),
					Model:     model,
					RequestID: requestID,
				},
				Tokens:     tokens,
				Extras:     extras,
				RecordedAt: time.Now(),
			}
			if cost, ok := usage.Default().Calculate(a.cfg.Provider(), model, tokens); ok {
				rec.Cost = cost
			}
			pub.UsageRecord(rec)
			pub.Completed(llm.CompletedEvent{StopReason: stopReason})
		}
	}
	return nil
}

func messagesStopReason(s string) llm.StopReason {
	switch s {
	case "end_turn":
		return llm.StopReasonEndTurn
	case "tool_use":
		return llm.StopReasonToolUse
	case "max_tokens":
		return llm.StopReasonMaxTokens
	default:
		return llm.StopReasonEndTurn
	}
}

// ── Convert ───────────────────────────────────────────────────────────────────

type messagesConvertConfig struct {
	thinkingMode     messages.ThinkingMode
	thinkingBudget   int
	outputEffort     string
	userID           string
	cacheLastSystem  bool
	maxTokensDefault int
}

// MessagesConvertOption configures MessagesRequestFromLLM.
type MessagesConvertOption func(*messagesConvertConfig)

func MessagesWithThinking(mode messages.ThinkingMode, budget int) MessagesConvertOption {
	return func(c *messagesConvertConfig) { c.thinkingMode = mode; c.thinkingBudget = budget }
}

func MessagesWithOutputEffort(effort string) MessagesConvertOption {
	return func(c *messagesConvertConfig) { c.outputEffort = effort }
}

func MessagesWithUserID(id string) MessagesConvertOption {
	return func(c *messagesConvertConfig) { c.userID = id }
}

func MessagesWithCacheLastSystem() MessagesConvertOption {
	return func(c *messagesConvertConfig) { c.cacheLastSystem = true }
}

func MessagesWithDefaultMaxTokens(n int) MessagesConvertOption {
	return func(c *messagesConvertConfig) { c.maxTokensDefault = n }
}

// MessagesRequestFromLLM converts an llm.Request to a Messages API wire Request.
func MessagesRequestFromLLM(req llm.Request, opts ...MessagesConvertOption) (*messages.Request, error) {
	cfg := &messagesConvertConfig{maxTokensDefault: 4096}
	for _, opt := range opts {
		opt(cfg)
	}

	r := &messages.Request{
		Model:  req.Model,
		Stream: true,
	}

	r.MaxTokens = req.MaxTokens
	if r.MaxTokens == 0 {
		r.MaxTokens = cfg.maxTokensDefault
	}
	if req.TopK > 0 {
		r.TopK = req.TopK
	}
	if req.TopP > 0 {
		r.TopP = req.TopP
	}
	if req.OutputFormat == llm.OutputFormatJSON {
		r.OutputConfig = &messages.OutputConfig{Format: &messages.JSONOutputFormat{Type: "json"}}
	}
	if cfg.outputEffort != "" {
		if r.OutputConfig == nil {
			r.OutputConfig = &messages.OutputConfig{}
		}
		r.OutputConfig.Effort = cfg.outputEffort
	}

	switch cfg.thinkingMode {
	case messages.ThinkingEnabled:
		budget := cfg.thinkingBudget
		if budget == 0 {
			budget = 10000
		}
		r.Thinking = &messages.ThinkingConfig{Type: "enabled", BudgetTokens: budget}
	case messages.ThinkingAdaptive:
		r.Thinking = &messages.ThinkingConfig{Type: "adaptive"}
	}

	if cfg.userID != "" {
		r.Metadata = &messages.Metadata{UserID: cfg.userID}
	}

	for _, t := range req.Tools {
		r.Tools = append(r.Tools, messages.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: sortmap.NewSortedMap(t.Parameters),
		})
	}

	if len(req.Tools) > 0 {
		switch tc := req.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			r.ToolChoice = map[string]string{"type": "auto"}
		case llm.ToolChoiceRequired:
			r.ToolChoice = map[string]string{"type": "any"}
		case llm.ToolChoiceNone:
			r.ToolChoice = map[string]string{"type": "none"}
		case llm.ToolChoiceTool:
			r.ToolChoice = map[string]any{"type": "tool", "name": tc.Name}
		}
	}

	msgs, system, err := messagesConvertMsgs(req.Messages, cfg.cacheLastSystem)
	if err != nil {
		return nil, err
	}
	r.System = system
	r.Messages = msgs
	return r, nil
}

func messagesConvertMsgs(in []llm.Message, cacheLastSystem bool) ([]messages.Message, messages.SystemBlocks, error) {
	var system messages.SystemBlocks
	var turns []messages.Message

	for _, m := range in {
		if m.Role == msg.RoleSystem {
			system = append(system, &messages.TextBlock{Type: "text", Text: m.Text()})
		}
	}
	if cacheLastSystem && len(system) > 0 {
		system[len(system)-1].CacheControl = &messages.CacheControl{Type: "ephemeral"}
	}

	for _, m := range in {
		switch m.Role {
		case msg.RoleSystem:
			continue
		case msg.RoleUser:
			turns = append(turns, messages.Message{Role: "user", Content: m.Text()})
		case msg.RoleAssistant:
			var content []any
			if t := m.Text(); t != "" {
				content = append(content, messages.TextBlock{Type: "text", Text: t})
			}
			for _, tc := range m.ToolCalls() {
				argsJSON, err := json.Marshal(tc.Args)
				if err != nil {
					return nil, nil, fmt.Errorf("marshal tool call args: %w", err)
				}
				content = append(content, messages.ToolUseBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: argsJSON,
				})
			}
			turns = append(turns, messages.Message{Role: "assistant", Content: content})
		case msg.RoleTool:
			var content []any
			for _, tr := range m.ToolResults() {
				content = append(content, messages.ToolResultBlock{
					Type:      "tool_result",
					ToolUseID: tr.ToolCallID,
					Content:   tr.ToolOutput,
				})
			}
			turns = append(turns, messages.Message{Role: "user", Content: content})
		}
	}
	return turns, system, nil
}
```

**Verification**:
```bash
go build ./api/adapt/...
```

---

## Task 2: Create completions_api.go — convert + streamer + adapter

**Files created**: `api/adapt/completions_api.go`
**Estimated time**: 5 min

```go
// api/adapt/completions_api.go
package adapt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/completions"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/sortmap"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

// ── Streamer interface ────────────────────────────────────────────────────────

// CompletionsStreamer is satisfied by *completions.Client and test fakes.
type CompletionsStreamer interface {
	Stream(ctx context.Context, req *completions.Request) (*apicore.StreamHandle, error)
}

// ── Adapter ───────────────────────────────────────────────────────────────────

// CompletionsAdapter bridges the Chat Completions API to llm.Publisher.
type CompletionsAdapter struct {
	sender   CompletionsStreamer
	cfg      apicore.AdapterConfig
	convOpts []CompletionsConvertOption
}

// NewCompletionsAdapter creates a CompletionsAdapter.
func NewCompletionsAdapter(
	sender CompletionsStreamer,
	base []apicore.AdapterOption,
	convOpts ...CompletionsConvertOption,
) *CompletionsAdapter {
	return &CompletionsAdapter{
		sender:   sender,
		cfg:      apicore.ApplyAdapterOptions(base...),
		convOpts: convOpts,
	}
}

// StreamTo converts req, streams it, and publishes events to pub.
func (a *CompletionsAdapter) StreamTo(ctx context.Context, req llm.Request, pub llm.Publisher) error {
	defer pub.Close()

	wireReq, err := CompletionsRequestFromLLM(req, a.convOpts...)
	if err != nil {
		return fmt.Errorf("convert request: %w", err)
	}

	handle, err := a.sender.Stream(ctx, wireReq)
	if err != nil {
		return err
	}

	pub.Publish(&llm.RequestEvent{
		OriginalRequest: req,
		ProviderRequest: llm.ProviderRequestFromHTTP(handle.Request, nil),
		ResolvedApiType: llm.ApiTypeOpenAIChatCompletion,
	})

	// Tool-call accumulation (adapter owns this, not the parser, because one
	// chunk may carry fragments for multiple call slots across multiple choices).
	type toolSlot struct {
		id     string
		name   string
		argBuf strings.Builder
	}
	activeTools := make(map[int]*toolSlot)

	var (
		started    bool
		requestID  string
		model      string
		stopReason llm.StopReason
		lastUsage  *completions.Usage
	)

	for result := range handle.Events {
		if result.Err != nil {
			pub.Error(result.Err)
			return result.Err
		}

		chunk, ok := result.Event.(*completions.Chunk)
		if !ok {
			// result.Done=true with nil Event = [DONE] sentinel
			if result.Done {
				break
			}
			continue
		}

		if !started && (chunk.ID != "" || chunk.Model != "") {
			started    = true
			requestID  = chunk.ID
			model      = chunk.Model
			if model != "" && model != req.Model {
				pub.ModelResolved(a.cfg.ProviderName, req.Model, model)
			}
			pub.Started(llm.StreamStartedEvent{
				RequestID: requestID,
				Model:     model,
				Provider:  a.cfg.Upstream(),
			})
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				pub.Delta(llm.TextDelta(choice.Delta.Content))
			}

			for _, tc := range choice.Delta.ToolCalls {
				slot := activeTools[tc.Index]
				if slot == nil {
					slot = &toolSlot{}
					activeTools[tc.Index] = slot
				}
				if tc.ID != "" {
					slot.id = tc.ID
				}
				if tc.Function.Name != "" {
					slot.name = tc.Function.Name
				}
				slot.argBuf.WriteString(tc.Function.Arguments)
			}

			switch choice.FinishReason {
			case completions.FinishReasonToolCalls:
				for idx, slot := range activeTools {
					var args map[string]any
					_ = json.Unmarshal([]byte(slot.argBuf.String()), &args)
					pub.ToolCall(tool.NewToolCall(slot.id, slot.name, args))
					delete(activeTools, idx)
				}
				stopReason = llm.StopReasonToolUse
			case completions.FinishReasonStop:
				stopReason = llm.StopReasonEndTurn
			case completions.FinishReasonLength:
				stopReason = llm.StopReasonMaxTokens
			}
		}

		if chunk.Usage != nil {
			lastUsage = chunk.Usage
		}
	}

	if lastUsage != nil {
		inputTokens  := lastUsage.PromptTokens
		outputTokens := lastUsage.CompletionTokens
		var cacheRead, reasoning int
		if lastUsage.PromptTokensDetails != nil {
			cacheRead    = lastUsage.PromptTokensDetails.CachedTokens
			inputTokens -= cacheRead
		}
		if lastUsage.CompletionTokensDetails != nil {
			reasoning    = lastUsage.CompletionTokensDetails.ReasoningTokens
			outputTokens -= reasoning
		}
		tokens := usage.TokenItems{
			{Kind: usage.KindInput,     Count: inputTokens},
			{Kind: usage.KindCacheRead, Count: cacheRead},
			{Kind: usage.KindOutput,    Count: outputTokens},
			{Kind: usage.KindReasoning, Count: reasoning},
		}.NonZero()

		rec := usage.Record{
			Dims: usage.Dims{
				Provider:  a.cfg.Provider(),
				Model:     model,
				RequestID: requestID,
			},
			Tokens:     tokens,
			RecordedAt: time.Now(),
		}
		if cost, ok := usage.Default().Calculate(a.cfg.Provider(), model, tokens); ok {
			rec.Cost = cost
		}
		pub.UsageRecord(rec)
	}

	pub.Completed(llm.CompletedEvent{StopReason: stopReason})
	return nil
}

// ── Convert ───────────────────────────────────────────────────────────────────

type completionsConvertConfig struct {
	cacheRetention   string
	maxTokensDefault int
}

// CompletionsConvertOption configures CompletionsRequestFromLLM.
type CompletionsConvertOption func(*completionsConvertConfig)

func CompletionsWithCacheRetention(ttl string) CompletionsConvertOption {
	return func(c *completionsConvertConfig) { c.cacheRetention = ttl }
}

func CompletionsWithDefaultMaxTokens(n int) CompletionsConvertOption {
	return func(c *completionsConvertConfig) { c.maxTokensDefault = n }
}

// CompletionsRequestFromLLM converts an llm.Request to a Chat Completions wire Request.
func CompletionsRequestFromLLM(req llm.Request, opts ...CompletionsConvertOption) (*completions.Request, error) {
	cfg := &completionsConvertConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	r := &completions.Request{
		Model:         req.Model,
		Stream:        true,
		StreamOptions: &completions.StreamOptions{IncludeUsage: true},
	}

	if req.MaxTokens > 0 {
		r.MaxTokens = req.MaxTokens
	} else if cfg.maxTokensDefault > 0 {
		r.MaxTokens = cfg.maxTokensDefault
	}
	if req.Temperature > 0 {
		r.Temperature = req.Temperature
	}
	if req.TopP > 0 {
		r.TopP = req.TopP
	}
	if req.TopK > 0 {
		r.TopK = req.TopK
	}
	if req.OutputFormat == llm.OutputFormatJSON {
		r.ResponseFormat = &completions.ResponseFormat{Type: "json_object"}
	}
	if cfg.cacheRetention != "" {
		r.PromptCacheRetention = cfg.cacheRetention
	}
	if !req.Effort.IsEmpty() {
		r.ReasoningEffort = string(req.Effort)
	}

	for _, t := range req.Tools {
		r.Tools = append(r.Tools, completions.Tool{
			Type: "function",
			Function: completions.FuncPayload{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  sortmap.NewSortedMap(t.Parameters),
			},
		})
	}
	if len(req.Tools) > 0 {
		switch tc := req.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			r.ToolChoice = "auto"
		case llm.ToolChoiceRequired:
			r.ToolChoice = "required"
		case llm.ToolChoiceNone:
			r.ToolChoice = "none"
		case llm.ToolChoiceTool:
			r.ToolChoice = map[string]any{"type": "function", "function": map[string]string{"name": tc.Name}}
		}
	}

	for _, m := range req.Messages {
		switch m.Role {
		case msg.RoleSystem:
			r.Messages = append(r.Messages, completions.Message{Role: "system", Content: m.Text()})
		case msg.RoleUser:
			r.Messages = append(r.Messages, completions.Message{Role: "user", Content: m.Text()})
		case msg.RoleAssistant:
			mp := completions.Message{Role: "assistant", Content: m.Text()}
			for _, tc := range m.ToolCalls() {
				argsJSON, err := json.Marshal(tc.Args)
				if err != nil {
					return nil, fmt.Errorf("marshal tool args: %w", err)
				}
				mp.ToolCalls = append(mp.ToolCalls, completions.ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: completions.FuncCall{Name: tc.Name, Arguments: string(argsJSON)},
				})
			}
			r.Messages = append(r.Messages, mp)
		case msg.RoleTool:
			for _, tr := range m.ToolResults() {
				r.Messages = append(r.Messages, completions.Message{
					Role:       "tool",
					Content:    tr.ToolOutput,
					ToolCallID: tr.ToolCallID,
				})
			}
		}
	}

	return r, nil
}
```

**Verification**:
```bash
go build ./api/adapt/...
```

---

## Task 3: Create responses_api.go — convert + streamer + adapter

**Files created**: `api/adapt/responses_api.go`
**Estimated time**: 5 min

```go
// api/adapt/responses_api.go
package adapt

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/sortmap"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

// ── Streamer interface ────────────────────────────────────────────────────────

// ResponsesStreamer is satisfied by *responses.Client and test fakes.
type ResponsesStreamer interface {
	Stream(ctx context.Context, req *responses.Request) (*apicore.StreamHandle, error)
}

// ── Adapter ───────────────────────────────────────────────────────────────────

// ResponsesAdapter bridges the OpenAI Responses API to llm.Publisher.
type ResponsesAdapter struct {
	sender   ResponsesStreamer
	cfg      apicore.AdapterConfig
	convOpts []ResponsesConvertOption
}

// NewResponsesAdapter creates a ResponsesAdapter.
func NewResponsesAdapter(
	sender ResponsesStreamer,
	base []apicore.AdapterOption,
	convOpts ...ResponsesConvertOption,
) *ResponsesAdapter {
	return &ResponsesAdapter{
		sender:   sender,
		cfg:      apicore.ApplyAdapterOptions(base...),
		convOpts: convOpts,
	}
}

// StreamTo converts req, streams it, and publishes events to pub.
func (a *ResponsesAdapter) StreamTo(ctx context.Context, req llm.Request, pub llm.Publisher) error {
	defer pub.Close()

	wireReq, err := ResponsesRequestFromLLM(req, a.convOpts...)
	if err != nil {
		return fmt.Errorf("convert request: %w", err)
	}

	handle, err := a.sender.Stream(ctx, wireReq)
	if err != nil {
		return err
	}

	pub.Publish(&llm.RequestEvent{
		OriginalRequest: req,
		ProviderRequest: llm.ProviderRequestFromHTTP(handle.Request, nil),
		ResolvedApiType: llm.ApiTypeOpenAIResponses,
	})

	var (
		requestID  string
		model      string
		stopReason llm.StopReason
	)

	for result := range handle.Events {
		if result.Err != nil {
			pub.Error(result.Err)
			return result.Err
		}

		switch evt := result.Event.(type) {
		case *responses.ResponseCreatedEvent:
			requestID = evt.Response.ID
			model     = evt.Response.Model
			if model != "" && model != req.Model {
				pub.ModelResolved(a.cfg.ProviderName, req.Model, model)
			}
			pub.Started(llm.StreamStartedEvent{
				RequestID: requestID,
				Model:     model,
				Provider:  a.cfg.Upstream(),
			})

		case *responses.TextDeltaEvent:
			pub.Delta(llm.TextDelta(evt.Delta))

		case *responses.ToolCompleteEvent:
			pub.ToolCall(tool.NewToolCall(evt.ID, evt.Name, evt.Args))

		case *responses.ResponseCompletedEvent:
			stopReason = responsesStopReason(evt.Response.Status, evt.Response.IncompleteDetails)
			if u := evt.Response.Usage; u != nil {
				inputTokens  := u.InputTokens
				outputTokens := u.OutputTokens
				var cacheRead, reasoning int
				if u.InputTokensDetails != nil {
					cacheRead    = u.InputTokensDetails.CachedTokens
					inputTokens -= cacheRead
				}
				if u.OutputTokensDetails != nil {
					reasoning    = u.OutputTokensDetails.ReasoningTokens
					outputTokens -= reasoning
				}
				tokens := usage.TokenItems{
					{Kind: usage.KindInput,     Count: inputTokens},
					{Kind: usage.KindCacheRead, Count: cacheRead},
					{Kind: usage.KindOutput,    Count: outputTokens},
					{Kind: usage.KindReasoning, Count: reasoning},
				}.NonZero()
				rec := usage.Record{
					Dims: usage.Dims{
						Provider:  a.cfg.Provider(),
						Model:     model,
						RequestID: requestID,
					},
					Tokens:     tokens,
					RecordedAt: time.Now(),
				}
				if cost, ok := usage.Default().Calculate(a.cfg.Provider(), model, tokens); ok {
					rec.Cost = cost
				}
				pub.UsageRecord(rec)
			}
			pub.Completed(llm.CompletedEvent{StopReason: stopReason})
		}
	}
	return nil
}

func responsesStopReason(status string, details *struct {
	Reason string `json:"reason"`
}) llm.StopReason {
	if status == responses.StatusCompleted {
		return llm.StopReasonEndTurn
	}
	if status == responses.StatusIncomplete && details != nil &&
		details.Reason == responses.StopReasonMaxOutputTokens {
		return llm.StopReasonMaxTokens
	}
	return llm.StopReasonEndTurn
}

// ── Convert ───────────────────────────────────────────────────────────────────

type responsesConvertConfig struct {
	cacheRetention   string
	maxTokensDefault int
}

// ResponsesConvertOption configures ResponsesRequestFromLLM.
type ResponsesConvertOption func(*responsesConvertConfig)

func ResponsesWithCacheRetention(ttl string) ResponsesConvertOption {
	return func(c *responsesConvertConfig) { c.cacheRetention = ttl }
}

func ResponsesWithDefaultMaxOutputTokens(n int) ResponsesConvertOption {
	return func(c *responsesConvertConfig) { c.maxTokensDefault = n }
}

// ResponsesRequestFromLLM converts an llm.Request to a Responses API wire Request.
func ResponsesRequestFromLLM(req llm.Request, opts ...ResponsesConvertOption) (*responses.Request, error) {
	cfg := &responsesConvertConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	r := &responses.Request{
		Model:  req.Model,
		Stream: true,
	}

	if req.MaxTokens > 0 {
		r.MaxOutputTokens = req.MaxTokens
	} else if cfg.maxTokensDefault > 0 {
		r.MaxOutputTokens = cfg.maxTokensDefault
	}
	if req.Temperature > 0 {
		r.Temperature = req.Temperature
	}
	if req.TopP > 0 {
		r.TopP = req.TopP
	}
	if req.TopK > 0 {
		r.TopK = req.TopK
	}
	if req.OutputFormat == llm.OutputFormatJSON {
		r.ResponseFormat = &responses.ResponseFormat{Type: "json_object"}
	}
	if cfg.cacheRetention != "" {
		r.PromptCacheRetention = cfg.cacheRetention
	}
	if !req.Effort.IsEmpty() {
		r.Reasoning = &responses.Reasoning{Effort: string(req.Effort)}
	}

	for _, t := range req.Tools {
		r.Tools = append(r.Tools, responses.Tool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  sortmap.NewSortedMap(t.Parameters),
		})
	}
	if len(req.Tools) > 0 {
		switch tc := req.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			r.ToolChoice = "auto"
		case llm.ToolChoiceRequired:
			r.ToolChoice = "required"
		case llm.ToolChoiceNone:
			r.ToolChoice = "none"
		case llm.ToolChoiceTool:
			r.ToolChoice = map[string]any{"type": "function", "name": tc.Name}
		}
	}

	instructionsSet := false
	for _, m := range req.Messages {
		switch m.Role {
		case msg.RoleSystem:
			if !instructionsSet {
				r.Instructions = m.Text()
				instructionsSet = true
			} else {
				r.Input = append(r.Input, responses.Input{Role: "developer", Content: m.Text()})
			}
		case msg.RoleUser:
			r.Input = append(r.Input, responses.Input{Role: "user", Content: m.Text()})
		case msg.RoleAssistant:
			if t := m.Text(); t != "" {
				r.Input = append(r.Input, responses.Input{Role: "assistant", Content: t})
			}
			for _, tc := range m.ToolCalls() {
				argsJSON, err := json.Marshal(tc.Args)
				if err != nil {
					return nil, fmt.Errorf("marshal tool call args: %w", err)
				}
				r.Input = append(r.Input, responses.Input{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Name,
					Arguments: string(argsJSON),
				})
			}
		case msg.RoleTool:
			for _, tr := range m.ToolResults() {
				r.Input = append(r.Input, responses.Input{
					Type:   "function_call_output",
					CallID: tr.ToolCallID,
					Output: tr.ToolOutput,
				})
			}
		}
	}

	return r, nil
}
```

**Verification**:
```bash
go build ./api/adapt/...
```

---

## Task 4: Create helpers.go — shared utilities

**Files created**: `api/adapt/helpers.go`
**Estimated time**: 2 min

```go
// api/adapt/helpers.go
package adapt

import "net/http"

// headersToMap converts selected http.Header values into the
// map[string]string format expected by llm.ParseRateLimits.
// Only keys present in the header and listed in keys are included.
func headersToMap(h http.Header, keys ...string) map[string]string {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v := h.Get(k); v != "" {
			out[k] = v
		}
	}
	return out
}
```

**Verification**:
```bash
go build ./api/adapt/...
```

---

## Task 5: Write messages_api_test.go

**Files created**: `api/adapt/messages_api_test.go`
**Estimated time**: 5 min

```go
// api/adapt/messages_api_test.go
package adapt_test

import (
	"context"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/adapt"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/llmtest"
	"github.com/codewandler/llm/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeMessagesStreamer struct{ results []apicore.StreamResult }

func (f *fakeMessagesStreamer) Stream(_ context.Context, _ *messages.Request) (*apicore.StreamHandle, error) {
	return apicore.NewTestHandle(f.results...), nil
}

func TestMessagesAdapter_TextDeltas(t *testing.T) {
	s := &fakeMessagesStreamer{results: []apicore.StreamResult{
		{Event: &messages.MessageStartEvent{Message: messages.MessageStartPayload{ID: "r1", Model: "claude-3-5-haiku-20241022"}}},
		{Event: &messages.ContentBlockDeltaEvent{Index: 0, Delta: messages.Delta{Type: "text_delta", Text: "hello "}}},
		{Event: &messages.ContentBlockDeltaEvent{Index: 0, Delta: messages.Delta{Type: "text_delta", Text: "world"}}},
		{Event: &messages.TextCompleteEvent{Index: 0, Text: "hello world"}},
		{Event: &messages.MessageDeltaEvent{}},
		{Event: &messages.MessageStopEvent{}, Done: true},
	}}
	a := adapt.NewMessagesAdapter(s, []apicore.AdapterOption{apicore.WithProviderName("anthropic")})
	result := llmtest.Collect(context.Background(), func(pub llm.Publisher) {
		_ = a.StreamTo(context.Background(), llm.Request{Model: "claude-3-5-haiku-20241022"}, pub)
	})
	assert.Equal(t, "hello world", result.Text())
	assert.Equal(t, llm.StopReasonEndTurn, result.StopReason())
}

func TestMessagesAdapter_ToolCall(t *testing.T) {
	s := &fakeMessagesStreamer{results: []apicore.StreamResult{
		{Event: &messages.MessageStartEvent{Message: messages.MessageStartPayload{ID: "r1", Model: "m"}}},
		{Event: &messages.ToolCompleteEvent{ID: "tc1", Name: "search", Args: map[string]any{"q": "go"}}},
		{Event: &messages.MessageDeltaEvent{}},
		{Event: &messages.MessageStopEvent{}, Done: true},
	}}
	a := adapt.NewMessagesAdapter(s, nil)
	result := llmtest.Collect(context.Background(), func(pub llm.Publisher) {
		_ = a.StreamTo(context.Background(), llm.Request{Model: "m"}, pub)
	})
	require.Len(t, result.ToolCalls(), 1)
	assert.Equal(t, "search", result.ToolCalls()[0].Name)
	assert.Equal(t, llm.StopReasonToolUse, result.StopReason())
}

func TestMessagesAdapter_UsageRecord(t *testing.T) {
	s := &fakeMessagesStreamer{results: []apicore.StreamResult{
		{Event: &messages.MessageStartEvent{Message: messages.MessageStartPayload{
			ID: "r1", Model: "m",
			Usage: messages.MessageUsage{InputTokens: 50, CacheCreationInputTokens: 10},
		}}},
		{Event: &messages.MessageDeltaEvent{
			Delta: struct{ StopReason string `json:"stop_reason"` }{StopReason: "end_turn"},
			Usage: struct{ OutputTokens int `json:"output_tokens"` }{OutputTokens: 20},
		}},
		{Event: &messages.MessageStopEvent{}, Done: true},
	}}
	a := adapt.NewMessagesAdapter(s, []apicore.AdapterOption{apicore.WithProviderName("anthropic")})
	result := llmtest.Collect(context.Background(), func(pub llm.Publisher) {
		_ = a.StreamTo(context.Background(), llm.Request{Model: "m"}, pub)
	})
	require.NotNil(t, result.Usage())
	assert.Equal(t, 50, result.Usage().Tokens.Count(usage.KindInput))
	assert.Equal(t, 10, result.Usage().Tokens.Count(usage.KindCacheWrite))
	assert.Equal(t, 20, result.Usage().Tokens.Count(usage.KindOutput))
}

func TestMessagesRequestFromLLM_ThinkingEnabled(t *testing.T) {
	wire, err := adapt.MessagesRequestFromLLM(
		llm.Request{Model: "claude-3-7-sonnet-20250219"},
		adapt.MessagesWithThinking(messages.ThinkingEnabled, 8000),
	)
	require.NoError(t, err)
	require.NotNil(t, wire.Thinking)
	assert.Equal(t, "enabled", wire.Thinking.Type)
	assert.Equal(t, 8000, wire.Thinking.BudgetTokens)
}

func TestMessagesRequestFromLLM_CacheLastSystem(t *testing.T) {
	req := llm.Request{
		Model:    "m",
		Messages: llmtest.Messages(llmtest.System("sys1"), llmtest.System("sys2"), llmtest.User("hi")),
	}
	wire, err := adapt.MessagesRequestFromLLM(req, adapt.MessagesWithCacheLastSystem())
	require.NoError(t, err)
	require.Len(t, wire.System, 2)
	assert.Nil(t, wire.System[0].CacheControl)
	require.NotNil(t, wire.System[1].CacheControl)
	assert.Equal(t, "ephemeral", wire.System[1].CacheControl.Type)
}
```

**Verification**:
```bash
go test ./api/adapt/... -v -run TestMessages -count=1
```

---

## Task 6: Write completions_api_test.go

**Files created**: `api/adapt/completions_api_test.go`
**Estimated time**: 5 min

```go
// api/adapt/completions_api_test.go
package adapt_test

import (
	"context"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/adapt"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/completions"
	"github.com/codewandler/llm/llmtest"
	"github.com/codewandler/llm/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCompletionsStreamer struct{ results []apicore.StreamResult }

func (f *fakeCompletionsStreamer) Stream(_ context.Context, _ *completions.Request) (*apicore.StreamHandle, error) {
	return apicore.NewTestHandle(f.results...), nil
}

func cc(model, content, finish string) apicore.StreamResult {
	return apicore.StreamResult{Event: &completions.Chunk{
		ID:    "c1",
		Model: model,
		Choices: []completions.Choice{{
			Delta:        completions.Delta{Content: content},
			FinishReason: finish,
		}},
	}}
}

func TestCompletionsAdapter_TextDeltas(t *testing.T) {
	s := &fakeCompletionsStreamer{results: []apicore.StreamResult{
		cc("gpt-4o", "hello", ""),
		cc("gpt-4o", " world", "stop"),
		{Done: true},
	}}
	a := adapt.NewCompletionsAdapter(s, []apicore.AdapterOption{apicore.WithProviderName("openai")})
	result := llmtest.Collect(context.Background(), func(pub llm.Publisher) {
		_ = a.StreamTo(context.Background(), llm.Request{Model: "gpt-4o"}, pub)
	})
	assert.Equal(t, "hello world", result.Text())
	assert.Equal(t, llm.StopReasonEndTurn, result.StopReason())
}

func TestCompletionsAdapter_ToolCall(t *testing.T) {
	s := &fakeCompletionsStreamer{results: []apicore.StreamResult{
		{Event: &completions.Chunk{
			ID: "c1", Model: "gpt-4o",
			Choices: []completions.Choice{{Delta: completions.Delta{
				ToolCalls: []completions.ToolCallDelta{
					{Index: 0, ID: "tc1", Type: "function", Function: completions.FuncCallDelta{Name: "search"}},
				},
			}}},
		}},
		{Event: &completions.Chunk{
			Choices: []completions.Choice{{
				Delta: completions.Delta{
					ToolCalls: []completions.ToolCallDelta{
						{Index: 0, Function: completions.FuncCallDelta{Arguments: `{"q":"go"}`}},
					},
				},
				FinishReason: "tool_calls",
			}},
		}},
		{Done: true},
	}}
	a := adapt.NewCompletionsAdapter(s, nil)
	result := llmtest.Collect(context.Background(), func(pub llm.Publisher) {
		_ = a.StreamTo(context.Background(), llm.Request{Model: "gpt-4o"}, pub)
	})
	require.Len(t, result.ToolCalls(), 1)
	assert.Equal(t, "search", result.ToolCalls()[0].Name)
	assert.Equal(t, llm.StopReasonToolUse, result.StopReason())
}

func TestCompletionsAdapter_UsageRecord(t *testing.T) {
	s := &fakeCompletionsStreamer{results: []apicore.StreamResult{
		cc("gpt-4o", "hi", "stop"),
		{Event: &completions.Chunk{Usage: &completions.Usage{PromptTokens: 20, CompletionTokens: 10}}},
		{Done: true},
	}}
	a := adapt.NewCompletionsAdapter(s, []apicore.AdapterOption{apicore.WithProviderName("openai")})
	result := llmtest.Collect(context.Background(), func(pub llm.Publisher) {
		_ = a.StreamTo(context.Background(), llm.Request{Model: "gpt-4o"}, pub)
	})
	require.NotNil(t, result.Usage())
	assert.Equal(t, 20, result.Usage().Tokens.Count(usage.KindInput))
	assert.Equal(t, 10, result.Usage().Tokens.Count(usage.KindOutput))
}

func TestCompletionsRequestFromLLM_SystemUserMessages(t *testing.T) {
	req := llm.Request{
		Model:    "gpt-4o",
		Messages: llmtest.Messages(llmtest.System("Be helpful"), llmtest.User("Hello")),
	}
	wire, err := adapt.CompletionsRequestFromLLM(req)
	require.NoError(t, err)
	require.Len(t, wire.Messages, 2)
	assert.Equal(t, "system", wire.Messages[0].Role)
	assert.Equal(t, "user", wire.Messages[1].Role)
	assert.True(t, wire.StreamOptions.IncludeUsage)
}
```

**Verification**:
```bash
go test ./api/adapt/... -v -run TestCompletions -count=1
```

---

## Task 7: Write responses_api_test.go

**Files created**: `api/adapt/responses_api_test.go`
**Estimated time**: 5 min

```go
// api/adapt/responses_api_test.go
package adapt_test

import (
	"context"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/adapt"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/llmtest"
	"github.com/codewandler/llm/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeResponsesStreamer struct{ results []apicore.StreamResult }

func (f *fakeResponsesStreamer) Stream(_ context.Context, _ *responses.Request) (*apicore.StreamHandle, error) {
	return apicore.NewTestHandle(f.results...), nil
}

func createdEvt(id, model string) apicore.StreamResult {
	return apicore.StreamResult{Event: &responses.ResponseCreatedEvent{
		Response: struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		}{ID: id, Model: model},
	}}
}

func completedEvt(u *responses.ResponseUsage) apicore.StreamResult {
	return apicore.StreamResult{
		Event: &responses.ResponseCompletedEvent{Response: struct {
			ID                string                   `json:"id"`
			Model             string                   `json:"model"`
			Status            string                   `json:"status"`
			IncompleteDetails *struct{ Reason string `json:"reason"` } `json:"incomplete_details,omitempty"`
			Usage             *responses.ResponseUsage `json:"usage,omitempty"`
		}{Status: "completed", Usage: u}},
		Done: true,
	}
}

func TestResponsesAdapter_TextDeltas(t *testing.T) {
	s := &fakeResponsesStreamer{results: []apicore.StreamResult{
		createdEvt("r1", "gpt-4o"),
		{Event: &responses.TextDeltaEvent{Delta: "hello "}},
		{Event: &responses.TextDeltaEvent{Delta: "world"}},
		completedEvt(&responses.ResponseUsage{InputTokens: 5, OutputTokens: 3}),
	}}
	a := adapt.NewResponsesAdapter(s, []apicore.AdapterOption{apicore.WithProviderName("openai")})
	result := llmtest.Collect(context.Background(), func(pub llm.Publisher) {
		_ = a.StreamTo(context.Background(), llm.Request{Model: "gpt-4o"}, pub)
	})
	assert.Equal(t, "hello world", result.Text())
	assert.Equal(t, llm.StopReasonEndTurn, result.StopReason())
}

func TestResponsesAdapter_ToolCall(t *testing.T) {
	s := &fakeResponsesStreamer{results: []apicore.StreamResult{
		createdEvt("r1", "gpt-4o"),
		{Event: &responses.ToolCompleteEvent{ID: "call_1", Name: "search", Args: map[string]any{"q": "go"}}},
		completedEvt(nil),
	}}
	a := adapt.NewResponsesAdapter(s, nil)
	result := llmtest.Collect(context.Background(), func(pub llm.Publisher) {
		_ = a.StreamTo(context.Background(), llm.Request{Model: "gpt-4o"}, pub)
	})
	require.Len(t, result.ToolCalls(), 1)
	assert.Equal(t, "search", result.ToolCalls()[0].Name)
}

func TestResponsesAdapter_UsageTokenSplit(t *testing.T) {
	u := &responses.ResponseUsage{
		InputTokens:         100,
		OutputTokens:        50,
		InputTokensDetails:  &struct{ CachedTokens int `json:"cached_tokens"` }{CachedTokens: 30},
		OutputTokensDetails: &struct{ ReasoningTokens int `json:"reasoning_tokens"` }{ReasoningTokens: 10},
	}
	s := &fakeResponsesStreamer{results: []apicore.StreamResult{
		createdEvt("r1", "gpt-4o"),
		completedEvt(u),
	}}
	a := adapt.NewResponsesAdapter(s, []apicore.AdapterOption{apicore.WithProviderName("openai")})
	result := llmtest.Collect(context.Background(), func(pub llm.Publisher) {
		_ = a.StreamTo(context.Background(), llm.Request{Model: "gpt-4o"}, pub)
	})
	require.NotNil(t, result.Usage())
	// input = 100 - 30 = 70, cache_read = 30, output = 50 - 10 = 40, reasoning = 10
	assert.Equal(t, 70, result.Usage().Tokens.Count(usage.KindInput))
	assert.Equal(t, 30, result.Usage().Tokens.Count(usage.KindCacheRead))
	assert.Equal(t, 40, result.Usage().Tokens.Count(usage.KindOutput))
	assert.Equal(t, 10, result.Usage().Tokens.Count(usage.KindReasoning))
}

func TestResponsesRequestFromLLM_FirstSystemIsInstructions(t *testing.T) {
	req := llm.Request{
		Model:    "gpt-4o",
		Messages: llmtest.Messages(llmtest.System("Be helpful"), llmtest.User("Hi")),
	}
	wire, err := adapt.ResponsesRequestFromLLM(req)
	require.NoError(t, err)
	assert.Equal(t, "Be helpful", wire.Instructions)
	require.Len(t, wire.Input, 1)
	assert.Equal(t, "user", wire.Input[0].Role)
}

func TestResponsesRequestFromLLM_SecondSystemIsDeveloper(t *testing.T) {
	req := llm.Request{
		Model: "gpt-4o",
		Messages: llmtest.Messages(
			llmtest.System("s1"), llmtest.System("s2"), llmtest.User("hi"),
		),
	}
	wire, err := adapt.ResponsesRequestFromLLM(req)
	require.NoError(t, err)
	assert.Equal(t, "s1", wire.Instructions)
	require.Len(t, wire.Input, 2)
	assert.Equal(t, "developer", wire.Input[0].Role)
	assert.Equal(t, "s2", wire.Input[0].Content)
}
```

**Verification**:
```bash
go test ./api/adapt/... -v -run TestResponses -count=1
```

---

## Phase completion check

```bash
go build ./api/adapt/...
go test ./api/adapt/... -race -count=1
go vet ./api/adapt/...
```

### Import boundary enforcement

After this plan is complete, verify no protocol package imports `llm`:

```bash
# Must print nothing (zero matches)
grep -r "codewandler/llm\"" api/messages/ api/completions/ api/responses/ \
  --include="*.go" | grep -v "_test.go"
```

All `llm` domain logic lives exclusively in `api/adapt/`.
