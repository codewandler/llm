# PLAN: api/completions — OpenAI Chat Completions API

> **Design ref**: `.agents/plans/DESIGN-api-extraction.md`
> **Depends on**: `PLAN-20260415-apicore.md` (must be complete first)
> **Blocks**: `PLAN-20260415-adapt.md` (Task 2 — CompletionsAdapter)
> **API reference**: https://platform.openai.com/docs/api-reference/chat/create
> **Streaming reference**: https://platform.openai.com/docs/api-reference/chat/streaming
> **Rate-limit headers**: https://platform.openai.com/docs/guides/rate-limits#headers
> **Estimated total**: ~30 min
> **Note**: convert/adapter logic lives in `PLAN-20260415-adapt.md` (`completions_api.go`)

---

## What this package owns

`api/completions` is a **pure wire layer** — no `github.com/codewandler/llm` import.

| File | Responsibility |
|------|----------------|
| `constants.go` | Header names, stream sentinel, finish-reason constants |
| `types.go` | JSON wire structs for request + streaming chunk payloads |
| `parser.go` | Stateless-per-stream parser factory (`[DONE]` + chunk decode) |
| `client.go` | `NewClient`, option aliases, `BearerAuthFunc`, `parseAPIError` |
| `testdata/` | Recorded SSE fixtures for parser tests |
| `parser_test.go` | Parser unit tests + fixture replay tests |
| `client_test.go` | Constructor/auth/error-parser tests |
| `integration_test.go` | Optional real API smoke test (env-gated) |

### Key parser invariants

| Behaviour | Detail |
|-----------|--------|
| Terminal event | `data: [DONE]` returns `Done: true` |
| SSE event name | Ignored for routing (Chat Completions uses data-only SSE) |
| Chunk payload | Non-`[DONE]` data must decode as `Chunk` |
| Usage chunk | Valid chunk may have `choices: []` and only `usage` |
| Tool deltas | Parser only decodes raw tool fragments; accumulation is adapter responsibility |
| Unknown fields | Ignored by `encoding/json` (forward-compatible) |
| Malformed chunk | Returns `Err` with `Done: false` (stream may continue to `[DONE]`) |

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
**Estimated time**: 5 min

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
	Usage   *Usage   `json:"usage,omitempty"` // final chunk when include_usage=true
}

// Choice is one completion choice inside a Chunk.
type Choice struct {
	Index        int    `json:"index"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason"` // "", stop/tool_calls/length/content_filter
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

// Usage is the token usage in the final Chunk (when IncludeUsage=true).
type Usage struct {
	PromptTokens            int         `json:"prompt_tokens"`
	CompletionTokens        int         `json:"completion_tokens"`
	TotalTokens             int         `json:"total_tokens"`
	PromptTokensDetails     *TokDetails `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *TokDetails `json:"completion_tokens_details,omitempty"`
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

Chat Completions streams are data-only SSE: parser routes by payload, not `event:` name.

```go
// api/completions/parser.go
package completions

import (
	"encoding/json"
	"fmt"

	"github.com/codewandler/llm/api/apicore"
)

// NewParser returns a ParserFactory for Chat Completions streaming payloads.
//
// Notes:
//  - The SSE event name is ignored (Chat Completions uses data-only SSE).
//  - Terminal signal is the literal StreamDone payload.
//  - Tool-call accumulation is adapter responsibility, not parser responsibility.
func NewParser() apicore.ParserFactory {
	return func() apicore.EventHandler {
		return func(_ string, data []byte) apicore.StreamResult {
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

---

## Task 4: Create client.go

**Files created**: `api/completions/client.go`
**Estimated time**: 4 min

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
	WithLogger       = apicore.WithLogger[Request]
)

// NewClient creates a Chat Completions client with protocol defaults.
// Caller supplies WithBaseURL and auth headers.
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
	if err := json.Unmarshal(body, &resp); err != nil || resp.Error.Message == "" {
		return &apicore.HTTPError{StatusCode: statusCode, Body: body}
	}
	if resp.Error.Type != "" {
		return fmt.Errorf("%s: %s (HTTP %d)", resp.Error.Type, resp.Error.Message, statusCode)
	}
	return fmt.Errorf("openai error: %s (HTTP %d)", resp.Error.Message, statusCode)
}
```

**Verification**:
```bash
go build ./api/completions/...
```

---

## Task 5: Create testdata SSE fixtures

**Files created**:
- `api/completions/testdata/text_stream.sse`
- `api/completions/testdata/tool_stream.sse`
- `api/completions/testdata/usage_stream.sse`
- `api/completions/testdata/malformed_stream.sse`

**Estimated time**: 3 min

**`api/completions/testdata/text_stream.sse`**
```text
data: {"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"},"finish_reason":""}]}

data: {"id":"chatcmpl-1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":""}]}

data: [DONE]
```

**`api/completions/testdata/tool_stream.sse`**
```text
data: {"id":"chatcmpl-2","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"loc\""}}]},"finish_reason":""}]}

data: {"id":"chatcmpl-2","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"Berlin\"}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]
```

**`api/completions/testdata/usage_stream.sse`**
```text
data: {"id":"chatcmpl-3","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":""}]}

data: {"id":"chatcmpl-3","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}}

data: [DONE]
```

**`api/completions/testdata/malformed_stream.sse`**
```text
data: {not valid json

data: [DONE]
```

**Verification**:
```bash
ls api/completions/testdata/
```

---

## Task 6: Write parser_test.go

**Files created**: `api/completions/parser_test.go`
**Estimated time**: 6 min

```go
// api/completions/parser_test.go
package completions_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/completions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeHandler() apicore.EventHandler {
	return completions.NewParser()()
}

func fixtureClient(t *testing.T, name string) *http.Client {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return &http.Client{Transport: apicore.FixedSSEResponse(200, string(data))}
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
	result := h("", []byte(completions.StreamDone))
	assert.True(t, result.Done)
	assert.Nil(t, result.Event)
}

func TestParser_DoneSignal_IgnoresEventName(t *testing.T) {
	h := makeHandler()
	result := h("unexpected", []byte(completions.StreamDone))
	assert.True(t, result.Done)
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
	assert.False(t, result.Done)
}

func TestParser_IsolatedState(t *testing.T) {
	factory := completions.NewParser()
	h1, h2 := factory(), factory()
	data := []byte(`{"id":"c1","model":"m","choices":[]}`)
	r1 := h1("", data)
	r2 := h2("", data)
	require.NoError(t, r1.Err)
	require.NoError(t, r2.Err)
}

func TestParser_FixtureTextStream(t *testing.T) {
	c := fixtureClient(t, "text_stream.sse")
	client := completions.NewClient(
		completions.WithBaseURL("https://example.com"),
		completions.WithHTTPClient(c),
	)
	handle, err := client.Stream(t.Context(), &completions.Request{Model: "gpt-4o", Stream: true})
	require.NoError(t, err)

	var text strings.Builder
	for ev := range handle.Events {
		require.NoError(t, ev.Err)
		if chunk, ok := ev.Event.(*completions.Chunk); ok && len(chunk.Choices) > 0 {
			text.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	assert.Equal(t, "Hello", text.String())
}

func TestParser_FixtureMalformedThenDone(t *testing.T) {
	c := fixtureClient(t, "malformed_stream.sse")
	client := completions.NewClient(
		completions.WithBaseURL("https://example.com"),
		completions.WithHTTPClient(c),
	)
	handle, err := client.Stream(t.Context(), &completions.Request{Model: "gpt-4o", Stream: true})
	require.NoError(t, err)

	var sawErr, sawDone bool
	for ev := range handle.Events {
		if ev.Err != nil {
			sawErr = true
		}
		if ev.Done {
			sawDone = true
		}
	}
	assert.True(t, sawErr)
	assert.True(t, sawDone)
}

func TestParser_FixtureUsageChunk(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "usage_stream.sse"))
	require.NoError(t, err)
	h := makeHandler()

	var usage *completions.Usage
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		res := h("", []byte(payload))
		if chunk, ok := res.Event.(*completions.Chunk); ok && chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	require.NotNil(t, usage)
	assert.Equal(t, 2, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 1, usage.CompletionTokensDetails.ReasoningTokens)
}
```

**Verification**:
```bash
go test ./api/completions/... -v -run TestParser -count=1
```

---

## Task 7: Write client_test.go

**Files created**: `api/completions/client_test.go`
**Estimated time**: 4 min

```go
// api/completions/client_test.go
package completions

import (
	"context"
	"net/http"
	"testing"

	"github.com/codewandler/llm/api/apicore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_DefaultPath(t *testing.T) {
	var gotPath string
	httpClient := &http.Client{Transport: apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: http.NoBody}, nil
	})}
	c := NewClient(WithBaseURL("https://api.example.com"), WithHTTPClient(httpClient))
	_, err := c.Stream(context.Background(), &Request{Model: "gpt-4o", Stream: true})
	require.NoError(t, err)
	assert.Equal(t, DefaultPath, gotPath)
}

func TestBearerAuthFunc(t *testing.T) {
	fn := BearerAuthFunc("sk-test")
	h, err := fn(context.Background(), &Request{})
	require.NoError(t, err)
	assert.Equal(t, "Bearer sk-test", h.Get("Authorization"))
}

func TestParseAPIError_JSON(t *testing.T) {
	err := parseAPIError(429, []byte(`{"error":{"type":"rate_limit_error","message":"too many requests"}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limit_error")
	assert.Contains(t, err.Error(), "too many requests")
}

func TestParseAPIError_FallbackHTTPError(t *testing.T) {
	err := parseAPIError(500, []byte("not-json"))
	var hErr *apicore.HTTPError
	require.ErrorAs(t, err, &hErr)
	assert.Equal(t, 500, hErr.StatusCode)
}

func TestParseAPIError_EmptyMessage_FallbackHTTPError(t *testing.T) {
	err := parseAPIError(400, []byte(`{"error":{"type":"invalid_request_error","message":""}}`))
	var hErr *apicore.HTTPError
	require.ErrorAs(t, err, &hErr)
	assert.Equal(t, 400, hErr.StatusCode)
}
```

**Verification**:
```bash
go test ./api/completions/... -v -run 'TestNewClient|TestBearerAuthFunc|TestParseAPIError' -count=1
```

---

## Task 8: Write integration_test.go (optional, env-gated)

**Files created**: `api/completions/integration_test.go`
**Estimated time**: 3 min

```go
// api/completions/integration_test.go
package completions_test

import (
	"context"
	"os"
	"testing"

	"github.com/codewandler/llm/api/completions"
	"github.com/stretchr/testify/require"
)

// Requires:
//   OPENROUTER_API_KEY
//   OPENROUTER_COMPLETIONS_FREE_MODEL (optional; default google/gemma-3-27b-it:free)
//
// OpenRouter exposes an OpenAI-compatible Chat Completions endpoint and offers
// free models, making this test runnable without paid OpenAI credits.
func TestIntegration_CompletionsStream(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set — skipping integration test")
	}

	model := os.Getenv("OPENROUTER_COMPLETIONS_FREE_MODEL")
	if model == "" {
		model = "google/gemma-3-27b-it:free"
	}

	client := completions.NewClient(
		completions.WithBaseURL("https://openrouter.ai/api/v1"),
		completions.WithHeaderFunc(completions.BearerAuthFunc(apiKey)),
	)

	handle, err := client.Stream(context.Background(), &completions.Request{
		Model:  model,
		Stream: true,
		Messages: []completions.Message{
			{Role: "user", Content: "Reply with exactly: ok"},
		},
		StreamOptions: &completions.StreamOptions{IncludeUsage: true},
	})
	require.NoError(t, err)

	var sawDone bool
	for ev := range handle.Events {
		if ev.Err != nil {
			t.Fatalf("stream error: %v", ev.Err)
		}
		if ev.Done {
			sawDone = true
		}
	}
	require.True(t, sawDone)
}
```

**Verification**:
```bash
OPENROUTER_API_KEY=*** \
OPENROUTER_COMPLETIONS_FREE_MODEL=google/gemma-3-27b-it:free \
go test ./api/completions/... -run TestIntegration -count=1 -v
```

---

## Phase completion check

```bash
go build ./api/completions/...
go test ./api/completions/... -race -count=1
go vet ./api/completions/...
```

All tests must pass. Adapter/convert work continues in `PLAN-20260415-adapt.md`.
