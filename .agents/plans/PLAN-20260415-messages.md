# PLAN: api/messages — Anthropic Messages API

> **Design ref**: `.agents/plans/DESIGN-api-extraction.md`
> **Depends on**: `PLAN-20260415-apicore.md` (complete first)
> **Blocks**: `PLAN-20260415-adapt.md` (Task 1 — `messages_api.go`)
> **API reference**: https://docs.anthropic.com/en/api/messages
> **Streaming reference**: https://docs.anthropic.com/en/api/messages-streaming
> **Rate-limit headers**: https://docs.anthropic.com/en/api/rate-limits
> **Beta headers**: https://docs.anthropic.com/en/api/beta-headers
> **Estimated total**: ~40 min
> **Note**: convert/adapter logic lives in `PLAN-20260415-adapt.md`.

---

## What this package owns

`api/messages` is a **wire layer** (no `github.com/codewandler/llm` import).

| File | Responsibility |
|------|----------------|
| `constants.go` | Event names, header names, API versions, block/delta constants |
| `types.go` | Request + streaming wire structs |
| `parser.go` | Stateful EventHandler factory (text/tool/thinking accumulation) |
| `client.go` | NewClient, option aliases, auth helper, HTTP error parser |
| `testdata/` | Representative SSE fixtures |
| `parser_test.go` | Unit + fixture replay parser tests |
| `integration_test.go` | Optional live integration + raw-event drift visibility |

---

## Event coverage matrix (from streaming docs)

### Top-level SSE event names

| SSE event | Action |
|-----------|--------|
| `message_start` | parse → `MessageStartEvent` |
| `content_block_start` | parse → `ContentBlockStartEvent`; init accumulator for block types that stream deltas |
| `content_block_delta` | parse → `ContentBlockDeltaEvent`; append to accumulator by `index` |
| `content_block_stop` | parse index; emit synthesized complete event if accumulator exists; otherwise emit `ContentBlockStopEvent` |
| `message_delta` | parse → `MessageDeltaEvent` |
| `message_stop` | parse → `MessageStopEvent`, `Done: true` |
| `ping` | explicit known non-actionable event (`PingEvent`) |
| `error` | parse → `StreamErrorEvent`, `Err` + `Done: true` |
| unknown future | `default` no-op, reserved for forward compatibility |

### `content_block_start.content_block.type`

| Block type | Parser behavior |
|------------|-----------------|
| `text` | initialize text accumulator |
| `tool_use` | initialize tool accumulator with id/name |
| `thinking` | initialize thinking accumulator |
| `server_tool_use` | known non-accumulating block: emit start + stop only |
| `web_search_tool_result` | known non-accumulating block: emit start + stop only |
| unknown future | emit start + stop, no accumulator |

### `content_block_delta.delta.type`

| Delta type | Parser behavior |
|------------|-----------------|
| `text_delta` | append text fragment |
| `input_json_delta` | append tool argument JSON fragment |
| `thinking_delta` | append thinking fragment |
| `signature_delta` | append thinking signature fragment |
| unknown future | explicit no-op (do not fail stream) |

---

## Task 1: Create constants.go

**Files created**: `api/messages/constants.go`  
**Estimated time**: 3 min

```go
package messages

// Request headers.
// Ref: https://docs.anthropic.com/en/api/getting-started#headers
const (
	HeaderAPIKey           = "x-api-key"
	HeaderAnthropicVersion = "anthropic-version"
	HeaderAnthropicBeta    = "anthropic-beta"
)

// Anthropic API version sent in every request.
const APIVersion = "2023-06-01"

// Beta feature values for anthropic-beta header.
// Ref: https://docs.anthropic.com/en/api/beta-headers
const (
	BetaInterleavedThinking = "interleaved-thinking-2025-05-14"
)

// HTTP response headers for rate-limit tracking.
// Ref: https://docs.anthropic.com/en/api/rate-limits#response-headers
const (
	HeaderRateLimitReqLimit        = "anthropic-ratelimit-requests-limit"
	HeaderRateLimitReqRemaining    = "anthropic-ratelimit-requests-remaining"
	HeaderRateLimitReqReset        = "anthropic-ratelimit-requests-reset"
	HeaderRateLimitTokLimit        = "anthropic-ratelimit-tokens-limit"
	HeaderRateLimitTokRemaining    = "anthropic-ratelimit-tokens-remaining"
	HeaderRateLimitTokReset        = "anthropic-ratelimit-tokens-reset"
	HeaderRateLimitInTokLimit      = "anthropic-ratelimit-input-tokens-limit"
	HeaderRateLimitInTokRemaining  = "anthropic-ratelimit-input-tokens-remaining"
	HeaderRateLimitInTokReset      = "anthropic-ratelimit-input-tokens-reset"
	HeaderRateLimitOutTokLimit     = "anthropic-ratelimit-output-tokens-limit"
	HeaderRateLimitOutTokRemaining = "anthropic-ratelimit-output-tokens-remaining"
	HeaderRateLimitOutTokReset     = "anthropic-ratelimit-output-tokens-reset"
	HeaderRequestID                = "request-id"
)

// SSE event names emitted by the Anthropic Messages streaming API.
// Ref: https://docs.anthropic.com/en/api/messages-streaming#event-types
const (
	EventMessageStart      = "message_start"
	EventContentBlockStart = "content_block_start"
	EventContentBlockDelta = "content_block_delta"
	EventContentBlockStop  = "content_block_stop"
	EventMessageDelta      = "message_delta"
	EventMessageStop       = "message_stop"
	EventError             = "error"
	EventPing              = "ping"
)

// Content block types (content_block_start.content_block.type).
const (
	BlockTypeText                = "text"
	BlockTypeToolUse             = "tool_use"
	BlockTypeThinking            = "thinking"
	BlockTypeServerToolUse       = "server_tool_use"
	BlockTypeWebSearchToolResult = "web_search_tool_result"
)

// Delta types (content_block_delta.delta.type).
const (
	DeltaTypeText      = "text_delta"
	DeltaTypeInputJSON = "input_json_delta"
	DeltaTypeThinking  = "thinking_delta"
	DeltaTypeSignature = "signature_delta"
)

// Stop reasons (message_delta.delta.stop_reason).
const (
	StopReasonEndTurn = "end_turn"
	StopReasonToolUse = "tool_use"
	StopReasonMaxTok  = "max_tokens"
)

// Default path for the Messages API.
const DefaultPath = "/v1/messages"

// ThinkingMode controls extended thinking configuration.
type ThinkingMode int

const (
	ThinkingDisabled ThinkingMode = iota
	ThinkingEnabled
	ThinkingAdaptive
)
```

**Verification**:
```bash
go build ./api/messages/...
```

---

## Task 2: Create types.go

**Files created**: `api/messages/types.go`  
**Estimated time**: 6 min

```go
package messages

import "encoding/json"

// Request is the wire body for POST /v1/messages.
// Ref: https://docs.anthropic.com/en/api/messages#body
type Request struct {
	Model        string           `json:"model"`
	MaxTokens    int              `json:"max_tokens"`
	Stream       bool             `json:"stream"`
	System       SystemBlocks     `json:"system,omitempty"`
	Messages     []Message        `json:"messages"`
	Tools        []ToolDefinition `json:"tools,omitempty"`
	ToolChoice   any              `json:"tool_choice,omitempty"`
	Thinking     *ThinkingConfig  `json:"thinking,omitempty"`
	Metadata     *Metadata        `json:"metadata,omitempty"`
	TopK         int              `json:"top_k,omitempty"`
	TopP         float64          `json:"top_p,omitempty"`
	OutputConfig *OutputConfig    `json:"output_config,omitempty"`
}

type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type OutputConfig struct {
	Format *JSONOutputFormat `json:"format,omitempty"`
	Effort string            `json:"effort,omitempty"`
}

type JSONOutputFormat struct {
	Type   string `json:"type"`
	Schema any    `json:"schema,omitempty"`
}

type Metadata struct {
	UserID string `json:"user_id,omitempty"`
}

type SystemBlocks []*TextBlock

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type TextBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type ImageBlock struct {
	Type   string      `json:"type"`
	Source ImageSource `json:"source"`
}

type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type ToolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type ToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type ThinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature,omitempty"`
}

type ToolDefinition struct {
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	InputSchema  any           `json:"input_schema"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type CacheControl struct {
	Type string `json:"type"`
}

// SSE: message_start
type MessageStartEvent struct {
	Message MessageStartPayload `json:"message"`
}

type MessageStartPayload struct {
	ID    string       `json:"id"`
	Model string       `json:"model"`
	Usage MessageUsage `json:"usage"`
}

type MessageUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// SSE: content_block_start
type ContentBlockStartEvent struct {
	Index        int             `json:"index"`
	ContentBlock json.RawMessage `json:"content_block"`
}

// StartBlockView is an optional helper for callers/tests that need typed views
// over ContentBlockStartEvent.ContentBlock.
type StartBlockView struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// SSE: content_block_delta
type ContentBlockDeltaEvent struct {
	Index int   `json:"index"`
	Delta Delta `json:"delta"`
}

type Delta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

// SSE: content_block_stop
type ContentBlockStopEvent struct {
	Index int `json:"index"`
}

// Parser-synthesized completion events (from content_block_stop)
type TextCompleteEvent struct {
	Index int
	Text  string
}

type ThinkingCompleteEvent struct {
	Index     int
	Thinking  string
	Signature string
}

type ToolCompleteEvent struct {
	Index int
	ID    string
	Name  string
	Args  map[string]any
}

// SSE: message_delta
type MessageDeltaEvent struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// SSE: message_stop
type MessageStopEvent struct{}

// SSE: error
type StreamErrorEvent struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (e *StreamErrorEvent) Error() string {
	return "messages stream error " + e.Error.Type + ": " + e.Error.Message
}

// SSE: ping
type PingEvent struct{}
```

**Verification**:
```bash
go build ./api/messages/...
```

---

## Task 3: Create parser.go

**Files created**: `api/messages/parser.go`  
**Estimated time**: 7 min

```go
package messages

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codewandler/llm/api/apicore"
)

type textAccum struct{ buf strings.Builder }

type thinkingAccum struct {
	thinking  strings.Builder
	signature strings.Builder
}

type toolAccum struct {
	id     string
	name   string
	argBuf strings.Builder
}

func NewParser() apicore.ParserFactory {
	return func() apicore.EventHandler {
		activeText := make(map[int]*textAccum)
		activeThinking := make(map[int]*thinkingAccum)
		activeTools := make(map[int]*toolAccum)

		return func(name string, data []byte) apicore.StreamResult {
			switch name {
			case EventMessageStart:
				var evt MessageStartEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse message_start: %w", err)}
				}
				return apicore.StreamResult{Event: &evt}

			case EventContentBlockStart:
				var evt ContentBlockStartEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse content_block_start: %w", err)}
				}

				// Decode type/id/name view for accumulator init.
				var block StartBlockView
				if err := json.Unmarshal(evt.ContentBlock, &block); err == nil {
					switch block.Type {
					case BlockTypeText:
						activeText[evt.Index] = &textAccum{}
					case BlockTypeThinking:
						activeThinking[evt.Index] = &thinkingAccum{}
					case BlockTypeToolUse:
						activeTools[evt.Index] = &toolAccum{id: block.ID, name: block.Name}
					case BlockTypeServerToolUse, BlockTypeWebSearchToolResult:
						// Known non-accumulating block types.
					}
				}
				return apicore.StreamResult{Event: &evt}

			case EventContentBlockDelta:
				var evt ContentBlockDeltaEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse content_block_delta: %w", err)}
				}

				switch evt.Delta.Type {
				case DeltaTypeText:
					if a := activeText[evt.Index]; a != nil {
						a.buf.WriteString(evt.Delta.Text)
					}
				case DeltaTypeThinking:
					if a := activeThinking[evt.Index]; a != nil {
						a.thinking.WriteString(evt.Delta.Thinking)
					}
				case DeltaTypeSignature:
					if a := activeThinking[evt.Index]; a != nil {
						a.signature.WriteString(evt.Delta.Signature)
					}
				case DeltaTypeInputJSON:
					if a := activeTools[evt.Index]; a != nil {
						a.argBuf.WriteString(evt.Delta.PartialJSON)
					}
				default:
					// Unknown future delta subtype: explicit no-op.
				}
				return apicore.StreamResult{Event: &evt}

			case EventContentBlockStop:
				var evt ContentBlockStopEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse content_block_stop: %w", err)}
				}
				idx := evt.Index

				if a, ok := activeText[idx]; ok {
					delete(activeText, idx)
					return apicore.StreamResult{Event: &TextCompleteEvent{Index: idx, Text: a.buf.String()}}
				}
				if a, ok := activeThinking[idx]; ok {
					delete(activeThinking, idx)
					return apicore.StreamResult{Event: &ThinkingCompleteEvent{
						Index: idx, Thinking: a.thinking.String(), Signature: a.signature.String(),
					}}
				}
				if a, ok := activeTools[idx]; ok {
					delete(activeTools, idx)
					var args map[string]any
					if a.argBuf.Len() > 0 {
						_ = json.Unmarshal([]byte(a.argBuf.String()), &args)
					}
					return apicore.StreamResult{Event: &ToolCompleteEvent{Index: idx, ID: a.id, Name: a.name, Args: args}}
				}

				// Known non-accumulating block stop (server_tool_use, web_search_tool_result)
				// or unknown block type: keep stop observable.
				return apicore.StreamResult{Event: &evt}

			case EventMessageDelta:
				var evt MessageDeltaEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse message_delta: %w", err)}
				}
				return apicore.StreamResult{Event: &evt}

			case EventPing:
				return apicore.StreamResult{Event: &PingEvent{}}

			case EventMessageStop:
				return apicore.StreamResult{Event: &MessageStopEvent{}, Done: true}

			case EventError:
				var evt StreamErrorEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse error event: %w", err), Done: true}
				}
				return apicore.StreamResult{Err: &evt, Done: true}

			default:
				// Forward-compatible unknown event.
				return apicore.StreamResult{}
			}
		}
	}
}
```

**Verification**:
```bash
go build ./api/messages/...
```

---

## Task 4: Create client.go

**Files created**: `api/messages/client.go`  
**Estimated time**: 3 min

```go
package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/codewandler/llm/api/apicore"
)

type (
	Client       = apicore.Client[Request]
	ClientOption = apicore.ClientOption[Request]
)

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

func NewClient(opts ...ClientOption) *Client {
	defaults := []ClientOption{
		WithPath(DefaultPath),
		WithHeader(HeaderAnthropicVersion, APIVersion),
		WithErrorParser(parseAPIError),
	}
	return apicore.NewClient[Request](NewParser(), append(defaults, opts...)...)
}

func parseAPIError(statusCode int, body []byte) error {
	var resp struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
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

func AuthHeaderFunc(apiKey string) apicore.HeaderFunc[Request] {
	return func(_ context.Context, _ *Request) (http.Header, error) {
		return http.Header{HeaderAPIKey: {apiKey}}, nil
	}
}
```

**Verification**:
```bash
go build ./api/messages/...
```

---

## Task 5: Add fixtures

**Files created**:
- `api/messages/testdata/text_stream.sse`
- `api/messages/testdata/tool_stream.sse`
- `api/messages/testdata/thinking_stream.sse`
- `api/messages/testdata/error_stream.sse`
- `api/messages/testdata/web_search_stream.sse`

**Estimated time**: 4 min

Include at least one fixture with:
- `server_tool_use` block type
- `web_search_tool_result` block type

so parser behavior for known non-accumulating block types is exercised.

**Verification**:
```bash
ls api/messages/testdata/
```

---

## Task 6: Write parser_test.go

**Files created**: `api/messages/parser_test.go`  
**Estimated time**: 7 min

Coverage requirements:

1. Actionable events:
- `message_start`
- `content_block_start`
- `content_block_delta` (`text_delta`, `thinking_delta`, `signature_delta`, `input_json_delta`)
- `content_block_stop` with synthesized events (`TextCompleteEvent`, `ThinkingCompleteEvent`, `ToolCompleteEvent`)
- `message_delta`
- `message_stop`
- `error`

2. Known non-actionable explicit event:
- `ping`

3. Known non-accumulating block types:
- `server_tool_use` and `web_search_tool_result` should emit start + stop events (no synthesized completion event)

4. Unknown future fallback:
- unknown event name returns no-op
- unknown delta subtype returns no-op inside delta-switch without failing stream

5. Per-stream isolation:
- two handlers from one factory must not share accumulators

6. Fixture replay tests with `apicore.FixedSSEResponse`

**Verification**:
```bash
go test ./api/messages/... -v -count=1 -run "TestParser|TestFixture"
```

---

## Task 7: Optional live integration (OpenRouter Messages)

**Files created**: `api/messages/integration_test.go`  
**Estimated time**: 7 min

Purpose:
- live end-to-end check (HTTP + SSE + parser + typed events)
- raw-event drift visibility via `WithParseHook`

Required env vars:
- `OPENROUTER_API_KEY`
- `OPENROUTER_MESSAGES_MODEL` (explicit, no default to avoid accidental cost)

Optional strict mode:
- `MESSAGES_STRICT_EVENTS=1` (fail on unknown unhandled raw event names)

Pattern:

```go
rawCounts := map[string]int{}
client := messages.NewClient(
	messages.WithBaseURL("https://openrouter.ai/api"),
	messages.WithHeaderFunc(messages.AuthHeaderFunc(apiKey)),
	messages.WithHeader("HTTP-Referer", "https://github.com/codewandler/llm"),
	messages.WithHeader("X-Title", "llm-integration-test"),
	messages.WithParseHook(func(_ *messages.Request, eventName string, _ []byte) any {
		if eventName != "" {
			rawCounts[eventName]++
		}
		return nil
	}),
)

// After stream collection:
// - compare rawCounts against handled + known-no-op event sets
// - log unknown unhandled names
// - if MESSAGES_STRICT_EVENTS=1, fail test on unknowns
```

**Verification**:
```bash
OPENROUTER_API_KEY=$OPENROUTER_API_KEY \
OPENROUTER_MESSAGES_MODEL=$OPENROUTER_MESSAGES_MODEL \
MESSAGES_STRICT_EVENTS=1 \
go test ./api/messages/... -v -run TestIntegration -count=1
```

---

## Phase completion check

```bash
# Build
go build ./api/messages/...

# Unit + fixture tests
go test ./api/messages/... -v -count=1

# Race detector
go test ./api/messages/... -race -count=1

# Vet
go vet ./api/messages/...

# Wire-layer import boundary (must print nothing)
grep -r '"github.com/codewandler/llm' api/messages/ --include="*.go" | grep -v '_test.go'

# Optional live integration (explicit model only)
OPENROUTER_API_KEY=$OPENROUTER_API_KEY \
OPENROUTER_MESSAGES_MODEL=$OPENROUTER_MESSAGES_MODEL \
MESSAGES_STRICT_EVENTS=1 \
go test ./api/messages/... -v -run TestIntegration -count=1
```

All tests must pass. Adapt-layer implementation continues in `PLAN-20260415-adapt.md`.
