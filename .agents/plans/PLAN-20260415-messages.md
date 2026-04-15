# PLAN: api/messages — Anthropic Messages API

> **Design ref**: `.agents/plans/DESIGN-api-extraction.md`
> **Depends on**: `PLAN-20260415-apicore.md` (must be complete first)
> **API reference**: https://docs.anthropic.com/en/api/messages
> **Streaming reference**: https://docs.anthropic.com/en/api/messages-streaming
> **Rate-limit headers**: https://docs.anthropic.com/en/api/rate-limits
> **Beta headers**: https://docs.anthropic.com/en/api/beta-headers
> **Estimated total**: ~22 min
> **Note**: convert/adapter logic has moved to `PLAN-20260415-adapt.md`

---

## Task 1: Create constants.go

**Files created**: `api/messages/constants.go`
**Estimated time**: 2 min

```go
// api/messages/constants.go
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

// SSE event names emitted by the Anthropic streaming API.
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

// Default path for the Messages API.
const DefaultPath = "/v1/messages"

// ThinkingMode controls extended thinking configuration.
type ThinkingMode int

const (
	ThinkingDisabled ThinkingMode = iota // omit thinking field
	ThinkingEnabled                      // type: "enabled", BudgetTokens required
	ThinkingAdaptive                     // type: "adaptive", model selects budget
)
```

**Verification**:
```bash
go build ./api/messages/...
```

---

## Task 2: Create types.go — request structs

**Files created**: `api/messages/types.go`
**Estimated time**: 5 min

```go
// api/messages/types.go
package messages

import "encoding/json"

// ── Request ──────────────────────────────────────────────────────────────────

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

// ThinkingConfig controls extended thinking.
// Type is "enabled", "adaptive", or "disabled".
type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

// OutputConfig controls format and effort of model output.
type OutputConfig struct {
	Format *JSONOutputFormat `json:"format,omitempty"`
	Effort string            `json:"effort,omitempty"` // "low", "medium", "high"
}

// JSONOutputFormat requests JSON output.
type JSONOutputFormat struct {
	Type   string `json:"type"`             // "json"
	Schema any    `json:"schema,omitempty"`
}

// Metadata passes optional user-level metadata.
type Metadata struct {
	UserID string `json:"user_id,omitempty"`
}

// SystemBlocks is a slice of TextBlocks used as the system prompt.
type SystemBlocks []*TextBlock

// Message is one turn in the conversation.
type Message struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content any    `json:"content"` // string or []any (polymorphic content blocks)
}

// TextBlock is a text content block.
type TextBlock struct {
	Type         string        `json:"type"` // "text"
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ImageBlock is an image content block.
type ImageBlock struct {
	Type   string      `json:"type"` // "image"
	Source ImageSource `json:"source"`
}

// ImageSource describes the image source.
type ImageSource struct {
	Type      string `json:"type"`                 // "base64" or "url"
	MediaType string `json:"media_type,omitempty"` // "image/jpeg", "image/png", etc.
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// ToolUseBlock is a tool call in an assistant message.
type ToolUseBlock struct {
	Type  string          `json:"type"` // "tool_use"
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResultBlock is a tool result in a user message.
type ToolResultBlock struct {
	Type      string `json:"type"`        // "tool_result"
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// ThinkingBlock is a thinking block in an assistant message.
type ThinkingBlock struct {
	Type      string `json:"type"`      // "thinking"
	Thinking  string `json:"thinking"`
	Signature string `json:"signature,omitempty"`
}

// ToolDefinition describes a tool the model may call.
type ToolDefinition struct {
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	InputSchema  any           `json:"input_schema"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// CacheControl enables prompt caching on a content block.
type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// ── SSE events ───────────────────────────────────────────────────────────────
// Ref: https://docs.anthropic.com/en/api/messages-streaming

// MessageStartEvent is emitted first, carrying the response ID and input token counts.
type MessageStartEvent struct {
	Message MessageStartPayload `json:"message"`
}

// MessageStartPayload is the payload of MessageStartEvent.
type MessageStartPayload struct {
	ID    string       `json:"id"`
	Model string       `json:"model"`
	Usage MessageUsage `json:"usage"`
}

// MessageUsage carries token counts from the message_start event.
type MessageUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// ContentBlockStartEvent marks the beginning of a content block.
type ContentBlockStartEvent struct {
	Index        int    `json:"index"`
	ContentBlock struct {
		Type string `json:"type"` // "text", "tool_use", "thinking"
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
	} `json:"content_block"`
}

// ContentBlockDeltaEvent carries an incremental delta within a content block.
type ContentBlockDeltaEvent struct {
	Index int   `json:"index"`
	Delta Delta `json:"delta"`
}

// Delta is the delta payload in ContentBlockDeltaEvent.
type Delta struct {
	// Type: "text_delta", "input_json_delta", "thinking_delta", "signature_delta"
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

// ContentBlockStopEvent signals the end of a content block.
// Not emitted to callers directly — superseded by the richer complete events below.
type ContentBlockStopEvent struct {
	Index int `json:"index"`
}

// TextCompleteEvent is synthesised by the parser on content_block_stop for text blocks.
// Carries the fully accumulated text and its block index.
type TextCompleteEvent struct {
	Index int
	Text  string
}

// ThinkingCompleteEvent is synthesised by the parser on content_block_stop for thinking blocks.
type ThinkingCompleteEvent struct {
	Index     int
	Thinking  string
	Signature string
}

// ToolCompleteEvent is synthesised by the parser on content_block_stop for tool_use blocks.
type ToolCompleteEvent struct {
	Index int
	ID    string
	Name  string
	Args  map[string]any // JSON-decoded input
}

// MessageDeltaEvent carries the stop reason and final output token count.
type MessageDeltaEvent struct {
	Delta struct {
		StopReason string `json:"stop_reason"` // "end_turn", "tool_use", "max_tokens"
	} `json:"delta"`
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// MessageStopEvent signals stream completion. No payload.
type MessageStopEvent struct{}

// StreamErrorEvent is a protocol-level error from the API.
type StreamErrorEvent struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// PingEvent is a keepalive. No meaningful payload.
type PingEvent struct{}
```

**Verification**:
```bash
go build ./api/messages/...
```

---

## Task 3: Create parser.go

**Files created**: `api/messages/parser.go`
**Estimated time**: 5 min

The parser routes on the SSE event **name** (`ev.Name`), which Anthropic sets to the
same string as the JSON `"type"` field. It maintains all accumulator state internally
so the adapter is stateless.

```go
// api/messages/parser.go
package messages

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codewandler/llm/api/apicore"
)

type textAccum struct {
	buf strings.Builder
}

type thinkingAccum struct {
	thinking  strings.Builder
	signature strings.Builder
}

type toolAccum struct {
	id     string
	name   string
	argBuf strings.Builder
}

// NewParser returns a ParserFactory for the Anthropic Messages API.
// Each call to the factory creates a fresh, isolated closure with its own
// accumulator state — safe for concurrent streams.
func NewParser() apicore.ParserFactory {
	return func() apicore.EventHandler {
		activeText     := make(map[int]*textAccum)
		activeThinking := make(map[int]*thinkingAccum)
		activeTools    := make(map[int]*toolAccum)

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
				switch evt.ContentBlock.Type {
				case "text":
					activeText[evt.Index] = &textAccum{}
				case "thinking":
					activeThinking[evt.Index] = &thinkingAccum{}
				case "tool_use":
					activeTools[evt.Index] = &toolAccum{id: evt.ContentBlock.ID, name: evt.ContentBlock.Name}
				}
				return apicore.StreamResult{Event: &evt}

			case EventContentBlockDelta:
				var evt ContentBlockDeltaEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse content_block_delta: %w", err)}
				}
				switch evt.Delta.Type {
				case "text_delta":
					if a := activeText[evt.Index]; a != nil {
						a.buf.WriteString(evt.Delta.Text)
					}
				case "thinking_delta":
					if a := activeThinking[evt.Index]; a != nil {
						a.thinking.WriteString(evt.Delta.Thinking)
					}
				case "signature_delta":
					if a := activeThinking[evt.Index]; a != nil {
						a.signature.WriteString(evt.Delta.Signature)
					}
				case "input_json_delta":
					if a := activeTools[evt.Index]; a != nil {
						a.argBuf.WriteString(evt.Delta.PartialJSON)
					}
				}
				return apicore.StreamResult{Event: &evt}

			case EventContentBlockStop:
				var raw struct {
					Index int `json:"index"`
				}
				if err := json.Unmarshal(data, &raw); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse content_block_stop: %w", err)}
				}
				idx := raw.Index

				if a, ok := activeText[idx]; ok {
					delete(activeText, idx)
					return apicore.StreamResult{Event: &TextCompleteEvent{Index: idx, Text: a.buf.String()}}
				}
				if a, ok := activeThinking[idx]; ok {
					delete(activeThinking, idx)
					return apicore.StreamResult{Event: &ThinkingCompleteEvent{
						Index:     idx,
						Thinking:  a.thinking.String(),
						Signature: a.signature.String(),
					}}
				}
				if a, ok := activeTools[idx]; ok {
					delete(activeTools, idx)
					var args map[string]any
					if a.argBuf.Len() > 0 {
						_ = json.Unmarshal([]byte(a.argBuf.String()), &args)
					}
					return apicore.StreamResult{Event: &ToolCompleteEvent{
						Index: idx,
						ID:    a.id,
						Name:  a.name,
						Args:  args,
					}}
				}
				// Unknown block type — no-op
				return apicore.StreamResult{}

			case EventMessageDelta:
				var evt MessageDeltaEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse message_delta: %w", err)}
				}
				return apicore.StreamResult{Event: &evt}

			case EventMessageStop:
				return apicore.StreamResult{Event: &MessageStopEvent{}, Done: true}

			case EventError:
				var evt StreamErrorEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse error event: %w", err)}
				}
				return apicore.StreamResult{
					Err:  fmt.Errorf("stream error %s: %s", evt.Error.Type, evt.Error.Message),
					Done: true,
				}

			case EventPing:
				return apicore.StreamResult{Event: &PingEvent{}}

			default:
				// Forward-compatible: silently ignore unknown events.
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

## Task 4: Create client.go

**Files created**: `api/messages/client.go`
**Estimated time**: 3 min

```go
// api/messages/client.go
package messages

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/codewandler/llm/api/apicore"
)

// Type aliases so callers write messages.Client, not apicore.Client[messages.Request].
type (
	Client       = apicore.Client[Request]
	ClientOption = apicore.ClientOption[Request]
)

// Re-export option constructors with the type parameter locked to Request.
// Callers: messages.WithBaseURL("...") instead of apicore.WithBaseURL[messages.Request]("...")
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
)

// NewClient creates a Messages API client.
// Caller must supply at least WithBaseURL and WithHeaderFunc (for auth).
func NewClient(opts ...ClientOption) *Client {
	defaults := []ClientOption{
		WithPath(DefaultPath),
		WithHeader(HeaderAnthropicVersion, APIVersion),
		WithErrorParser(parseAPIError),
	}
	return apicore.NewClient[Request](NewParser(), append(defaults, opts...)...)
}

// parseAPIError converts an Anthropic HTTP error body to a typed error.
// Ref: https://docs.anthropic.com/en/api/errors
func parseAPIError(statusCode int, body []byte) error {
	var resp struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return &apicore.HTTPError{StatusCode: statusCode, Body: body}
	}
	return fmt.Errorf("%s: %s (HTTP %d)",
		resp.Error.Type, resp.Error.Message, statusCode)
}

// AuthHeaderFunc returns a HeaderFunc that sets x-api-key.
func AuthHeaderFunc(apiKey string) apicore.HeaderFunc[Request] {
	return func(_ interface{ Done() <-chan struct{} }, _ *Request) (http.Header, error) {
		return http.Header{HeaderAPIKey: {apiKey}}, nil
	}
}
```

**Note**: `AuthHeaderFunc` signature needs the `context.Context` — correct it:

```go
import "context"

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

## Task 5: Write parser_test.go

**Files created**: `api/messages/parser_test.go`
**Estimated time**: 5 min

```go
// api/messages/parser_test.go
package messages_test

import (
	"testing"

	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/messages"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeHandler() apicore.EventHandler {
	return messages.NewParser()()
}

func TestParser_MessageStart(t *testing.T) {
	h := makeHandler()
	data := []byte(`{"message":{"id":"msg_01","model":"claude-3-5-haiku-20241022","usage":{"input_tokens":10}}}`)
	result := h(messages.EventMessageStart, data)
	require.NoError(t, result.Err)
	evt, ok := result.Event.(*messages.MessageStartEvent)
	require.True(t, ok)
	assert.Equal(t, "msg_01", evt.Message.ID)
	assert.Equal(t, 10, evt.Message.Usage.InputTokens)
}

func TestParser_TextBlock_AccumulatedAndComplete(t *testing.T) {
	h := makeHandler()

	// content_block_start (text)
	h(messages.EventContentBlockStart, []byte(`{"index":0,"content_block":{"type":"text"}}`))

	// two deltas
	h(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"text_delta","text":"hello "}}`))
	h(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"text_delta","text":"world"}}`))

	// stop → expect TextCompleteEvent
	result := h(messages.EventContentBlockStop, []byte(`{"index":0}`))
	require.NoError(t, result.Err)
	evt, ok := result.Event.(*messages.TextCompleteEvent)
	require.True(t, ok, "expected *TextCompleteEvent, got %T", result.Event)
	assert.Equal(t, "hello world", evt.Text)
	assert.Equal(t, 0, evt.Index)
}

func TestParser_ToolBlock_AccumulatedAndComplete(t *testing.T) {
	h := makeHandler()

	h(messages.EventContentBlockStart, []byte(`{"index":1,"content_block":{"type":"tool_use","id":"toolu_01","name":"get_weather"}}`))
	h(messages.EventContentBlockDelta, []byte(`{"index":1,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"Berlin"}}`))
	h(messages.EventContentBlockDelta, []byte(`{"index":1,"delta":{"type":"input_json_delta","partial_json":"\"}}"`))

	result := h(messages.EventContentBlockStop, []byte(`{"index":1}`))
	require.NoError(t, result.Err)
	evt, ok := result.Event.(*messages.ToolCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, "toolu_01", evt.ID)
	assert.Equal(t, "get_weather", evt.Name)
	assert.Equal(t, map[string]any{"city": "Berlin"}, evt.Args)
}

func TestParser_ThinkingBlock(t *testing.T) {
	h := makeHandler()
	h(messages.EventContentBlockStart, []byte(`{"index":0,"content_block":{"type":"thinking"}}`))
	h(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}`))
	h(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"signature_delta","signature":"sig123"}}`))

	result := h(messages.EventContentBlockStop, []byte(`{"index":0}`))
	evt, ok := result.Event.(*messages.ThinkingCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, "Let me think...", evt.Thinking)
	assert.Equal(t, "sig123", evt.Signature)
}

func TestParser_MessageStop_SetsDone(t *testing.T) {
	h := makeHandler()
	result := h(messages.EventMessageStop, []byte(`{}`))
	assert.True(t, result.Done)
	assert.IsType(t, &messages.MessageStopEvent{}, result.Event)
}

func TestParser_ErrorEvent_ReturnsDoneAndErr(t *testing.T) {
	h := makeHandler()
	result := h(messages.EventError, []byte(`{"error":{"type":"overloaded_error","message":"overloaded"}}`))
	assert.True(t, result.Done)
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "overloaded")
}

func TestParser_UnknownEvent_NoOp(t *testing.T) {
	h := makeHandler()
	result := h("future.event", []byte(`{"data":"x"}`))
	assert.Nil(t, result.Event)
	assert.NoError(t, result.Err)
	assert.False(t, result.Done)
}

func TestParser_IsolatedAcrossStreams(t *testing.T) {
	// Two handlers must NOT share accumulator state
	factory := messages.NewParser()
	h1 := factory()
	h2 := factory()

	// h1 starts a text block
	h1(messages.EventContentBlockStart, []byte(`{"index":0,"content_block":{"type":"text"}}`))
	h1(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"text_delta","text":"h1 text"}}`))

	// h2 starts a different text block
	h2(messages.EventContentBlockStart, []byte(`{"index":0,"content_block":{"type":"text"}}`))
	h2(messages.EventContentBlockDelta, []byte(`{"index":0,"delta":{"type":"text_delta","text":"h2 text"}}`))

	r1 := h1(messages.EventContentBlockStop, []byte(`{"index":0}`))
	r2 := h2(messages.EventContentBlockStop, []byte(`{"index":0}`))

	e1 := r1.Event.(*messages.TextCompleteEvent)
	e2 := r2.Event.(*messages.TextCompleteEvent)
	assert.Equal(t, "h1 text", e1.Text)
	assert.Equal(t, "h2 text", e2.Text)
}
```

**Verification**:
```bash
go test ./api/messages/... -v -run TestParser -count=1
```


---

## Phase completion check

```bash
go build ./api/messages/...
go test ./api/messages/... -race -count=1
go vet ./api/messages/...
```

All tests must pass. Convert/adapter work continues in `PLAN-20260415-adapt.md`.