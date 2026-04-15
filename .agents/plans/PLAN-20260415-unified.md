# PLAN: api/unified — Canonical Schema + Bridge Layer

> **Design ref**: `.agents/plans/DESIGN-api-unified.md`
> **Supersedes**: `PLAN-20260415-adapt.md` (retired)
> **Depends on**:
>   - `PLAN-20260415-apicore.md`
>   - `PLAN-20260415-messages.md`
>   - `PLAN-20260415-completions.md`
>   - `PLAN-20260415-responses.md`
> **Estimated total**: ~85 min

---

## Goal

Create `api/unified` as the single internal layer that:

1. Defines canonical request/event schema
2. Converts canonical requests to each wire protocol
3. Converts wire events to canonical events
4. Bridges canonical events into `llm.Publisher`

This replaces the old standalone adapt-plan responsibilities.

---

## Task 1: Scaffold package + core request types

**Files created**:
- `api/unified/types_request.go`
- `api/unified/types_extensions.go`
- `api/unified/validate.go`
- `api/unified/doc.go`

**Estimated time**: 6 min

**Code to write (skeleton)**:

```go
// api/unified/types_request.go
package unified

type Request struct {
	Model       string
	Messages    []Message
	MaxTokens   int
	Temperature float64
	TopP        float64
	TopK        int
	OutputFormat OutputFormat
	Tools      []Tool
	ToolChoice ToolChoice
	Effort   Effort
	Thinking ThinkingMode
	CacheHint *CacheHint
	UserID string
	Extras RequestExtras
}

type Message struct {
	Role Role
	Parts []Part
	CacheHint *CacheHint
}

type Part struct {
	Type PartType
	Text string
	Thinking *ThinkingPart
	ToolCall *ToolCall
	ToolResult *ToolResult
	Native any // protocol-specific block payload
}
```

```go
// api/unified/types_extensions.go
package unified

type RequestExtras struct {
	Messages    *MessagesExtras
	Completions *CompletionsExtras
	Responses   *ResponsesExtras
	Provider    map[string]any
}

type MessagesExtras struct {
	AnthropicBeta []string
}

type CompletionsExtras struct {
	PromptCacheRetention string
}

type ResponsesExtras struct {
	PromptCacheRetention string
}
```

```go
// api/unified/validate.go
package unified

func (r Request) Validate() error {
	if r.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(r.Messages) == 0 {
		return fmt.Errorf("messages are required")
	}
	return nil
}
```

**Verification**:
```bash
go build ./api/unified/...
```

---

## Task 2: Add llm request bridge

**Files created**:
- `api/unified/llm_bridge.go`
- `api/unified/llm_bridge_test.go`

**Estimated time**: 7 min

**Code to write (key funcs)**:

```go
package unified

import (
	"encoding/json"
	"fmt"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

func RequestFromLLM(req llm.Request) (Request, error) {
	// map model/options
	// map msg.Message parts -> unified parts
	// map tool defs and tool choice
	return Request{...}, nil
}

func RequestToLLM(req Request) (llm.Request, error) {
	// best-effort reverse helper (for debugging/tooling)
	return llm.Request{...}, nil
}
```

Test matrix:
- system/user/assistant/tool messages
- text/thinking/tool-call/tool-result parts
- tool choice variants
- output format mapping
- invalid request validation path

**Verification**:
```bash
go test ./api/unified/... -v -run TestRequestFromLLM -count=1
```

---

## Task 3: Canonical -> Messages converter

**Files created**:
- `api/unified/messages_api.go`
- `api/unified/messages_api_test.go`

**Estimated time**: 8 min

**Code to write (key funcs)**:

```go
func RequestToMessages(r Request, opts ...MessagesOption) (*messages.Request, error)
func RequestFromMessages(r messages.Request) (Request, error)
```

Mapping requirements:
- preserve message role/part ordering
- map tool definitions + tool choice
- map thinking + effort + output format
- preserve messages extras (`AnthropicBeta`) in `RequestExtras.Messages`

Test cases:
- roundtrip semantic parity for key fields
- system caching hint position behavior
- tool_use / tool_result conversion

**Verification**:
```bash
go test ./api/unified/... -v -run TestMessages -count=1
```

---

## Task 4: Canonical -> Completions converter

**Files created**:
- `api/unified/completions_api.go`
- `api/unified/completions_api_test.go`

**Estimated time**: 7 min

Key funcs:

```go
func RequestToCompletions(r Request, opts ...CompletionsOption) (*completions.Request, error)
func RequestFromCompletions(r completions.Request) (Request, error)
```

Include:
- stream_options.include_usage=true default
- function tool conversion
- prompt cache retention from extras

**Verification**:
```bash
go test ./api/unified/... -v -run TestCompletions -count=1
```

---

## Task 5: Canonical -> Responses converter

**Files created**:
- `api/unified/responses_api.go`
- `api/unified/responses_api_test.go`

**Estimated time**: 8 min

Key funcs:

```go
func RequestToResponses(r Request, opts ...ResponsesOption) (*responses.Request, error)
func RequestFromResponses(r responses.Request) (Request, error)
```

Include:
- first system -> instructions mapping
- subsequent system -> developer input item
- function_call / function_call_output mapping

**Verification**:
```bash
go test ./api/unified/... -v -run TestResponses -count=1
```

---

## Task 6: Add canonical event schema

**Files created**:
- `api/unified/types_event.go`

**Estimated time**: 5 min

```go
type StreamEvent struct {
	Type StreamEventType
	Started   *Started
	Delta     *Delta
	ToolCall  *ToolCall
	Content   *ContentPart
	Usage     *Usage
	Completed *Completed
	Error     *StreamError
	Extras    EventExtras
}

type EventExtras struct {
	RawEventName string
	RawEvent     map[string]any
	Provider     map[string]any
}
```

**Verification**:
```bash
go build ./api/unified/...
```

---

## Task 7: Messages event converter

**Files modified**:
- `api/unified/messages_api.go`
- `api/unified/messages_api_test.go`

**Estimated time**: 8 min

Key func:

```go
func EventFromMessages(ev any) (StreamEvent, bool, error)
```

Coverage:
- start/delta/tool/content/stop/error events
- known non-actionable events explicit no-op (`PingEvent`, non-accumulating stop blocks)
- unknown events preserved in `EventExtras.RawEventName`

**Verification**:
```bash
go test ./api/unified/... -v -run TestEventFromMessages -count=1
```

---

## Task 8: Completions event converter

**Files modified**:
- `api/unified/completions_api.go`
- `api/unified/completions_api_test.go`

**Estimated time**: 7 min

Key func:

```go
func EventFromCompletions(ev any) (StreamEvent, bool, error)
```

Coverage:
- chunk text/tool deltas
- usage-only chunks
- [DONE] terminal behavior represented through adapter flow

**Verification**:
```bash
go test ./api/unified/... -v -run TestEventFromCompletions -count=1
```

---

## Task 9: Responses event converter

**Files modified**:
- `api/unified/responses_api.go`
- `api/unified/responses_api_test.go`

**Estimated time**: 8 min

Key func:

```go
func EventFromResponses(ev any) (StreamEvent, bool, error)
```

Coverage:
- response.created/output_text/function_call delta/output_item.done/response.completed/response.failed/error
- known no-op events explicit
- unknown raw preservation

**Verification**:
```bash
go test ./api/unified/... -v -run TestEventFromResponses -count=1
```

---

## Task 10: Publisher bridge (replaces old adapt event mapping core)

**Files created**:
- `api/unified/publisher_bridge.go`
- `api/unified/publisher_bridge_test.go`

**Estimated time**: 7 min

Key func:

```go
func Publish(pub llm.Publisher, ev StreamEvent) error
```

Behavior:
- maps unified events to existing `llm` event types (`Started`, `Delta`, `ToolCall`, `ContentBlock`, `UsageRecord`, `Completed`, `Error`)
- no semantic changes vs old adapt behavior

**Verification**:
```bash
go test ./api/unified/... -v -run TestPublish -count=1
```

---

## Task 11: Migrate provider streaming paths to unified

**Files modified**:
- `provider/anthropic/anthropic.go`
- `provider/minimax/minimax.go`
- `provider/openai/openai.go`
- `provider/openrouter/openrouter.go`
- `provider/ollama/ollama.go`
- provider tests covering stream parity

**Estimated time**: 10 min

Implementation:
- keep provider external `CreateStream` behavior stable
- internally route provider request/event mapping through `api/unified`:
  - `llm.Request -> unified.Request`
  - `unified.Request -> wire request`
  - wire events -> `EventFrom<Protocol>` -> `Publish`
- remove provider-local duplicate mapping branches as unified takes over

**Verification**:
```bash
go test ./provider/... -v -count=1
```

---

## Task 12: Retire old adapt plan + cross-plan references

**Files modified**:
- `.agents/plans/PLAN-20260415-messages.md`
- `.agents/plans/PLAN-20260415-completions.md`
- `.agents/plans/PLAN-20260415-responses.md`
- `.agents/plans/DESIGN-api-extraction.md`
- `.agents/plans/DESIGN-api-unified.md`

**Files deleted**:
- (none; already removed in prior cleanup)

**Estimated time**: 4 min

- remove stale references to the retired adapt plan

**Verification**:
```bash
grep -r "PLAN-20260415-adapt.md" .agents/plans || true
# expected: historical/superseded references only (no active task dependency)
```

---

## Whole-phase verification

```bash
# Unified package
go build ./api/unified/...
go test ./api/unified/... -race -count=1
go vet ./api/unified/...

# Wire packages still green
go test ./api/messages/... ./api/completions/... ./api/responses/... -count=1

# Provider behavior remains green
go test ./provider/... -count=1
```

Acceptance gates:
- unified request/event conversions compile and pass tests
- provider stream path delegates through unified without behavior drift
- stale task/dependency references to retired adapt plan are removed
