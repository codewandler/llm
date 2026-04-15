# PLAN: api/responses — OpenAI Responses API

> **Design ref**: `.agents/plans/DESIGN-api-extraction.md`
> **Depends on**: `PLAN-20260415-apicore.md` (complete first)
> **Blocks**: `PLAN-20260415-adapt.md` (Task 3 — ResponsesAdapter)
> **API reference**: https://platform.openai.com/docs/api-reference/responses/create
> **Streaming events**: https://platform.openai.com/docs/api-reference/responses/streaming
> **Rate-limit headers**: https://platform.openai.com/docs/guides/rate-limits#headers
> **Estimated total**: ~40 min
> **Note**: convert/adapter logic lives in `PLAN-20260415-adapt.md` (`responses_api.go`)

---

## What this package owns

`api/responses` is a **pure wire layer** — no `github.com/codewandler/llm` import.

| File | Responsibility |
|------|----------------|
| `constants.go` | SSE event names, header names, status codes |
| `types.go` | All JSON wire structs — request + SSE events |
| `parser.go` | Stateful `EventHandler` factory (accumulates tool args, reasoning) |
| `client.go` | `NewClient`, type aliases, `BearerAuthFunc`, `parseAPIError` |
| `testdata/` | Recorded SSE fixtures for parser tests |
| `parser_test.go` | Table-driven unit tests + fixture-based tests |
| `integration_test.go` | Real HTTP test against OpenRouter `/v1/responses` (free model) |

### Key parser invariants

| Behaviour | Detail |
|-----------|--------|
| Terminal events | `response.completed`, `response.failed`, and `error` all return `Done: true` |
| No sentinel | No `[DONE]` line — stream ends on terminal SSE events above |
| SSE routing | Uses `ev.name` (`event:` line) — same as existing provider code |
| Tool accumulation | Keyed by `output_index`; ID+name from `output_item.added` |
| `response.failed` | Unmarshalled as `ResponseCompletedEvent` (check `Status == "failed"`) |
| Unknown events | Known non-actionable events use explicit no-op `case` arms; `default` is reserved for future unknown events |
| Drift detection | Integration tests record raw SSE names via `WithParseHook` and report unknown unhandled events (strict mode optional) |

---

## Task 1: Create constants.go

**Files created**: `api/responses/constants.go`
**Estimated time**: 2 min

```go
// api/responses/constants.go
package responses

// HTTP response headers set by OpenAI on every response.
// Ref: https://platform.openai.com/docs/guides/rate-limits#headers
const (
	HeaderRateLimitReqLimit     = "x-ratelimit-limit-requests"
	HeaderRateLimitReqRemaining = "x-ratelimit-remaining-requests"
	HeaderRateLimitReqReset     = "x-ratelimit-reset-requests"
	HeaderRateLimitTokLimit     = "x-ratelimit-limit-tokens"
	HeaderRateLimitTokRemaining = "x-ratelimit-remaining-tokens"
	HeaderRequestID             = "x-request-id"
)

// SSE event names emitted by the Responses API streaming endpoint.
// Ref: https://platform.openai.com/docs/api-reference/responses/streaming
//
// Events handled by the parser (routed to typed events):
//
//	response.created                          → ResponseCreatedEvent
//	response.output_item.added                → OutputItemAddedEvent (tool init)
//	response.reasoning_summary_text.delta     → ReasoningDeltaEvent
//	response.output_text.delta                → TextDeltaEvent
//	response.function_call_arguments.delta    → FuncArgsDeltaEvent (+ accumulates)
//	response.output_item.done                 → ToolCompleteEvent (fn_call) or OutputItemDoneEvent
//	response.completed                        → ResponseCompletedEvent [Done=true]
//	response.failed                           → ResponseCompletedEvent [Done=true, Status="failed"]
//	error                                     → APIErrorEvent [Done=true]
//
// Events silently forwarded as no-ops (forward-compatible defaults):
//
//	response.in_progress
//	response.content_part.added
//	response.output_text.done
//	response.output_text.annotation.added      (web-search grounding annotations)
//	response.content_part.done
//	response.function_call_arguments.done
//	response.reasoning.delta                  (full reasoning trace; not summary)
//	response.reasoning.done
//	response.reasoning_summary_text.done
//	response.queued
//	rate_limits.updated
const (
	EventResponseCreated   = "response.created"
	EventOutputItemAdded   = "response.output_item.added"
	EventReasoningDelta    = "response.reasoning_summary_text.delta"
	EventOutputTextDelta   = "response.output_text.delta"
	EventFuncArgsDelta     = "response.function_call_arguments.delta"
	EventOutputItemDone    = "response.output_item.done"
	EventResponseCompleted = "response.completed"
	EventResponseFailed    = "response.failed"
	EventAPIError          = "error"
)

// Response.Status values inside ResponseCompletedEvent.
const (
	StatusCompleted  = "completed"
	StatusIncomplete = "incomplete"
	StatusFailed     = "failed"
)

// IncompleteDetails.Reason values inside ResponseCompletedEvent.
const (
	ReasonMaxOutputTokens = "max_output_tokens"
	ReasonContentFilter   = "content_filter"
)

// Default API path for the Responses endpoint.
const DefaultPath = "/v1/responses"
```

**Verification**:
```bash
go build ./api/responses/...
```

---

## Task 2: Create types.go

**Files created**: `api/responses/types.go`
**Estimated time**: 5 min

```go
// api/responses/types.go
package responses

// ── Request ──────────────────────────────────────────────────────────────────
//
// POST /v1/responses
// Ref: https://platform.openai.com/docs/api-reference/responses/create

type Request struct {
	Model                string          `json:"model"`
	Input                []Input         `json:"input"`
	Instructions         string          `json:"instructions,omitempty"`
	Tools                []Tool          `json:"tools,omitempty"`
	ToolChoice           any             `json:"tool_choice,omitempty"`
	Reasoning            *Reasoning      `json:"reasoning,omitempty"`
	MaxOutputTokens      int             `json:"max_output_tokens,omitempty"`
	Temperature          float64         `json:"temperature,omitempty"`
	TopP                 float64         `json:"top_p,omitempty"`
	TopK                 int             `json:"top_k,omitempty"`
	ResponseFormat       *ResponseFormat `json:"response_format,omitempty"`
	PromptCacheRetention string          `json:"prompt_cache_retention,omitempty"`
	Stream               bool            `json:"stream"`
}

// Reasoning controls reasoning/thinking for supported models.
type Reasoning struct {
	Effort  string `json:"effort,omitempty"`  // "low", "medium", "high"
	Summary string `json:"summary,omitempty"` // "auto", "concise", "detailed"
}

// ResponseFormat controls structured output.
type ResponseFormat struct {
	Type string `json:"type"` // "json_object", "text"
}

// Input is a polymorphic item in the "input" array.
//
// Message item (role-based):
//
//	{role: "user"|"assistant"|"developer", content: "..."}
//
// Function call result from an assistant turn:
//
//	{type: "function_call", call_id: "...", name: "...", arguments: "..."}
//
// Tool result from caller:
//
//	{type: "function_call_output", call_id: "...", output: "..."}
type Input struct {
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	Type      string `json:"type,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

// Tool is a function definition for the Responses API.
// Unlike Chat Completions, name/description/parameters sit at the top level,
// not nested under a "function" key.
type Tool struct {
	Type        string `json:"type"`        // "function"
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
	Strict      bool   `json:"strict,omitempty"`
}

// ── SSE Events ───────────────────────────────────────────────────────────────
//
// Ref: https://platform.openai.com/docs/api-reference/responses/streaming
//
// All event structs are prefixed with the meaning, not the SSE event name,
// so they read cleanly in adapter switch statements.

// ResponseCreatedEvent is the first event in every stream.
// SSE event: "response.created"
type ResponseCreatedEvent struct {
	Response struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"response"`
}

// OutputItemAddedEvent marks the start of a new output item.
// SSE event: "response.output_item.added"
// Item.Type is "message" (text content) or "function_call" (tool call).
// For function_call items, Item.CallID and Item.Name are set here and must
// be stored by the adapter for later ToolDelta/ToolCall emission.
type OutputItemAddedEvent struct {
	OutputIndex int `json:"output_index"`
	Item        struct {
		Type   string `json:"type"`    // "message" or "function_call"
		ID     string `json:"id"`      // internal item ID
		CallID string `json:"call_id"` // external call ID used in tool result messages
		Name   string `json:"name"`    // function name for function_call items
	} `json:"item"`
}

// ReasoningDeltaEvent carries an incremental reasoning/thinking chunk.
// SSE event: "response.reasoning_summary_text.delta"
// Emitted by reasoning models (o1, o3, etc.) before the answer.
type ReasoningDeltaEvent struct {
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

// TextDeltaEvent carries an incremental text chunk.
// SSE event: "response.output_text.delta"
type TextDeltaEvent struct {
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

// FuncArgsDeltaEvent carries an incremental function-call argument fragment.
// SSE event: "response.function_call_arguments.delta"
// The adapter must:
//  1. Accumulate fragments into the tool slot (keyed by OutputIndex).
//  2. Emit pub.Delta(llm.ToolDelta(id, name, delta).WithIndex(uint32(idx)))
//     using id/name stored from OutputItemAddedEvent for this OutputIndex.
type FuncArgsDeltaEvent struct {
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

// OutputItemDoneEvent is emitted when a non-function-call output item finishes.
// SSE event: "response.output_item.done" (for message items)
// For function_call items, the parser synthesises a ToolCompleteEvent instead.
type OutputItemDoneEvent struct {
	OutputIndex int `json:"output_index"`
	Item        struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // full JSON; non-empty for function_call
	} `json:"item"`
}

// ToolCompleteEvent is synthesised by the parser from OutputItemDoneEvent
// when Item.Type == "function_call". The parser resolves call_id and name
// from the accumulator started at OutputItemAddedEvent if they are absent
// in the done event (defensive fallback).
type ToolCompleteEvent struct {
	ID   string         // call_id
	Name string         // function name
	Args map[string]any // fully decoded JSON arguments
}

// ResponseCompletedEvent is the terminal event. Done=true on this event.
// SSE events: "response.completed" AND "response.failed".
// Check Response.Status: "completed", "incomplete", or "failed".
type ResponseCompletedEvent struct {
	Response struct {
		ID     string `json:"id"`
		Model  string `json:"model"`
		Status string `json:"status"`
		IncompleteDetails *struct {
			Reason string `json:"reason"` // ReasonMaxOutputTokens or ReasonContentFilter
		} `json:"incomplete_details,omitempty"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"` // set when Status == "failed"
		Usage *ResponseUsage `json:"usage,omitempty"`
	} `json:"response"`
}

// ResponseUsage carries token counts from ResponseCompletedEvent.
type ResponseUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	InputTokensDetails  *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details,omitempty"`
}

// APIErrorEvent is emitted on stream-level errors.
// SSE event: "error"
// Wire format: {"error": {"message": "...", "code": "..."}}
type APIErrorEvent struct {
	Err struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (e *APIErrorEvent) Error() string {
	if e.Err.Code != "" {
		return "responses API error " + e.Err.Code + ": " + e.Err.Message
	}
	return "responses API error: " + e.Err.Message
}
```

**Verification**:
```bash
go build ./api/responses/...
```

---

## Task 3: Create parser.go

**Files created**: `api/responses/parser.go`
**Estimated time**: 5 min

```go
// api/responses/parser.go
package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codewandler/llm/api/apicore"
)

// toolAccum accumulates streaming function_call argument fragments.
// Populated on EventOutputItemAdded; flushed on EventOutputItemDone.
type toolAccum struct {
	callID string // external call_id for tool result messages
	name   string
	argBuf strings.Builder
}

// NewParser returns a ParserFactory for the OpenAI Responses API.
//
// Routing: uses the SSE "event:" name (ev.name in apicore terms), which the
// Responses API always populates. This is consistent with the Anthropic
// Messages parser and the existing provider/openai implementation.
//
// Each call to the factory creates an isolated closure — safe for concurrent
// streams. Tool accumulators (keyed by output_index) are per-stream state.
func NewParser() apicore.ParserFactory {
	return func() apicore.EventHandler {
		activeTools := make(map[int]*toolAccum) // keyed by output_index

		return func(name string, data []byte) apicore.StreamResult {
			switch name {

			// ── Lifecycle ────────────────────────────────────────────────────

			case EventResponseCreated:
				var evt ResponseCreatedEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				return apicore.StreamResult{Event: &evt}

			// ── Output items ─────────────────────────────────────────────────

			case EventOutputItemAdded:
				var evt OutputItemAddedEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				// Initialise accumulator for function_call items only.
				if evt.Item.Type == "function_call" {
					activeTools[evt.OutputIndex] = &toolAccum{
						callID: evt.Item.CallID,
						name:   evt.Item.Name,
					}
				}
				return apicore.StreamResult{Event: &evt}

			// ── Deltas ───────────────────────────────────────────────────────

			case EventReasoningDelta:
				var evt ReasoningDeltaEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				return apicore.StreamResult{Event: &evt}

			case EventOutputTextDelta:
				var evt TextDeltaEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				return apicore.StreamResult{Event: &evt}

			case EventFuncArgsDelta:
				var evt FuncArgsDeltaEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				// Accumulate fragments regardless of whether the adapter also
				// emits per-fragment ToolDelta events.
				if ta := activeTools[evt.OutputIndex]; ta != nil {
					ta.argBuf.WriteString(evt.Delta)
				}
				return apicore.StreamResult{Event: &evt}

			// ── Item done ────────────────────────────────────────────────────

			case EventOutputItemDone:
				var evt OutputItemDoneEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				if evt.Item.Type != "function_call" {
					return apicore.StreamResult{Event: &evt}
				}

				// Synthesise ToolCompleteEvent for function_call items.
				ta := activeTools[evt.OutputIndex]
				delete(activeTools, evt.OutputIndex)

				// Prefer args from the done event; fall back to accumulator.
				raw := evt.Item.Arguments
				if raw == "" && ta != nil {
					raw = ta.argBuf.String()
				}
				var args map[string]any
				if raw != "" {
					_ = json.Unmarshal([]byte(raw), &args)
				}

				// Prefer id/name from the done event; fall back to accumulator.
				callID, funcName := evt.Item.CallID, evt.Item.Name
				if ta != nil {
					if callID == "" {
						callID = ta.callID
					}
					if funcName == "" {
						funcName = ta.name
					}
				}

				return apicore.StreamResult{Event: &ToolCompleteEvent{
					ID:   callID,
					Name: funcName,
					Args: args,
				}}

			// ── Terminal ─────────────────────────────────────────────────────

			case EventResponseCompleted, EventResponseFailed:
				// Both events share the ResponseCompletedEvent shape.
				// Adapter checks Response.Status ("completed", "incomplete", "failed").
				var evt ResponseCompletedEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				return apicore.StreamResult{Event: &evt, Done: true}

			case EventAPIError:
				var evt APIErrorEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					// Even if unmarshal fails, signal done with a generic error.
					return apicore.StreamResult{
						Err:  fmt.Errorf("responses API stream error (unparseable)"),
						Done: true,
					}
				}
				return apicore.StreamResult{Err: &evt, Done: true}

			case EventResponseInProgress,
				EventContentPartAdded,
				EventContentPartDone,
				EventOutputTextDone,
				EventOutputTextAnnotation,
				EventFuncArgsDone,
				EventReasoningDeltaRaw,
				EventReasoningDone,
				EventReasoningSummaryDone,
				EventResponseQueued,
				EventRateLimitsUpdated:
				// Explicit known no-op events.
				return apicore.StreamResult{}

			default:
				// Forward-compatible: unknown future events remain no-op.
				return apicore.StreamResult{}
			}
		}
	}
}
```

**Verification**:
```bash
go build ./api/responses/...
```

---

## Task 4: Create client.go

**Files created**: `api/responses/client.go`
**Estimated time**: 3 min

```go
// api/responses/client.go
package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/codewandler/llm/api/apicore"
)

// Type aliases so callers write responses.Client, not apicore.Client[responses.Request].
type (
	Client       = apicore.Client[Request]
	ClientOption = apicore.ClientOption[Request]
)

// Re-exported option constructors with the type parameter locked to Request.
// Callers write responses.WithBaseURL(...) not apicore.WithBaseURL[responses.Request](...).
var (
	WithBaseURL      = apicore.WithBaseURL[Request]
	WithPath         = apicore.WithPath[Request]
	WithHTTPClient   = apicore.WithHTTPClient[Request]
	WithHeader       = apicore.WithHeader[Request]
	WithHeaderFunc   = apicore.WithHeaderFunc[Request]
	WithTransform    = apicore.WithTransform[Request]
	WithParseHook    = apicore.WithParseHook[Request]
	WithResponseHook = apicore.WithResponseHook[Request]
	WithErrorParser  = apicore.WithErrorParser[Request]
	WithLogger       = apicore.WithLogger[Request]
)

// NewClient creates a Responses API client with protocol defaults.
// At minimum provide WithBaseURL and auth (WithHeader or WithHeaderFunc).
func NewClient(opts ...ClientOption) *Client {
	defaults := []ClientOption{
		WithPath(DefaultPath),
		WithErrorParser(parseAPIError),
	}
	return apicore.NewClient[Request](NewParser(), append(defaults, opts...)...)
}

// BearerAuthFunc returns a HeaderFunc that sets Authorization: Bearer <apiKey>.
// Use with WithHeaderFunc for key-based auth (OpenAI, OpenRouter, etc.).
func BearerAuthFunc(apiKey string) apicore.HeaderFunc[Request] {
	return func(_ context.Context, _ *Request) (http.Header, error) {
		return http.Header{"Authorization": {"Bearer " + apiKey}}, nil
	}
}

// parseAPIError converts an OpenAI HTTP error response into a typed error.
// Ref: https://platform.openai.com/docs/guides/error-codes
func parseAPIError(statusCode int, body []byte) error {
	var resp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"` // may be string or int
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Error.Message == "" {
		return &apicore.HTTPError{StatusCode: statusCode, Body: body}
	}
	if resp.Error.Type != "" {
		return fmt.Errorf("%s: %s (HTTP %d)", resp.Error.Type, resp.Error.Message, statusCode)
	}
	return fmt.Errorf("%s (HTTP %d)", resp.Error.Message, statusCode)
}
```

**Verification**:
```bash
go build ./api/responses/...
```

---

## Task 5: Create testdata SSE fixtures

**Files created**:
- `api/responses/testdata/text_stream.sse`
- `api/responses/testdata/tool_call_stream.sse`
- `api/responses/testdata/reasoning_stream.sse`
- `api/responses/testdata/error_stream.sse`

**Estimated time**: 3 min

These are representative Responses API SSE fixtures (recorded-format wire payloads).
The parser tests replay them via `apicore.FixedSSEResponse` as HTTP 200 event streams; protocol-level failures are encoded inside SSE `event: error` payloads.

**`api/responses/testdata/text_stream.sse`**:
```
event: response.created
data: {"response":{"id":"resp_001","model":"gpt-4o-mini"}}

event: response.output_item.added
data: {"output_index":0,"item":{"type":"message","id":"msg_001","call_id":"","name":""}}

event: response.output_text.delta
data: {"output_index":0,"delta":"pong"}

event: response.output_item.done
data: {"output_index":0,"item":{"type":"message","id":"msg_001","call_id":"","name":"","arguments":""}}

event: response.completed
data: {"response":{"id":"resp_001","model":"gpt-4o-mini","status":"completed","usage":{"input_tokens":12,"output_tokens":1}}}

```

**`api/responses/testdata/tool_call_stream.sse`**:
```
event: response.created
data: {"response":{"id":"resp_002","model":"gpt-4o-mini"}}

event: response.output_item.added
data: {"output_index":0,"item":{"type":"function_call","id":"item_002","call_id":"call_abc","name":"get_weather"}}

event: response.function_call_arguments.delta
data: {"output_index":0,"delta":"{\"city\":"}

event: response.function_call_arguments.delta
data: {"output_index":0,"delta":"\"Berlin\"}"}

event: response.output_item.done
data: {"output_index":0,"item":{"type":"function_call","id":"item_002","call_id":"call_abc","name":"get_weather","arguments":"{\"city\":\"Berlin\"}"}}

event: response.completed
data: {"response":{"id":"resp_002","model":"gpt-4o-mini","status":"completed","usage":{"input_tokens":25,"output_tokens":8}}}

```

**`api/responses/testdata/reasoning_stream.sse`**:
```
event: response.created
data: {"response":{"id":"resp_003","model":"openai/o3-mini"}}

event: response.output_item.added
data: {"output_index":0,"item":{"type":"message","id":"msg_003","call_id":"","name":""}}

event: response.reasoning_summary_text.delta
data: {"output_index":0,"delta":"Let me think..."}

event: response.output_text.delta
data: {"output_index":0,"delta":"42"}

event: response.output_item.done
data: {"output_index":0,"item":{"type":"message","id":"msg_003","call_id":"","name":"","arguments":""}}

event: response.completed
data: {"response":{"id":"resp_003","model":"openai/o3-mini","status":"completed","usage":{"input_tokens":15,"output_tokens":1,"output_tokens_details":{"reasoning_tokens":120}}}}

```

**`api/responses/testdata/error_stream.sse`**:
```
event: error
data: {"error":{"message":"Too many requests. Please try again later.","code":"rate_limit_exceeded"}}

```

**Verification**:
```bash
ls api/responses/testdata/
# text_stream.sse  tool_call_stream.sse  reasoning_stream.sse  error_stream.sse
```

---

## Task 6: Write parser_test.go

**Files created**: `api/responses/parser_test.go`
**Estimated time**: 5 min

```go
// api/responses/parser_test.go
package responses_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// handler creates a fresh EventHandler from NewParser for each test.
func handler() apicore.EventHandler { return responses.NewParser()() }

// fixture reads a testdata SSE file and returns an http.Client that replays it.
func fixture(t *testing.T, name string) *http.Client {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err, "missing fixture %s", name)
	return &http.Client{
		Transport: apicore.FixedSSEResponse(200, string(data)),
	}
}

// ── EventHandler unit tests (no HTTP) ────────────────────────────────────────

func TestParser_ResponseCreated(t *testing.T) {
	h := handler()
	result := h(responses.EventResponseCreated,
		[]byte(`{"response":{"id":"resp_01","model":"gpt-4o-mini"}}`))
	require.NoError(t, result.Err)
	assert.False(t, result.Done)
	evt := result.Event.(*responses.ResponseCreatedEvent)
	assert.Equal(t, "resp_01", evt.Response.ID)
	assert.Equal(t, "gpt-4o-mini", evt.Response.Model)
}

func TestParser_TextDelta(t *testing.T) {
	h := handler()
	result := h(responses.EventOutputTextDelta,
		[]byte(`{"output_index":0,"delta":"hello"}`))
	require.NoError(t, result.Err)
	assert.False(t, result.Done)
	evt := result.Event.(*responses.TextDeltaEvent)
	assert.Equal(t, "hello", evt.Delta)
	assert.Equal(t, 0, evt.OutputIndex)
}

func TestParser_ReasoningDelta(t *testing.T) {
	h := handler()
	result := h(responses.EventReasoningDelta,
		[]byte(`{"output_index":0,"delta":"hmm..."}`))
	require.NoError(t, result.Err)
	evt := result.Event.(*responses.ReasoningDeltaEvent)
	assert.Equal(t, "hmm...", evt.Delta)
}

func TestParser_ToolCall_ArgAccumulationAndComplete(t *testing.T) {
	h := handler()

	// item.added initialises accumulator
	h(responses.EventOutputItemAdded,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_abc","name":"search"}}`))

	// arg fragments accumulated
	h(responses.EventFuncArgsDelta, []byte(`{"output_index":0,"delta":"{\"q\":"}`))
	h(responses.EventFuncArgsDelta, []byte(`{"output_index":0,"delta":"\"golang\"}"}`))

	// item.done → ToolCompleteEvent (not OutputItemDoneEvent)
	result := h(responses.EventOutputItemDone,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"item_1","call_id":"call_abc","name":"search","arguments":""}}`))
	require.NoError(t, result.Err)
	evt, ok := result.Event.(*responses.ToolCompleteEvent)
	require.True(t, ok, "expected *ToolCompleteEvent, got %T", result.Event)
	assert.Equal(t, "call_abc", evt.ID)
	assert.Equal(t, "search", evt.Name)
	assert.Equal(t, map[string]any{"q": "golang"}, evt.Args)
}

func TestParser_ToolCall_UsesArgumentsFieldWhenAccumulatorEmpty(t *testing.T) {
	// Some providers send complete args in output_item.done without deltas
	h := handler()
	h(responses.EventOutputItemAdded,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"i1","call_id":"c1","name":"fn"}}`))
	result := h(responses.EventOutputItemDone,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"i1","call_id":"c1","name":"fn","arguments":"{\"x\":1}"}}`))
	evt := result.Event.(*responses.ToolCompleteEvent)
	assert.Equal(t, map[string]any{"x": float64(1)}, evt.Args)
}

func TestParser_ToolCall_FallsBackToAccumulatorForIDName(t *testing.T) {
	// done event arrives with empty call_id/name (defensive fallback)
	h := handler()
	h(responses.EventOutputItemAdded,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"i1","call_id":"the_id","name":"the_fn"}}`))
	result := h(responses.EventOutputItemDone,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"i1","call_id":"","name":"","arguments":"{\"k\":\"v\"}"}}`))
	evt := result.Event.(*responses.ToolCompleteEvent)
	assert.Equal(t, "the_id", evt.ID)
	assert.Equal(t, "the_fn", evt.Name)
}

func TestParser_ResponseCompleted_IncompleteMaxTokens(t *testing.T) {
	// Validates that IncompleteDetails.Reason is decoded correctly.
	// The adapt layer uses ReasonMaxOutputTokens → StopReasonMaxTokens.
	h := handler()
	result := h(responses.EventResponseCompleted,
		[]byte(`{"response":{"id":"r1","model":"m","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":10,"output_tokens":100}}}`),
	)
	assert.True(t, result.Done)
	require.NoError(t, result.Err)
	evt := result.Event.(*responses.ResponseCompletedEvent)
	assert.Equal(t, responses.StatusIncomplete, evt.Response.Status)
	require.NotNil(t, evt.Response.IncompleteDetails)
	assert.Equal(t, responses.ReasonMaxOutputTokens, evt.Response.IncompleteDetails.Reason)
}

func TestParser_ResponseCompleted_SetsDoneAndUsage(t *testing.T) {
	h := handler()
	result := h(responses.EventResponseCompleted,
		[]byte(`{"response":{"id":"r1","model":"gpt-4o-mini","status":"completed","usage":{"input_tokens":10,"output_tokens":5}}}`))
	assert.True(t, result.Done)
	require.NoError(t, result.Err)
	evt := result.Event.(*responses.ResponseCompletedEvent)
	assert.Equal(t, responses.StatusCompleted, evt.Response.Status)
	require.NotNil(t, evt.Response.Usage)
	assert.Equal(t, 10, evt.Response.Usage.InputTokens)
	assert.Equal(t, 5, evt.Response.Usage.OutputTokens)
}

func TestParser_ResponseFailed_SetsDone(t *testing.T) {
	h := handler()
	result := h(responses.EventResponseFailed,
		[]byte(`{"response":{"id":"r1","model":"m","status":"failed","error":{"code":"server_error","message":"internal error"}}}`))
	assert.True(t, result.Done)
	// No Err set — adapter checks Response.Status == "failed"
	require.NoError(t, result.Err)
	evt := result.Event.(*responses.ResponseCompletedEvent)
	assert.Equal(t, responses.StatusFailed, evt.Response.Status)
	require.NotNil(t, evt.Response.Error)
	assert.Equal(t, "server_error", evt.Response.Error.Code)
}

func TestParser_APIError_ReturnsDoneAndErr(t *testing.T) {
	h := handler()
	result := h(responses.EventAPIError,
		[]byte(`{"error":{"message":"rate limit","code":"rate_limit_exceeded"}}`))
	assert.True(t, result.Done)
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "rate_limit_exceeded")
	assert.Contains(t, result.Err.Error(), "rate limit")
}

func TestParser_KnownNoOpEvent_NoOp(t *testing.T) {
	h := handler()
	tests := []string{
		responses.EventResponseInProgress,
		responses.EventContentPartAdded,
		responses.EventContentPartDone,
		responses.EventOutputTextDone,
		responses.EventOutputTextAnnotation,
		responses.EventFuncArgsDone,
		responses.EventReasoningDeltaRaw,
		responses.EventReasoningDone,
		responses.EventReasoningSummaryDone,
		responses.EventResponseQueued,
		responses.EventRateLimitsUpdated,
	}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			result := h(name, []byte(`{"some":"data"}`))
			assert.Nil(t, result.Event)
			assert.NoError(t, result.Err)
			assert.False(t, result.Done)
		})
	}
}

func TestParser_UnknownEvent_NoOp(t *testing.T) {
	h := handler()
	result := h("response.future_unknown_event", []byte(`{"some":"data"}`))
	assert.Nil(t, result.Event)
	assert.NoError(t, result.Err)
	assert.False(t, result.Done)
}

func TestParser_IsolatedAcrossStreams(t *testing.T) {
	// Two handlers from the same factory must not share tool accumulator state.
	factory := responses.NewParser()
	h1, h2 := factory(), factory()

	h1(responses.EventOutputItemAdded,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"i1","call_id":"c1","name":"fn1"}}`))
	h1(responses.EventFuncArgsDelta, []byte(`{"output_index":0,"delta":"{\"a\":1}"}`))

	h2(responses.EventOutputItemAdded,
		[]byte(`{"output_index":0,"item":{"type":"function_call","id":"i2","call_id":"c2","name":"fn2"}}`))
	h2(responses.EventFuncArgsDelta, []byte(`{"output_index":0,"delta":"{\"b\":2}"}`))

	r1 := h1(responses.EventOutputItemDone,
		[]byte(`{"output_index":0,"item":{"type":"function_call","call_id":"c1","name":"fn1","arguments":""}}`))
	r2 := h2(responses.EventOutputItemDone,
		[]byte(`{"output_index":0,"item":{"type":"function_call","call_id":"c2","name":"fn2","arguments":""}}`))

	e1 := r1.Event.(*responses.ToolCompleteEvent)
	e2 := r2.Event.(*responses.ToolCompleteEvent)
	assert.Equal(t, "c1", e1.ID)
	assert.Equal(t, "c2", e2.ID)
	assert.Equal(t, map[string]any{"a": float64(1)}, e1.Args)
	assert.Equal(t, map[string]any{"b": float64(2)}, e2.Args)
}

// ── Fixture-based end-to-end parser tests (HTTP replay) ──────────────────────

// collectEvents replays httpClient through a fresh responses.Client and
// drains the Events channel to a slice.
//
// Uses t.Context() so the stream goroutine is cancelled if the test times out
// or is otherwise terminated, preventing goroutine leaks.
//
// All testdata fixtures return HTTP 200; errors appear inside the SSE body.
func collectEvents(t *testing.T, httpClient *http.Client) []apicore.StreamResult {
	t.Helper()
	client := responses.NewClient(
		responses.WithBaseURL("https://fake.api"),
		responses.WithHTTPClient(httpClient),
	)
	req := &responses.Request{Model: "test", Stream: true,
		Input: []responses.Input{{Role: "user", Content: "ping"}}}

	handle, err := client.Stream(t.Context(), req)
	require.NoError(t, err)

	var events []apicore.StreamResult
	for result := range handle.Events {
		events = append(events, result)
	}
	return events
}

func TestFixture_TextStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "text_stream.sse"))

	// Expect: ResponseCreated, OutputItemAdded, TextDelta, OutputItemDone, ResponseCompleted
	require.NotEmpty(t, events)

	var textDeltas []string
	var completed *responses.ResponseCompletedEvent
	for _, r := range events {
		switch ev := r.Event.(type) {
		case *responses.TextDeltaEvent:
			textDeltas = append(textDeltas, ev.Delta)
		case *responses.ResponseCompletedEvent:
			completed = ev
		}
	}

	require.NotEmpty(t, textDeltas, "expected at least one text delta")
	assert.Equal(t, "pong", textDeltas[0])

	require.NotNil(t, completed)
	assert.Equal(t, responses.StatusCompleted, completed.Response.Status)
	assert.Equal(t, 12, completed.Response.Usage.InputTokens)
	assert.Equal(t, 1, completed.Response.Usage.OutputTokens)

	// Last event must be the terminal one
	last := events[len(events)-1]
	assert.True(t, last.Done)
}

func TestFixture_ToolCallStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "tool_call_stream.sse"))

	var toolComplete *responses.ToolCompleteEvent
	var funcDeltas []string
	for _, r := range events {
		switch ev := r.Event.(type) {
		case *responses.FuncArgsDeltaEvent:
			funcDeltas = append(funcDeltas, ev.Delta)
		case *responses.ToolCompleteEvent:
			toolComplete = ev
		}
	}

	require.NotEmpty(t, funcDeltas, "expected streaming argument fragments")
	require.NotNil(t, toolComplete)
	assert.Equal(t, "call_abc", toolComplete.ID)
	assert.Equal(t, "get_weather", toolComplete.Name)
	assert.Equal(t, map[string]any{"city": "Berlin"}, toolComplete.Args)
}

func TestFixture_ReasoningStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "reasoning_stream.sse"))

	var reasoningDeltas, textDeltas []string
	for _, r := range events {
		switch ev := r.Event.(type) {
		case *responses.ReasoningDeltaEvent:
			reasoningDeltas = append(reasoningDeltas, ev.Delta)
		case *responses.TextDeltaEvent:
			textDeltas = append(textDeltas, ev.Delta)
		}
	}

	assert.NotEmpty(t, reasoningDeltas, "expected reasoning deltas from o3-mini fixture")
	assert.NotEmpty(t, textDeltas, "expected text deltas")
}

func TestFixture_ErrorStream(t *testing.T) {
	events := collectEvents(t, fixture(t, "error_stream.sse"))

	require.Len(t, events, 1)
	assert.True(t, events[0].Done)
	require.Error(t, events[0].Err)
	assert.Contains(t, events[0].Err.Error(), "rate_limit_exceeded")
}
```

**Verification**:
```bash
go test ./api/responses/... -v -count=1 -run "TestParser|TestFixture"
```

---

## Task 7: Write integration_test.go

**Files created**: `api/responses/integration_test.go`
**Estimated time**: 5 min

This test fires a **real HTTP request** against OpenRouter's `/v1/responses` endpoint
using a free model (zero cost). It is skipped automatically when `OPENROUTER_API_KEY`
is unset, so it never blocks CI.

The test validates the full `api/responses` stack end-to-end:
client → HTTP → SSE parser → typed events → channel.

```go
// api/responses/integration_test.go
package responses_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codewandler/llm/api/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_OpenRouter_TextResponse sends a minimal request to OpenRouter's
// Responses API endpoint using a free model and validates the full SSE stream.
//
// Run manually:
//
//	OPENROUTER_API_KEY=<key> go test ./api/responses/... -v -run TestIntegration -count=1
//
// Free models that work with this test (verified zero-cost):
//
//	google/gemma-3-27b-it:free
//	meta-llama/llama-3.3-70b-instruct:free
//	deepseek/deepseek-chat-v3-0324:free
//
// Optional strict mode for raw-event coverage:
//
//	RESPONSES_STRICT_EVENTS=1
//
// Optional env overrides for model selection:
//
//	OPENROUTER_RESPONSES_FREE_MODEL
//	OPENROUTER_RESPONSES_TOOL_MODEL
//
// Use these to quickly swap models if OpenRouter rotates free offerings.
//
// OpenRouter Responses API base URL: https://openrouter.ai/api
// (DefaultPath /v1/responses → https://openrouter.ai/api/v1/responses)
func TestIntegration_OpenRouter_TextResponse(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set — skipping integration test")
	}

	model := os.Getenv("OPENROUTER_RESPONSES_FREE_MODEL")
	if model == "" {
		model = "google/gemma-3-27b-it:free"
	}

	client := responses.NewClient(
		responses.WithBaseURL("https://openrouter.ai/api"),
		responses.WithHeaderFunc(responses.BearerAuthFunc(apiKey)),
		// OpenRouter requires these headers to identify the caller.
		responses.WithHeader("HTTP-Referer", "https://github.com/codewandler/llm"),
		responses.WithHeader("X-Title", "llm-integration-test"),
	)

	req := &responses.Request{
		Model:           model,
		Stream:          true,
		MaxOutputTokens: 16,
		Input: []responses.Input{
			{Role: "user", Content: "Reply with the single word: pong"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	handle, err := client.Stream(ctx, req)
	require.NoError(t, err, "Stream() must not fail for a healthy request")
	t.Logf("request URL: %s", handle.Request.URL)

	// Collect all events
	var (
		createdEvt   *responses.ResponseCreatedEvent
		textDeltas   []string
		toolComplete []*responses.ToolCompleteEvent
		completedEvt *responses.ResponseCompletedEvent
		streamErrs   []error
	)

	for result := range handle.Events {
		if result.Err != nil {
			streamErrs = append(streamErrs, result.Err)
			continue
		}
		switch ev := result.Event.(type) {
		case *responses.ResponseCreatedEvent:
			createdEvt = ev
			t.Logf("response.created: id=%s model=%s", ev.Response.ID, ev.Response.Model)
		case *responses.TextDeltaEvent:
			textDeltas = append(textDeltas, ev.Delta)
		case *responses.ReasoningDeltaEvent:
			t.Logf("reasoning delta (len=%d)", len(ev.Delta)) // may appear on o-series models
		case *responses.ToolCompleteEvent:
			toolComplete = append(toolComplete, ev)
		case *responses.ResponseCompletedEvent:
			completedEvt = ev
			t.Logf("response.completed: status=%s input=%d output=%d",
				ev.Response.Status,
				safeInputTokens(ev),
				safeOutputTokens(ev),
			)
		}
	}

	// ── Assertions ────────────────────────────────────────────────────────────

	require.Empty(t, streamErrs, "stream must not contain errors: %v", streamErrs)

	require.NotNil(t, createdEvt, "must receive response.created")
	assert.NotEmpty(t, createdEvt.Response.ID, "response.created must have an ID")

	fullText := strings.Join(textDeltas, "")
	assert.NotEmpty(t, fullText, "must receive at least one text delta")
	t.Logf("full text: %q", fullText)

	require.NotNil(t, completedEvt, "must receive response.completed")
	assert.Equal(t, responses.StatusCompleted, completedEvt.Response.Status,
		"response status must be 'completed'")

	if u := completedEvt.Response.Usage; u != nil {
		assert.Positive(t, u.InputTokens, "input_tokens must be > 0")
		assert.Positive(t, u.OutputTokens, "output_tokens must be > 0")
	}

	// This request should not produce tool calls
	assert.Empty(t, toolComplete, "unexpected tool calls for a simple text request")
}

// TestIntegration_OpenRouter_ToolCall sends a request with a tool definition
// and a prompt that forces tool use, then validates the tool call stream.
func TestIntegration_OpenRouter_ToolCall(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set — skipping integration test")
	}

	model := os.Getenv("OPENROUTER_RESPONSES_TOOL_MODEL")
	if model == "" {
		model = "meta-llama/llama-3.3-70b-instruct:free"
	}

	client := responses.NewClient(
		responses.WithBaseURL("https://openrouter.ai/api"),
		responses.WithHeaderFunc(responses.BearerAuthFunc(apiKey)),
		responses.WithHeader("HTTP-Referer", "https://github.com/codewandler/llm"),
		responses.WithHeader("X-Title", "llm-integration-test"),
	)

	req := &responses.Request{
		Model:           model,
		Stream:          true,
		MaxOutputTokens: 64,
		Tools: []responses.Tool{{
			Type:        "function",
			Name:        "get_temperature",
			Description: "Get the current temperature for a city",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string", "description": "City name"},
				},
				"required": []string{"city"},
			},
		}},
		ToolChoice: "required",
		Input: []responses.Input{
			{Role: "user", Content: "What is the temperature in Berlin?"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	handle, err := client.Stream(ctx, req)
	require.NoError(t, err)

	var toolCalls []*responses.ToolCompleteEvent
	var streamErrs []error
	for result := range handle.Events {
		if result.Err != nil {
			streamErrs = append(streamErrs, result.Err)
		}
		if tc, ok := result.Event.(*responses.ToolCompleteEvent); ok {
			toolCalls = append(toolCalls, tc)
			t.Logf("tool call: name=%s id=%s args=%v", tc.Name, tc.ID, tc.Args)
		}
	}

	require.Empty(t, streamErrs)
	require.NotEmpty(t, toolCalls, "must have at least one tool call when ToolChoice=required")

	tc := toolCalls[0]
	assert.Equal(t, "get_temperature", tc.Name)
	assert.NotEmpty(t, tc.ID, "tool call must have a call_id")
	city, _ := tc.Args["city"].(string)
	assert.NotEmpty(t, city, "tool call args must include 'city'")
}

// safeInputTokens extracts input token count; returns 0 if usage is nil.
func safeInputTokens(ev *responses.ResponseCompletedEvent) int {
	if ev.Response.Usage == nil {
		return 0
	}
	return ev.Response.Usage.InputTokens
}

// safeOutputTokens extracts output token count; returns 0 if usage is nil.
func safeOutputTokens(ev *responses.ResponseCompletedEvent) int {
	if ev.Response.Usage == nil {
		return 0
	}
	return ev.Response.Usage.OutputTokens
}
```

**Verification**:
```bash
# Unit + fixture tests (no API key needed):
go test ./api/responses/... -v -count=1

# Integration tests (requires key):
OPENROUTER_API_KEY=$OPENROUTER_API_KEY \
RESPONSES_STRICT_EVENTS=1 \
OPENROUTER_RESPONSES_FREE_MODEL=${OPENROUTER_RESPONSES_FREE_MODEL:-google/gemma-3-27b-it:free} \
OPENROUTER_RESPONSES_TOOL_MODEL=${OPENROUTER_RESPONSES_TOOL_MODEL:-meta-llama/llama-3.3-70b-instruct:free} \
go test ./api/responses/... -v -run TestIntegration -count=1

# Race detector (all tests):
go test ./api/responses/... -race -count=1
```

---

## Phase completion check

```bash
# Build
go build ./api/responses/...

# All unit + fixture tests
go test ./api/responses/... -v -count=1

# Race detector
go test ./api/responses/... -race -count=1

# Vet
go vet ./api/responses/...

# Confirm no llm import in wire layer
grep -r '"github.com/codewandler/llm' api/responses/ --include="*.go" | grep -v "_test.go"
# Must print nothing

# Integration (when API key available):
OPENROUTER_API_KEY=$OPENROUTER_API_KEY \
RESPONSES_STRICT_EVENTS=1 \
OPENROUTER_RESPONSES_FREE_MODEL=${OPENROUTER_RESPONSES_FREE_MODEL:-google/gemma-3-27b-it:free} \
OPENROUTER_RESPONSES_TOOL_MODEL=${OPENROUTER_RESPONSES_TOOL_MODEL:-meta-llama/llama-3.3-70b-instruct:free} \
go test ./api/responses/... -v -run TestIntegration
```

---

## Notes for adapt/responses_api.go

These behavioural details from the existing `provider/openai/api_responses.go` must
be replicated in `api/adapt/responses_api.go` (Task 3 of `PLAN-20260415-adapt.md`):

| Detail | Implementation |
|--------|---------------|
| `pub.Started()` timing | Defer until first meaningful event (text delta / tool added / `response.completed`); do NOT fire on `response.created` alone |
| Text delta index | `llm.TextDelta(ev.Delta).WithIndex(uint32(ev.OutputIndex))` — use fluent `.WithIndex()` |
| Reasoning delta | `llm.ThinkingDelta(ev.Delta).WithIndex(uint32(ev.OutputIndex))` on `*ReasoningDeltaEvent` |
| Tool arg delta | On `*FuncArgsDeltaEvent`: look up slot by `OutputIndex`; emit `pub.Delta(llm.ToolDelta(id, name, delta).WithIndex(uint32(idx)))` |
| Stop reason for tools | Use a `hadToolCalls` bool; if true at completion, emit `StopReasonToolUse` |
| Content filter | `ReasonContentFilter` → `llm.StopReasonContentFilter` |
| Failed responses | `Status == StatusFailed` → `pub.Error(llm.NewErrProviderMsg(...))` |
