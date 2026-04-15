# PLAN: api/completions — OpenAI Chat Completions API

> **Design ref**: `.agents/plans/DESIGN-api-extraction.md`
> **Depends on**: `PLAN-20260415-apicore.md` (must be complete first)
> **API reference**: https://platform.openai.com/docs/api-reference/chat/create
> **Streaming reference**: https://platform.openai.com/docs/api-reference/chat/streaming
> **Rate-limit headers**: https://platform.openai.com/docs/guides/rate-limits#headers
> **Estimated total**: ~18 min
> **Note**: convert/adapter logic has moved to `PLAN-20260415-adapt.md`

---

## Task 1: Create constants.go

**Files created**: `api/completions/constants.go`
**Estimated time**: 2 min

```go
// api/completions/constants.go
package completions

// HTTP response headers set by OpenAI.
// Ref: https://platform.openai.com/docs/guides/rate-limits#headers
const (
	HeaderRateLimitReqLimit     = "x-ratelimit-limit-requests"
	HeaderRateLimitReqRemaining = "x-ratelimit-remaining-requests"
	HeaderRateLimitReqReset     = "x-ratelimit-reset-requests"
	HeaderRateLimitTokLimit     = "x-ratelimit-limit-tokens"
	HeaderRateLimitTokRemaining = "x-ratelimit-remaining-tokens"
	HeaderRequestID             = "x-request-id"
)

// StreamDone is the SSE sentinel that terminates a Chat Completions stream.
const StreamDone = "[DONE]"

// finish_reason values from the API.
const (
	FinishReasonStop          = "stop"
	FinishReasonToolCalls     = "tool_calls"
	FinishReasonLength        = "length"
	FinishReasonContentFilter = "content_filter"
)

// Default API path.
const DefaultPath = "/v1/chat/completions"
```

**Verification**:
```bash
go build ./api/completions/...
```

---

## Task 2: Create types.go

**Files created**: `api/completions/types.go`
**Estimated time**: 4 min

```go
// api/completions/types.go
package completions

// ── Request ──────────────────────────────────────────────────────────────────

// Request is the wire body for POST /v1/chat/completions.
// Ref: https://platform.openai.com/docs/api-reference/chat/create
type Request struct {
	Model                string          `json:"model"`
	Messages             []Message       `json:"messages"`
	Tools                []Tool          `json:"tools,omitempty"`
	ToolChoice           any             `json:"tool_choice,omitempty"`
	ReasoningEffort      string          `json:"reasoning_effort,omitempty"`
	PromptCacheRetention string          `json:"prompt_cache_retention,omitempty"`
	MaxTokens            int             `json:"max_tokens,omitempty"`
	Temperature          float64         `json:"temperature,omitempty"`
	TopP                 float64         `json:"top_p,omitempty"`
	TopK                 int             `json:"top_k,omitempty"`
	ResponseFormat       *ResponseFormat `json:"response_format,omitempty"`
	Stream               bool            `json:"stream"`
	StreamOptions        *StreamOptions  `json:"stream_options,omitempty"`
}

// Message is a chat message in the messages array.
type Message struct {
	Role       string     `json:"role"` // "system", "user", "assistant", "tool"
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a tool call stored in an assistant message.
type ToolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"` // "function"
	Function FuncCall `json:"function"`
}

// FuncCall is the function invocation inside a ToolCall.
type FuncCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded
}

// Tool is a tool definition in the request.
type Tool struct {
	Type     string      `json:"type"` // "function"
	Function FuncPayload `json:"function"`
}

// FuncPayload is the function spec in a Tool definition.
type FuncPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
	Strict      bool   `json:"strict,omitempty"`
}

// ResponseFormat controls structured output.
type ResponseFormat struct {
	Type string `json:"type"` // "json_object", "text"
}

// StreamOptions controls stream metadata.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// ── SSE Stream ───────────────────────────────────────────────────────────────
// Ref: https://platform.openai.com/docs/api-reference/chat/streaming

// Chunk is one SSE payload in a Chat Completions stream.
type Chunk struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"` // only in final chunk when include_usage=true
}

// Choice is one completion choice inside a Chunk.
type Choice struct {
	Index        int    `json:"index"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason"` // "", "stop", "tool_calls", "length", "content_filter"
}

// Delta is the content delta in a Choice.
type Delta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []ToolCallDelta `json:"tool_calls,omitempty"`
}

// ToolCallDelta is an incremental tool call fragment in a streaming Delta.
type ToolCallDelta struct {
	Index    int           `json:"index"`
	ID       string        `json:"id,omitempty"`
	Type     string        `json:"type,omitempty"` // "function"
	Function FuncCallDelta `json:"function"`
}

// FuncCallDelta is an incremental function call fragment.
type FuncCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"` // accumulate across chunks
}

// Usage is the token usage in the final Chunk (when StreamOptions.IncludeUsage=true).
type Usage struct {
	PromptTokens            int          `json:"prompt_tokens"`
	CompletionTokens        int          `json:"completion_tokens"`
	TotalTokens             int          `json:"total_tokens"`
	PromptTokensDetails     *TokDetails  `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *TokDetails  `json:"completion_tokens_details,omitempty"`
}

// TokDetails breaks down prompt or completion token counts.
type TokDetails struct {
	CachedTokens    int `json:"cached_tokens,omitempty"`
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}
```

**Verification**:
```bash
go build ./api/completions/...
```

---

## Task 3: Create parser.go

**Files created**: `api/completions/parser.go`
**Estimated time**: 3 min

Chat Completions sends `data: {json}` lines only — no `event:` line, so `name` is
always `""`. The stream ends with `data: [DONE]`.

The parser is intentionally simple: it just deserialises each `Chunk`. Tool-call
accumulation is **adapter** responsibility because a single chunk can interleave
partial calls from multiple tool slots across multiple choices.

```go
// api/completions/parser.go
package completions

import (
	"encoding/json"
	"fmt"

	"github.com/codewandler/llm/api/apicore"
)

// NewParser returns a ParserFactory for the Chat Completions streaming API.
func NewParser() apicore.ParserFactory {
	return func() apicore.EventHandler {
		return func(name string, data []byte) apicore.StreamResult {
			// name is always "" for Chat Completions (no SSE event: line)
			if string(data) == StreamDone {
				return apicore.StreamResult{Done: true}
			}
			var chunk Chunk
			if err := json.Unmarshal(data, &chunk); err != nil {
				return apicore.StreamResult{Err: fmt.Errorf("parse chunk: %w", err)}
			}
			return apicore.StreamResult{Event: &chunk}
		}
	}
}
```

**Verification**:
```bash
go build ./api/completions/...
```


## Task 4: Create client.go

**Files created**: `api/completions/client.go`
**Estimated time**: 3 min

```go
// api/completions/client.go
package completions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/codewandler/llm/api/apicore"
)

// Type aliases.
type (
	Client       = apicore.Client[Request]
	ClientOption = apicore.ClientOption[Request]
)

// Re-export option constructors with type parameter locked to Request.
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

// NewClient creates a Chat Completions client with sensible defaults.
// Caller must provide WithBaseURL and auth via WithHeader or WithHeaderFunc.
func NewClient(opts ...ClientOption) *Client {
	defaults := []ClientOption{
		WithPath(DefaultPath),
		WithErrorParser(parseAPIError),
	}
	return apicore.NewClient[Request](NewParser(), append(defaults, opts...)...)
}

// BearerAuthFunc returns a HeaderFunc that sets Authorization: Bearer <key>.
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
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return &apicore.HTTPError{StatusCode: statusCode, Body: body}
	}
	return fmt.Errorf("%s: %s (HTTP %d)",
		resp.Error.Type, resp.Error.Message, statusCode)
}
```

**Verification**:
```bash
go build ./api/completions/...
```


## Task 5: Write parser_test.go

**Files created**: `api/completions/parser_test.go`
**Estimated time**: 4 min

```go
// api/completions/parser_test.go
package completions_test

import (
	"testing"

	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/completions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeHandler() apicore.EventHandler {
	return completions.NewParser()()
}

func TestParser_TextChunk(t *testing.T) {
	h := makeHandler()
	data := []byte(`{"id":"c1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":""}]}`)
	result := h("", data)
	require.NoError(t, result.Err)
	assert.False(t, result.Done)
	chunk, ok := result.Event.(*completions.Chunk)
	require.True(t, ok)
	require.Len(t, chunk.Choices, 1)
	assert.Equal(t, "hello", chunk.Choices[0].Delta.Content)
}

func TestParser_DoneSignal(t *testing.T) {
	h := makeHandler()
	result := h("", []byte("[DONE]"))
	assert.True(t, result.Done)
	assert.Nil(t, result.Event)
}

func TestParser_FinalChunkWithUsage(t *testing.T) {
	h := makeHandler()
	data := []byte(`{"id":"c1","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	result := h("", data)
	require.NoError(t, result.Err)
	chunk := result.Event.(*completions.Chunk)
	require.NotNil(t, chunk.Usage)
	assert.Equal(t, 10, chunk.Usage.PromptTokens)
	assert.Equal(t, 5, chunk.Usage.CompletionTokens)
}

func TestParser_MalformedJSON_ReturnsError(t *testing.T) {
	h := makeHandler()
	result := h("", []byte(`{not valid json`))
	require.Error(t, result.Err)
}

func TestParser_IsolatedState(t *testing.T) {
	// Each handler from the factory is independent
	factory := completions.NewParser()
	h1, h2 := factory(), factory()
	data := []byte(`{"id":"c1","model":"m","choices":[]}`)
	r1 := h1("", data)
	r2 := h2("", data)
	require.NoError(t, r1.Err)
	require.NoError(t, r2.Err)
}
```

**Verification**:
```bash
go test ./api/completions/... -v -run TestParser -count=1
```


---

## Phase completion check

```bash
go build ./api/completions/...
go test ./api/completions/... -race -count=1
go vet ./api/completions/...
```

All tests must pass. Convert/adapter work continues in `PLAN-20260415-adapt.md`.