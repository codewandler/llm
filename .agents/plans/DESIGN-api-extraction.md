# Design: API Client Extraction

> **Scope**: Extract wire-protocol clients into reusable packages.
> **Out of scope**: Model registry, offerings, smart routing (see DESIGN-model-registry.md).
> **Out of scope**: Bedrock (proprietary AWS SDK protocol).

---

## Problem

Wire-format logic (request types, SSE parsers, request builders) lives inside
`provider/<name>` — packages that are supposed to represent a *company's service*.

This causes **lateral imports between providers**:
- `provider/minimax` imports `provider/anthropic` for `BuildRequest()` + `ParseStreamWith()`
- `provider/openrouter` imports `provider/anthropic` for the same reason
- `provider/openrouter` imports `provider/openai` for `RespParseStream()` + `UseResponsesAPI()`
- `provider/openrouter` **duplicates** Chat Completions request building + parsing
  (742 lines) because importing `provider/openai` would leak OpenAI-specific model logic
- `provider/dockermr` wraps `provider/openai` entirely (same wire format)

A new provider that speaks "Anthropic Messages" (e.g. Google Vertex AI Claude, or a
future Azure Claude) would need to import `provider/anthropic` — mixing Google's
provider with Anthropic's package.

**One goal of this design: no provider in `provider/` reaches into another provider.**

---

## Solution

Extract three API client packages named by **protocol**, not by company,
built on a shared generic client:

```
api/
├── apicore/          # Generic client infrastructure (no protocol knowledge)
│   ├── client.go     # Client[Req] — generic HTTP+SSE client
│   ├── options.go    # ClientOption[Req], WithBaseURL, WithHeader, etc.
│   ├── stream.go     # StreamResult, StreamHandle, ParserFactory, EventHandler, ParseHook
│   ├── constants.go  # Shared HTTP constants, retryable status codes
│   ├── retry.go      # RetryTransport, RetryConfig (composable RoundTripper)
│   ├── adapter.go    # AdapterConfig, AdapterOption (shared identity)
│   └── testing.go    # Test helpers: RoundTripFunc, FixedSSEResponse, NewTestHandle
│
├── messages/         # Anthropic Messages API (/v1/messages)
│   ├── types.go      # Wire types: Request, Message, SSE events (no llm dependency)
│   ├── constants.go  # Header names, API versions, SSE event names
│   ├── parser.go     # Messages EventHandler factory (no llm dependency)
│   └── client.go     # NewClient (convenience + defaults), option aliases
│
├── completions/      # OpenAI Chat Completions API (/v1/chat/completions)
│   ├── types.go      # Wire types (no llm dependency)
│   ├── constants.go
│   ├── parser.go     # (no llm dependency)
│   └── client.go
│
├── responses/        # OpenAI Responses API (/v1/responses)
│   ├── types.go      # Wire types (no llm dependency)
│   ├── constants.go
│   ├── parser.go     # (no llm dependency)
│   └── client.go
│
└── adapt/            # Bridges wire API types ↔ llm domain (only layer with llm import)
    ├── messages_api.go     # MessagesRequestFromLLM, MessagesAdapter, MessagesStreamer
    ├── completions_api.go  # CompletionsRequestFromLLM, CompletionsAdapter, CompletionsStreamer
    └── responses_api.go    # ResponsesRequestFromLLM, ResponsesAdapter, ResponsesStreamer
```

---

## Architecture: Shared Generic Client + Protocol-Specific Layers

The key insight: all three API clients do the same thing — take a typed request,
serialize it, send HTTP, parse SSE back into typed events. The only differences
are the request type, the parser, and defaults. This is a textbook generic.

### `api/apicore` — The Generic Client

```go
package apicore

import "log/slog"

// Client[Req] is a generic HTTP+SSE streaming client parameterized by the
// wire request type. It handles serialization, HTTP mechanics, headers,
// transforms, SSE scanning, hook calling, and channel management.
// Protocol-specific behavior is injected via the EventParser and request type.
type Client[Req any] struct {
    baseURL      string
    path         string
    httpClient   *http.Client
    headers      http.Header        // static headers merged into every request
    headerFunc   HeaderFunc[Req]    // dynamic headers (auth, model-conditional)
    transform    TransformFunc[Req] // surgical request modification
    parseHook    ParseHook[Req]     // optional: emit provider-specific events
    responseHook ResponseHook[Req]  // optional: inspect response headers
    parser       ParserFactory      // creates a per-stream event handler
    errParser    ErrorParser        // optional: parse protocol-specific HTTP errors
    logger       *slog.Logger       // optional: structured logging
}

// HeaderFunc returns headers to add to each HTTP request.
// It receives the typed wire request so headers can be conditional on
// request fields (e.g. model-dependent beta headers).
type HeaderFunc[Req any] func(ctx context.Context, req *Req) (http.Header, error)

// TransformFunc allows inspection and surgical modification of the fully-built
// wire request before JSON serialization.
type TransformFunc[Req any] func(req *Req) any

// ParserFactory creates a new EventHandler for a single stream. Each call
// returns a fresh handler with its own state (tool accumulation, block
// tracking, etc.). Called once per Stream() invocation.
type ParserFactory func() EventHandler

// EventHandler processes a single raw SSE event. It receives the SSE event
// name (empty string for Chat Completions) and the raw JSON data bytes.
// Returns StreamResult with Done=true on terminal events.
type EventHandler func(name string, data []byte) StreamResult

// ParseHook is called for each SSE event after the standard EventHandler.
// It receives the original wire request (for model-aware decisions) plus
// the SSE event name and raw JSON data.
// If it returns non-nil, the value is emitted as a standalone StreamResult
// on the channel immediately after the standard event.
type ParseHook[Req any] func(req *Req, eventName string, data []byte) any

// ResponseHook is called after receiving the HTTP response, before stream
// parsing begins. It receives the wire request and response metadata
// (status + headers) for inspection — extracting rate limits, request IDs,
// provider quotas, logging per-model metrics, etc.
// Called on EVERY response, including 2xx. The body is NOT available
// (it's owned by the SSE parser).
type ResponseHook[Req any] func(req *Req, meta ResponseMeta)

// ResponseMeta is the HTTP response metadata available to the ResponseHook.
type ResponseMeta struct {
    StatusCode int
    Headers    http.Header
}

// ErrorParser converts a non-2xx HTTP response into a typed error.
// Not generic — error format is protocol-level, not request-dependent.
// If nil, apicore returns a generic HTTPError with status + body.
type ErrorParser func(statusCode int, body []byte) error

// HTTPError is the default error for non-2xx responses when no ErrorParser
// is configured.
type HTTPError struct {
    StatusCode int
    Body       []byte
}

func (e *HTTPError) Error() string { ... }

// --- Options (all generic over Req) ---

type ClientOption[Req any] func(*Client[Req])

func WithBaseURL[Req any](url string) ClientOption[Req]                       { ... }
func WithPath[Req any](path string) ClientOption[Req]                         { ... }
func WithHTTPClient[Req any](c *http.Client) ClientOption[Req]                { ... }
func WithHeader[Req any](key, value string) ClientOption[Req]                 { ... }
func WithHeaderFunc[Req any](fn HeaderFunc[Req]) ClientOption[Req]            { ... }
func WithTransform[Req any](fn TransformFunc[Req]) ClientOption[Req]          { ... }
func WithParseHook[Req any](fn ParseHook[Req]) ClientOption[Req]              { ... }
func WithResponseHook[Req any](fn ResponseHook[Req]) ClientOption[Req]        { ... }
func WithErrorParser[Req any](fn ErrorParser) ClientOption[Req]               { ... }
func WithLogger[Req any](logger *slog.Logger) ClientOption[Req]               { ... }

func NewClient[Req any](parser ParserFactory, opts ...ClientOption[Req]) *Client[Req] { ... }

// --- Stream ---

type StreamHandle struct {
    Events  <-chan StreamResult
    Request *http.Request  // the sent request (for observability)
    Headers http.Header    // response headers (rate limits, etc.)
}

type StreamResult struct {
    Event any    // standard typed event OR provider-specific event from ParseHook
    Err   error
    Done  bool   // true on terminal event
}

// Stream sends a streaming request and returns native typed events.
// All SSE scanning, hook calling, and channel management happens here.
func (c *Client[Req]) Stream(ctx context.Context, req *Req) (*StreamHandle, error) {
    // 1. Call HeaderFunc(ctx, req) → dynamic headers (model-conditional)
    // 2. Apply TransformFunc(req) → final serializable value
    // 3. Serialize to JSON
    // 4. Build http.Request with static headers + dynamic headers
    // 5. Log request (if logger configured): method, URL, content-length
    // 6. Send request via httpClient (retry is handled by transport layer)
    // 7. Log response (if logger configured): status, latency
    // 8. Call ResponseHook(req, meta) (if configured) with status + headers
    //    — runs on ALL responses including 2xx, before error check
    //    — providers extract rate limits, request IDs, quotas here
    // 9. Check status code:
    //    - 2xx: proceed to stream parsing
    //    - non-2xx: read body, return ErrorParser(status, body) or *HTTPError
    // 10. Create EventHandler from ParserFactory (fresh state per stream)
    // 11. Start background goroutine:
    //     a. SSE scanning (via internal/sse)
    //     b. For each SSE event:
    //        - Call EventHandler(name, data) → emit StreamResult
    //        - Call ParseHook(req, name, data) → if non-nil, emit standalone StreamResult
    //        - Log SSE event at Debug level (if logger configured)
    //     c. Close channel when done
    // 12. Return StreamHandle
}
```

**What apicore owns (written once):**
- HTTP request building, header merging, JSON serialization
- Transform application
- Structured logging (`*slog.Logger`)
- Response hook invocation (header extraction point)
- HTTP error handling (status check + error parsing)
- SSE scanning (`internal/sse`)
- ParseHook integration
- Channel creation, goroutine lifecycle, body cleanup

**What protocol packages provide:**
- `ParserFactory` — returns a stateful `EventHandler` per stream
- `ErrorParser` — converts HTTP error bodies into protocol-specific errors
- Wire types, default path

**What providers compose via options:**
- `ResponseHook` — extract rate limits, request IDs, quotas from headers
- `RetryTransport` — wrap `http.RoundTripper` for retry with backoff
- `ParseHook` — extract provider-specific data from SSE events

### Protocol Packages: Thin Wrappers + Types + Parsers

Each `api/<protocol>` package provides:
1. **Wire types** — request/response structs matching the API spec
2. **ParserFactory** — returns a stateful `EventHandler` per stream
3. **ErrorParser** — converts protocol-specific HTTP error bodies
4. **`NewClient`** — convenience constructor with protocol defaults
5. **Option aliases** — hide the generic type parameter for clean call sites
6. **Adapter** — bridges native events ↔ `llm.Publisher` (protocol-specific)
7. **Convert** — `llm.Request` → wire request (protocol-specific)

```go
package messages

import "github.com/codewandler/llm/api/apicore"

// Type aliases hide the generic parameter for clean call sites.
type Client = apicore.Client[Request]
type ClientOption = apicore.ClientOption[Request]

// Re-exported option constructors — callers write messages.WithBaseURL(...),
// not apicore.WithBaseURL[messages.Request](...).
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

// NewClient creates a Messages API client with Anthropic defaults.
func NewClient(opts ...ClientOption) *Client {
    defaults := []ClientOption{
        WithPath("/v1/messages"),
        apicore.WithErrorParser[Request](parseAPIError),
    }
    return apicore.NewClient[Request](newParser, append(defaults, opts...)...)
}

// newParser returns a fresh stateful event handler for one stream.
func newParser() apicore.EventHandler {
    var currentBlockIdx int
    var toolBlocks map[int]*toolBlock
    // ... per-stream state

    return func(name string, data []byte) apicore.StreamResult {
        switch name {
        case EventMessageStart:
            // ...
        case EventContentBlockDelta:
            // ... uses currentBlockIdx
        case EventMessageStop:
            return apicore.StreamResult{Event: &MessageStopEvent{}, Done: true}
        }
        // ...
    }
}

// parseAPIError converts Anthropic error JSON to a typed error.
func parseAPIError(statusCode int, body []byte) error {
    var apiErr struct {
        Type  string `json:"type"`
        Error struct {
            Type    string `json:"type"`
            Message string `json:"message"`
        } `json:"error"`
    }
    if err := json.Unmarshal(body, &apiErr); err != nil {
        return &apicore.HTTPError{StatusCode: statusCode, Body: body}
    }
    return fmt.Errorf("%s: %s (HTTP %d)", apiErr.Error.Type, apiErr.Error.Message, statusCode)
}
```

Provider code looks identical to the non-generic version:
```go
client := messages.NewClient(
    messages.WithBaseURL("https://api.anthropic.com"),
    messages.WithHeader(messages.HeaderAnthropicVersion, messages.APIVersion20230601),
    messages.WithTransform(func(req *messages.Request) any { ... }),
)
```

The generic type parameter is completely hidden from callers.

### What's Shared vs. Protocol-Specific

| `apicore` (shared, no llm import) | `messages` / `completions` / `responses` (no llm import) | `adapt` (llm import only here) |
|---|---|---|
| HTTP request building | Wire types (Request, SSE events) | `llm.Request` → wire `Request` |
| Header merging (static + dynamic) | Stateful SSE `EventHandler` | Wire events → `llm.Publisher` calls |
| TransformFunc application | `ErrorParser` (HTTP errors) | `usage.Record` construction |
| JSON serialization | Default path + headers | `tool.NewToolCall` mapping |
| Structured logging (`slog`) | `NewClient` convenience ctor | `StopReason` mapping |
| ResponseHook invocation | Option aliases (hide generics) | `llm.ProviderRequestFromHTTP` |
| HTTP error handling | | `msg.Text`, `msg.Thinking` |
| SSE scanning (`internal/sse`) | | `sortmap.NewSortedMap` |
| ParseHook integration | | |
| Channel + goroutine lifecycle | | |
| RetryTransport (composable) | | |

### Three Layers (revised)

```
┌──────────────────────────────────────────────────────────┐
│  Layer 1: Wire Types                (per-protocol)       │
│  Pure structs matching the API specification.            │
│  No llm dependency. JSON-serializable.                   │
│  api/<protocol>/types.go                                 │
├──────────────────────────────────────────────────────────┤
│  Layer 2: Generic Client + Parser   (shared + per-proto) │
│  apicore.Client[Req] handles HTTP, headers, transforms.  │
│  Per-protocol parser function injected at construction.   │
│  No llm dependency.                                      │
│  api/apicore/ + api/<protocol>/parser.go                  │
├──────────────────────────────────────────────────────────┤
│  Layer 3: Adapter                   (per-protocol)       │
│  Converts llm.Request → wire Request.                    │
│  Maps native events → llm.Publisher calls.               │
│  api/<protocol>/adapter.go + convert.go                   │
└──────────────────────────────────────────────────────────┘
```

**Layer 1 + 2** are usable standalone — a consumer can call the Anthropic Messages API
without importing `llm` at all. This is the "native client" use case.

**Layer 3** is what our providers use. The adapter bridges native events → `llm.Publisher`.

---

## Layer 1: Wire Types

Each package defines **exported** structs matching the API's JSON schema.
These are the single source of truth for request/response shapes.

### `api/messages/types.go`

Moved from `provider/anthropic/request.go`, `event.go`, `message.go`, `cache.go`:

```go
package messages

// --- Request ---

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
    CacheControl *CacheControl    `json:"cache_control,omitempty"`
    TopK         int              `json:"top_k,omitempty"`
    TopP         float64          `json:"top_p,omitempty"`
    OutputConfig *OutputConfig    `json:"output_config,omitempty"`
}

// Content block types (Message, TextBlock, ToolUseBlock, etc.)
// ... moved from message.go

// --- SSE Events ---

type MessageStartEvent struct { ... }     // from event.go
type ContentBlockDeltaEvent struct { ... }
// ... all SSE event types
```

### `api/completions/types.go`

Moved from `provider/openai/api_completions.go`:

```go
package completions

type Request struct {
    Model                string          `json:"model"`
    Messages             []Message       `json:"messages"`
    Tools                []Tool          `json:"tools,omitempty"`
    ToolChoice           any             `json:"tool_choice,omitempty"`
    MaxTokens            int             `json:"max_tokens,omitempty"`
    Temperature          float64         `json:"temperature,omitempty"`
    TopP                 float64         `json:"top_p,omitempty"`
    TopK                 int             `json:"top_k,omitempty"`
    ResponseFormat       *ResponseFormat `json:"response_format,omitempty"`
    Stream               bool            `json:"stream"`
    StreamOptions        *StreamOptions  `json:"stream_options,omitempty"`
    ReasoningEffort      string          `json:"reasoning_effort,omitempty"`
    PromptCacheRetention string          `json:"prompt_cache_retention,omitempty"`
}

type StreamChunk struct {
    ID      string   `json:"id"`
    Model   string   `json:"model"`
    Choices []Choice `json:"choices"`
    Usage   *Usage   `json:"usage,omitempty"`
}

// ... Message, Choice, Delta, ToolCallDelta, Usage, etc.
```

### `api/responses/types.go`

Moved from `provider/openai/api_responses.go`:

```go
package responses

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

// SSE event types: ResponseCreated, OutputItemAdded, TextDelta, etc.
// ... moved from api_responses.go
```

---

## Layer 2 Details: Configuration, Transforms, Parse Hooks

The generic `apicore.Client[Req]` and its types are defined above in the Architecture
section. This section covers usage patterns and examples.

### TransformFunc Examples

**MiniMax — remove unsupported fields:**
```go
messages.WithTransform(func(req *messages.Request) any {
    req.Thinking = nil // MiniMax doesn't support the thinking field
    return req
})
```

**OpenRouter — inject provider-specific fields:**
```go
messages.WithTransform(func(req *messages.Request) any {
    return struct {
        *messages.Request
        Provider  *orProviderConfig `json:"provider,omitempty"`
        SessionID string            `json:"session_id,omitempty"`
        Models    []string          `json:"models,omitempty"`
    }{
        Request:   req,
        Provider:  p.providerRouting,
        SessionID: p.sessionID,
    }
})
```

### Provider Configuration Examples

**Anthropic direct:**
```go
client := messages.NewClient(
    messages.WithBaseURL("https://api.anthropic.com"),
    messages.WithHeaderFunc(func(ctx context.Context, req *messages.Request) (http.Header, error) {
        key, err := p.opts.ResolveAPIKey(ctx)
        if err != nil { return nil, err }
        h := http.Header{}
        h.Set(messages.HeaderAPIKey, key)
        h.Set(messages.HeaderAnthropicVersion, messages.APIVersion20230601)
        if needsInterleavedThinking(req.Model) {
            h.Set(messages.HeaderAnthropicBeta, messages.BetaInterleavedThinking)
        }
        return h, nil
    }),
)
```

**MiniMax (Anthropic-compatible):**
```go
client := messages.NewClient(
    messages.WithBaseURL("https://api.minimax.io/anthropic"),
    messages.WithHeaderFunc(func(ctx context.Context, req *messages.Request) (http.Header, error) {
        key, err := p.opts.ResolveAPIKey(ctx)
        if err != nil { return nil, err }
        h := http.Header{}
        h.Set(messages.HeaderAPIKey, key)
        h.Set("Authorization", "Bearer "+key)
        h.Set(messages.HeaderAnthropicVersion, messages.APIVersion20230601)
        return h, nil
    }),
    messages.WithTransform(func(req *messages.Request) any {
        req.Thinking = nil
        return req
    }),
)
```

**OpenRouter via Messages API:**
```go
client := messages.NewClient(
    messages.WithBaseURL("https://openrouter.ai/api"),
    messages.WithHeaderFunc(func(ctx context.Context, req *messages.Request) (http.Header, error) {
        key, err := p.opts.ResolveAPIKey(ctx)
        if err != nil { return nil, err }
        h := http.Header{}
        h.Set("Authorization", "Bearer "+key)
        h.Set(messages.HeaderAnthropicVersion, messages.APIVersion20230601)
        if needsInterleavedThinking(req.Model) {
            h.Set(messages.HeaderAnthropicBeta, messages.BetaInterleavedThinking)
        }
        return h, nil
    }),
    messages.WithTransform(func(req *messages.Request) any {
        return struct {
            *messages.Request
            Provider  *orProviderConfig `json:"provider,omitempty"`
            SessionID string            `json:"session_id,omitempty"`
        }{Request: req, Provider: p.routing, SessionID: p.sessionID}
    }),
)
```

**OpenRouter via Responses API:**
```go
client := responses.NewClient(
    responses.WithBaseURL("https://openrouter.ai/api"),
    responses.WithHeaderFunc(func(ctx context.Context, req *responses.Request) (http.Header, error) {
        key, err := p.opts.ResolveAPIKey(ctx)
        if err != nil { return nil, err }
        h := http.Header{}
        h.Set("Authorization", "Bearer "+key)
        return h, nil
    }),
    responses.WithTransform(func(req *responses.Request) any {
        return struct {
            *responses.Request
            Provider  *orProviderConfig `json:"provider,omitempty"`
            SessionID string            `json:"session_id,omitempty"`
        }{Request: req, Provider: p.routing, SessionID: p.sessionID}
    }),
)
```

**Ollama via Completions API:**
```go
client := completions.NewClient(
    completions.WithBaseURL("http://localhost:11434"),
    completions.WithPath("/v1/chat/completions"),
    completions.WithHTTPClient(p.client),
)
```

### Custom Parse Hooks

For each SSE event, the parser first emits the standard typed event, then calls
the `ParseHook`. If the hook returns non-nil, the result is emitted as a
**separate `StreamResult`** on the same channel — a standalone event.

The channel sequence for one SSE event with a hook:
```
→ StreamResult{Event: MessageStopEvent{...}}     // standard, always emitted
→ StreamResult{Event: &OpenRouterUsage{Cost: …}}  // from hook, only if non-nil
```

**Example — OpenRouter extracting cost + upstream provider from Messages API:**
```go
type OpenRouterUsage struct {
    Cost     float64 `json:"cost,omitempty"`
    Provider string  `json:"provider,omitempty"`
}

msgClient := messages.NewClient(
    messages.WithBaseURL("https://openrouter.ai/api"),
    // ... auth, transform ...
    messages.WithParseHook(func(req *messages.Request, eventName string, data []byte) any {
        if eventName != messages.EventMessageDelta && eventName != messages.EventMessageStop {
            return nil // only inspect events that might carry usage/cost
        }
        var extra OpenRouterUsage
        _ = json.Unmarshal(data, &extra)
        if extra.Cost > 0 || extra.Provider != "" {
            return &extra
        }
        return nil
    }),
)
```

The adapter handles hook events with the same type switch:
```go
for result := range handle.Events {
    switch evt := result.Event.(type) {
    case *messages.MessageStartEvent:
        a.handleStart(evt, pub)
    case *messages.ContentBlockDeltaEvent:
        a.handleDelta(evt, pub)
    case *messages.MessageStopEvent:
        a.handleStop(evt, pub)

    // Provider-specific events from ParseHook:
    case *OpenRouterUsage:
        a.reportedCost = evt.Cost
    }
}
```

### SSE Parser (apicore owns scanning, protocol owns parsing)

SSE scanning, hook calling, and channel management are handled by
`apicore.Client[Req].Stream()`. The protocol only provides a stateful
`EventHandler` that maps `(eventName, data) → StreamResult`.

Inside `apicore.Client.Stream()`:
```go
// apicore — this logic is written ONCE for all protocols
handler := c.parser()  // fresh per-stream state from ParserFactory
ch := make(chan StreamResult)
go func() {
    defer close(ch)
    defer resp.Body.Close()
    sse.ForEachEvent(ctx, resp.Body, func(name string, data []byte) bool {
        // 1. Protocol-specific parsing
        result := handler(name, data)
        ch <- result

        // 2. ParseHook (if configured)
        if c.parseHook != nil {
            if extra := c.parseHook(req, name, data); extra != nil {
                ch <- StreamResult{Event: extra}
            }
        }
        return !result.Done
    })
}()
```

Each protocol parser is just a closure with state:
```go
// api/messages/parser.go
func newParser() apicore.EventHandler {
    var blockIdx int
    var toolBlocks map[int]*toolAccumulator

    return func(name string, data []byte) apicore.StreamResult {
        switch name {
        case EventMessageStart:
            var evt MessageStartEvent
            json.Unmarshal(data, &evt)
            return apicore.StreamResult{Event: &evt}

        case EventContentBlockDelta:
            var evt ContentBlockDeltaEvent
            json.Unmarshal(data, &evt)
            return apicore.StreamResult{Event: &evt}

        case EventError:
            var evt StreamErrorEvent
            json.Unmarshal(data, &evt)
            return apicore.StreamResult{Err: &evt}

        case EventMessageStop:
            return apicore.StreamResult{Event: &MessageStopEvent{}, Done: true}
        // ...
        }
    }
}
```

**Zero boilerplate per parser** — no SSE scanning, no channel management,
no hook calling, no body closing. Just `(name, data) → StreamResult`.

### Response Header Extraction (ResponseHook)

Providers need to read metadata from HTTP response headers.

**No magic strings.** All header names are defined as constants in the
respective protocol or provider package. Code never uses bare string
literals for header keys, status codes, SSE event names, API versions,
or other protocol values.

```go
// api/messages/constants.go — Anthropic Messages API constants
package messages

// HTTP request headers.
const (
    HeaderAnthropicVersion = "Anthropic-Version"
    HeaderAnthropicBeta    = "Anthropic-Beta"
    HeaderAPIKey           = "x-api-key"
)

// API versions.
const (
    APIVersion20230601 = "2023-06-01"
    BetaInterleavedThinking = "interleaved-thinking-2025-05-14"
)

// HTTP response headers (rate limits, request tracking).
const (
    HeaderRateLimitRequestsLimit     = "anthropic-ratelimit-requests-limit"
    HeaderRateLimitRequestsRemaining = "anthropic-ratelimit-requests-remaining"
    HeaderRateLimitRequestsReset     = "anthropic-ratelimit-requests-reset"
    HeaderRateLimitTokensLimit       = "anthropic-ratelimit-tokens-limit"
    HeaderRateLimitTokensRemaining   = "anthropic-ratelimit-tokens-remaining"
    HeaderRateLimitTokensReset       = "anthropic-ratelimit-tokens-reset"
    HeaderRateLimitInputTokensLimit      = "anthropic-ratelimit-input-tokens-limit"
    HeaderRateLimitInputTokensRemaining  = "anthropic-ratelimit-input-tokens-remaining"
    HeaderRateLimitInputTokensReset      = "anthropic-ratelimit-input-tokens-reset"
    HeaderRateLimitOutputTokensLimit     = "anthropic-ratelimit-output-tokens-limit"
    HeaderRateLimitOutputTokensRemaining = "anthropic-ratelimit-output-tokens-remaining"
    HeaderRateLimitOutputTokensReset     = "anthropic-ratelimit-output-tokens-reset"
    HeaderRequestID                  = "request-id"
)

// SSE event names.
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
```

```go
// api/completions/constants.go — OpenAI Chat Completions API constants
package completions

const (
    HeaderRateLimitRequestsLimit     = "x-ratelimit-limit-requests"
    HeaderRateLimitRequestsRemaining = "x-ratelimit-remaining-requests"
    HeaderRateLimitRequestsReset     = "x-ratelimit-reset-requests"
    HeaderRateLimitTokensLimit       = "x-ratelimit-limit-tokens"
    HeaderRateLimitTokensRemaining   = "x-ratelimit-remaining-tokens"
    HeaderRequestID                  = "x-request-id"
)

// SSE sentinel.
const StreamDone = "[DONE]"
```

```go
// api/apicore/constants.go — shared HTTP constants
package apicore

const (
    HeaderRetryAfter  = "Retry-After"
    HeaderContentType = "Content-Type"

    ContentTypeJSON        = "application/json"
    ContentTypeEventStream = "text/event-stream"
)

// Retryable HTTP status codes.
const (
    StatusTooManyRequests     = 429
    StatusInternalServerError = 500
    StatusBadGateway          = 502
    StatusServiceUnavailable  = 503
    StatusOverloaded          = 529 // Anthropic-specific
)
```

The `ResponseHook` runs after receiving the HTTP response, before stream
parsing begins. It sees status + headers (not the body) and is called on
**every** response including 2xx.

The hook receives `ResponseMeta` (read-only view, no body access) and is
observation-only — it cannot abort the request or modify the response.
Error handling remains the responsibility of `ErrorParser`.

```go
// Provider extracts rate limits into its own state using named constants:
client := messages.NewClient(
    messages.WithBaseURL("https://api.anthropic.com"),
    messages.WithHeaderFunc(func(ctx context.Context, req *messages.Request) (http.Header, error) {
        key, err := p.opts.ResolveAPIKey(ctx)
        if err != nil { return nil, err }
        h := http.Header{}
        h.Set(messages.HeaderAPIKey, key)
        h.Set(messages.HeaderAnthropicVersion, messages.APIVersion20230601)
        if needsInterleavedThinking(req.Model) {
            h.Set(messages.HeaderAnthropicBeta, messages.BetaInterleavedThinking)
        }
        return h, nil
    }),
    // ResponseHook receives the request for per-model tracking:
    messages.WithResponseHook(func(req *messages.Request, meta apicore.ResponseMeta) {
        p.mu.Lock()
        defer p.mu.Unlock()
        p.rateLimit = RateLimitInfo{
            Model:             req.Model,
            RequestsRemaining: parseIntHeader(meta.Headers, messages.HeaderRateLimitRequestsRemaining),
            RequestsReset:     parseTimeHeader(meta.Headers, messages.HeaderRateLimitRequestsReset),
            TokensRemaining:   parseIntHeader(meta.Headers, messages.HeaderRateLimitTokensRemaining),
            TokensReset:       parseTimeHeader(meta.Headers, messages.HeaderRateLimitTokensReset),
            RequestID:         meta.Headers.Get(messages.HeaderRequestID),
        }
    }),
)
```

The adapter also has access to response headers via `StreamHandle.Headers`
for anything needed during event processing (e.g. including `request-id`
in usage records).

**This no-magic-values rule applies everywhere:**
- Request header names → `Header*` constants
- Response header names → `Header*` constants
- API versions → `APIVersion*` / `Beta*` constants
- SSE event names → `Event*` constants (used in parsers and `ParseHook`)
- HTTP status codes → `Status*` constants (used in `RetryConfig`, `ErrorParser`)
- Stream sentinels → `StreamDone` constant (used in completions parser)

Every string literal in a code example earlier in this document (e.g.
`"Anthropic-Version"`, `"2023-06-01"`, `"message_start"`) becomes a named
constant in the final implementation.

### Retry Transport (`apicore/retry.go`)

Retry is handled at the `http.RoundTripper` level — composable, standard Go
pattern, transparent to the client. apicore provides a `RetryTransport` helper.

```go
package apicore

// RetryConfig controls retry behavior for transient HTTP errors.
type RetryConfig struct {
    MaxRetries        int           // default: 2
    RetryableStatuses []int         // default: DefaultRetryableStatuses
    InitialBackoff    time.Duration // default: 1s
    MaxBackoff        time.Duration // default: 60s
    Logger            *slog.Logger  // optional: logs retry attempts
}

// DefaultRetryableStatuses are the HTTP status codes that trigger a retry.
var DefaultRetryableStatuses = []int{
    StatusTooManyRequests,     // 429
    StatusInternalServerError, // 500
    StatusBadGateway,          // 502
    StatusServiceUnavailable,  // 503
    StatusOverloaded,          // 529
}

// NewRetryTransport wraps a base RoundTripper with retry logic.
// On retryable status codes:
//   1. Reads Retry-After header (seconds or HTTP-date)
//   2. If no Retry-After, uses exponential backoff with jitter
//   3. Waits (respecting context cancellation)
//   4. Retries up to MaxRetries times
//   5. Returns the last response if all retries exhausted
func NewRetryTransport(base http.RoundTripper, cfg RetryConfig) http.RoundTripper
```

Providers compose it via `WithHTTPClient`:

```go
client := messages.NewClient(
    messages.WithBaseURL("https://api.anthropic.com"),
    messages.WithHTTPClient(&http.Client{
        Transport: apicore.NewRetryTransport(http.DefaultTransport, apicore.RetryConfig{
            MaxRetries: 3,
            Logger:     logger,
        }),
    }),
    messages.WithResponseHook(func(req *messages.Request, meta apicore.ResponseMeta) {
        // This runs AFTER retry succeeds (or all retries exhausted).
        // Only the final response's headers are visible here.
        p.updateRateLimits(req.Model, meta.Headers)
    }),
    messages.WithLogger(logger),
)
```

**Why a transport wrapper, not built into `Stream()`:**
- **Standard Go pattern** — `http.RoundTripper` is the idiomatic composition point
- **Transparent** — `Stream()` doesn't know about retry; it just gets the final response
- **Composable** — providers can stack multiple transports (retry + tracing + metrics)
- **Testable** — `RetryTransport` can be tested independently with `RoundTripFunc`
- **Reusable** — same transport works for non-streaming requests too (e.g. `CountTokensAPI`)

### Logging

`WithLogger` accepts `*slog.Logger` (Go 1.21+ stdlib). When configured:

| What | Level | Fields |
|---|---|---|
| Request sent | `Info` | `method`, `url`, `content_length` |
| Response received | `Info` | `status`, `latency_ms`, `request_id` |
| Non-2xx error | `Warn` | `status`, `error`, `retry_after` |
| SSE event received | `Debug` | `event_name`, `data_size` |
| Stream completed | `Info` | `events_count`, `duration_ms` |
| ParseHook emitted | `Debug` | `event_type` |

When no logger is configured (the default), zero overhead — no allocations,
no string formatting. `RetryTransport` accepts its own logger for retry-specific
logging independent of the client.

---

## Layer 3: Adapter (Bridge to `llm`)

The adapter is the **only place** that imports `github.com/codewandler/llm`.
All `llm.Publisher` event publishing — `Started`, `Delta`, `ToolCall`, `Completed`,
`UsageRecord`, `RequestEvent`, `Error` — happens here.

### Shared Adapter Config

Common adapter settings (provider identity) are shared in `apicore` to avoid
duplication. Cost calculation stays protocol-specific since each adapter builds
usage records differently.

```go
// apicore/adapter.go

// AdapterConfig holds identity settings shared across all protocol adapters.
type AdapterConfig struct {
    ProviderName     string
    UpstreamProvider string
}

type AdapterOption func(*AdapterConfig)

func WithProviderName(name string) AdapterOption     { ... }
func WithUpstreamProvider(name string) AdapterOption  { ... }
```

Protocol-specific adapters embed `AdapterConfig` and add their own options
(cost calculator, thinking mode, cache retention, etc.).

### Creation Pattern

`NewAdapter` is a **standalone function**, not a method on `Client`.
(Go doesn't allow adding methods to type aliases, and `Client` is an alias
for `apicore.Client[Request]`.)

```go
// api/messages/adapter.go

// Streamer sends a streaming request and returns native typed events.
// *Client implements this. Tests can provide a fake.
type Streamer interface {
    Stream(ctx context.Context, req *Request) (*apicore.StreamHandle, error)
}

type Adapter struct {
    sender Streamer  // *Client in production, fake in tests
    cfg    apicore.AdapterConfig
    // protocol-specific settings:
    thinkingMode ThinkingMode
    thinkingBudgetLo, thinkingBudgetHi int
    outputEffort string
    userID       string
    costCalc     CostCalculator
}

// MessagesOption configures messages-specific adapter behavior.
type MessagesOption func(*Adapter)

func WithThinkingMode(mode ThinkingMode) MessagesOption    { ... }
func WithThinkingBudget(lo, hi int) MessagesOption         { ... }
func WithOutputEffort(effort string) MessagesOption        { ... }
func WithUserID(id string) MessagesOption                  { ... }
func WithCostCalculator(cc CostCalculator) MessagesOption  { ... }

// NewAdapter creates a Messages API adapter.
// Accepts Streamer (typically *Client, but any implementation works for testing).
func NewAdapter(sender Streamer, base []apicore.AdapterOption, opts ...MessagesOption) *Adapter {
    cfg := apicore.ApplyAdapterOptions(base...)
    a := &Adapter{sender: sender, cfg: cfg}
    for _, opt := range opts {
        opt(a)
    }
    return a
}
```

Provider usage:
```go
// Provider creates client once, then creates adapter from it:
adapter := messages.NewAdapter(client,
    []apicore.AdapterOption{
        apicore.WithProviderName("anthropic"),
    },
    messages.WithThinkingMode(messages.ThinkingAdaptive),
    messages.WithCostCalculator(usage.Default()),
)

// Provider's CreateStream delegates to the adapter:
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
    opts, err := src.BuildRequest(ctx)
    ...
    pub, ch := llm.NewEventPublisher()
    go func() { p.adapter.StreamTo(ctx, opts, pub) }()
    return ch, nil
}
```

### `StreamTo` — The Main Entry Point

```go
// StreamTo converts an llm.Request to a wire request, sends it via the client,
// and publishes llm events to the publisher. Blocks until the stream ends.
// Takes ownership of pub and calls pub.Close() when done.
func (a *Adapter) StreamTo(ctx context.Context, req llm.Request, pub llm.Publisher) error {
    defer pub.Close()

    // 1. Convert llm.Request → wire Request (via convert.go + adapter options)
    wireReq, err := a.convertRequest(req)

    // 2. Send via sender, get native event stream + HTTP metadata
    handle, err := a.sender.Stream(ctx, wireReq)

    // 3. Publish RequestEvent (before processing any response events)
    pub.Publish(&llm.RequestEvent{
        OriginalRequest: req,
        ProviderRequest: llm.ProviderRequestFromHTTP(handle.Request, ...),
        ResolvedApiType: llm.ApiTypeAnthropicMessages,
    })

    // 4. Process native events → llm events (tool accumulation, usage, etc.)
    for result := range handle.Events {
        a.processEvent(result, pub, handle.Headers)
    }

    return nil
}
```

### `convert.go` — `llm.Request` → Wire Request

```go
// api/messages/convert.go

type ConvertOption func(*convertConfig)

func ConvertThinkingMode(mode ThinkingMode) ConvertOption
func ConvertThinkingBudget(lo, hi int) ConvertOption
func ConvertOutputEffort(effort string) ConvertOption
func ConvertUserID(id string) ConvertOption
func ConvertCacheRetention(ttl string) ConvertOption

// RequestFromLLM converts an llm.Request to a Messages API Request.
func RequestFromLLM(req llm.Request, opts ...ConvertOption) (*Request, error) { ... }
```

**Model-specific logic**: Today, `BuildRequest` calls `isAdaptiveThinkingSupported(model)`
to decide thinking mode. This is model knowledge that doesn't belong in the API package.

The provider passes the decision as an option:

```go
// In provider/anthropic — provider knows its models:
wireReq, err := messages.RequestFromLLM(opts,
    messages.ConvertThinkingMode(messages.ThinkingAdaptive),
    messages.ConvertOutputEffort("medium"),
)
```

> **NOTE**: `api/messages/` may temporarily contain helpers like
> `DefaultThinkingMode(model string) ThinkingMode` as a migration aid. These
> are explicitly marked for removal when `model.Registry` ships.

---

## OpenRouter: Standard APIs, Not Proprietary Completions

### Current State (the problem)

OpenRouter's `provider/openrouter` currently uses three API paths:
1. **Chat Completions** — their original proprietary API. This is NOT standard
   OpenAI Chat Completions; it has extra fields (`reasoning_content`,
   `reasoning_details`, `cost` in usage, `cache_write_tokens`). Most traffic goes here.
2. **Responses API** — standard OpenAI Responses format. Only for `openai/*` models
   that require it.
3. **Messages API** — standard Anthropic Messages format. Only for `anthropic/*` models.

The proprietary Chat Completions path duplicates 742 lines of request building + stream
parsing that can't be shared with `provider/openai` because the formats differ.

### New State (the solution)

OpenRouter now offers standard **Responses API** and **Messages API** as first-class
endpoints. These follow the standard wire formats with provider-specific fields
added at the request level (not in SSE events).

**New dispatch rules:**
- `anthropic/*` models → `api/messages` client
- Everything else → `api/responses` client (universal endpoint for all models)

The proprietary Chat Completions path is **deprecated** — no new features will be
built on it. It may be kept briefly as a legacy fallback during migration but is
not extracted into `api/`.

### OpenRouter's Non-Inference APIs

OpenRouter has non-inference APIs (credits, models, generations, analytics, etc.)
that are proprietary to OpenRouter. These stay in `provider/openrouter/` as a
native client:

```go
// provider/openrouter/client.go — OpenRouter-specific resource APIs
type OpenRouterClient struct {
    baseURL    string
    httpClient *http.Client
    apiKey     string
}

func (c *OpenRouterClient) GetCredits(ctx context.Context) (*Credits, error) { ... }
func (c *OpenRouterClient) ListModels(ctx context.Context) ([]Model, error) { ... }
func (c *OpenRouterClient) GetGeneration(ctx context.Context, id string) (*Generation, error) { ... }
```

### Provider-Specific Fields via Transform

OpenRouter adds fields to standard API requests (`provider`, `models`, `session_id`,
`plugins`, `trace`). These are injected via `WithTransform`:

```go
// In provider/openrouter — for the Responses API path:
respClient := responses.NewClient(
    responses.WithBaseURL("https://openrouter.ai/api"),
    responses.WithHeaderFunc(orAuthHeaders(p)),
    responses.WithTransform(func(req *responses.Request) any {
        return struct {
            *responses.Request
            Provider  *orProviderConfig `json:"provider,omitempty"`
            Models    []string          `json:"models,omitempty"`
            SessionID string            `json:"session_id,omitempty"`
        }{
            Request:   req,
            Provider:  p.routingConfig(),
            SessionID: p.sessionID,
        }
    }),
)
```

### Cost Handling

OpenRouter's **old Chat Completions** API reported `cost` in the usage chunk of the
SSE stream. For their standard Responses and Messages APIs, cost information may or
may not be present in the SSE stream — if not, it's available via the
`/api/v1/generations/{id}` endpoint after the request completes.

The `ParseHook` mechanism handles both cases cleanly:
- If cost/usage data appears in SSE events, the provider's `ParseHook` extracts it
  into a typed event emitted on the stream. The adapter handles it in its type switch.
- If cost is only available post-request, the provider queries it separately
  and correlates via the generation/request ID from `StreamStartedEvent`.
- Providers without special cost reporting simply don't configure a hook.

---

## Ollama: Migrate from Proprietary `/api/chat` to Standard APIs

### Current State

`provider/ollama` uses Ollama's proprietary `/api/chat` endpoint with its own
request types (non-standard field names like `num_predict` instead of `max_tokens`),
its own NDJSON stream format (not SSE), and its own stream parser — ~235 lines of
bespoke wire code.

### New State

Ollama (v0.13.3+) now supports all three standard APIs:
- `/v1/chat/completions` — OpenAI Chat Completions (stable, longest support)
- `/v1/responses` — OpenAI Responses API (new)
- `/v1/messages` — Anthropic Messages API (new, with thinking + tools)

`provider/ollama` can migrate to use `api/completions` (or `api/messages` for
thinking-capable models), deleting all bespoke wire code.

**Recommended path**: `api/completions` as the primary API — it's the most mature
Ollama compat endpoint and matches what Docker Model Runner uses too. The Messages
API path can be added later for enhanced thinking support.

```go
// provider/ollama — after migration:
client := completions.NewClient(
    completions.WithBaseURL("http://localhost:11434"),
    completions.WithPath("/v1/chat/completions"),
    completions.WithHTTPClient(p.client),
    // No auth needed — Ollama is local, no headers required
)
```

### What Gets Deleted

- `type request struct` and all Ollama-specific request types (~40 lines)
- `func buildRequest()` (~80 lines)
- `type streamChunk struct` and all Ollama-specific response types (~20 lines)
- `func parseStream()` and NDJSON parsing logic (~100 lines)

Total: ~240 lines of bespoke wire code replaced by `api/completions`.

### What Stays

- `Provider`, `Models()`, `Resolve()`, `CreateStream()`, model list
- `FetchModels()` (queries `/api/tags` — Ollama-proprietary, not inference)
- `Available()` probe logic
- `CountTokens()` (BPE-based, not API-dependent)

---

## What Moves Where

### From `provider/anthropic/` → `api/messages/`

| Source file | What moves | Target |
|---|---|---|
| `request.go` | `Request`, `ThinkingConfig`, `ToolDefinition`, `OutputConfig`, `Metadata` | `types.go` |
| `request.go` | `BuildRequest()`, `BuildRequestBytes()` → becomes `RequestFromLLM()` | `convert.go` |
| `request.go` | `isAdaptiveThinkingSupported()`, `isEffortSupported()` | temp helper (marked for removal) |
| `message.go` | `Message`, `MessageContent`, `TextBlock`, `ToolUseBlock`, etc. | `types.go` |
| `message.go` | `convertMessages()` | `convert.go` |
| `cache.go` | `CacheControl`, `buildCacheControl()` | `types.go` / `convert.go` |
| `event.go` | All SSE event structs | `types.go` |
| `stream_processor.go` | SSE parsing logic → parser; llm event mapping → adapter | `parser.go` + `adapter.go` |
| `stream.go` | `ParseStream()`, `ParseStreamWith()` → replaced by `Client.Stream()` + `Adapter.StreamTo()` | `client.go` + `adapter.go` |
| `anthropic.go` | `AnthropicVersion`, `BetaInterleavedThinking` constants + all header name constants | `constants.go` |
| `count_tokens_api.go` | Stays in `provider/anthropic/` (provider-specific endpoint) | — |

### From `provider/openai/` → `api/completions/`

| Source file | What moves | Target |
|---|---|---|
| `api_completions.go` | `ccRequest` → `Request`, `ccMessagePayload` → `Message`, etc. | `types.go` |
| `api_completions.go` | `ccBuildRequest()` → `RequestFromLLM()` | `convert.go` |
| `api_completions.go` | `ccStreamChunk` → `StreamChunk`, `ccParseStream()` | `types.go` + `parser.go` |
| `api_completions.go` | `ccEmitToolCalls()` → adapter | `adapter.go` |
| `usage.go` | `buildUsageTokenItems()` | `adapter.go` (usage building is adapter concern) |

### From `provider/openai/` → `api/responses/`

| Source file | What moves | Target |
|---|---|---|
| `api_responses.go` | `respRequest` → `Request`, `respInput` → `Input`, etc. | `types.go` |
| `api_responses.go` | `respBuildRequest()` → `RequestFromLLM()` | `convert.go` |
| `api_responses.go` | All `resp*` SSE event types | `types.go` |
| `api_responses.go` | `RespParseStream()`, `respHandleEvent()` | `parser.go` + `adapter.go` |

### What stays in `provider/<name>/`

| Provider | Keeps |
|---|---|
| `provider/anthropic/` | `Provider`, `Models()`, `Resolve()`, `CreateStream()`, `CountTokensAPI()`, model-specific decisions → creates `api/messages.Client` + `Adapter` |
| `provider/openai/` | `Provider`, model registry (`models.go`), `enrichOpts()`, API dispatch (completions vs responses), Codex logic → creates `api/completions` + `api/responses` clients + adapters |
| `provider/openrouter/` | `Provider`, `selectAPI()` (simplified: messages vs responses), model normalization, native client for credits/models/generations → creates `api/messages` + `api/responses` clients + adapters |
| `provider/minimax/` | `Provider`, `adjustThinkingForMiniMax()` (now a `WithTransform`) → creates `api/messages.Client` + `Adapter` |
| `provider/dockermr/` | Unchanged — wraps `provider/openai` (no direct wire-level calls) |
| `provider/ollama/` | `Provider`, `Models()`, `FetchModels()` (`/api/tags`), `Available()`, `CountTokens()` → creates `api/completions.Client` + `Adapter`, deletes ~240 lines of bespoke wire code |
| `provider/bedrock/` | Unchanged — uses AWS SDK, proprietary protocol |

---

## Dependency Graph (after extraction)

```
api/apicore/           ← depends on: internal/sse (no llm or usage dependency!)
api/messages/          ← depends on: api/apicore, llm (adapter only)
api/completions/       ← depends on: api/apicore, llm (adapter only)
api/responses/         ← depends on: api/apicore, llm (adapter only)

provider/anthropic/    ← depends on: llm, api/messages
provider/openai/       ← depends on: llm, api/completions, api/responses
provider/openrouter/   ← depends on: llm, api/responses, api/messages
provider/minimax/      ← depends on: llm, api/messages
provider/ollama/       ← depends on: llm, api/completions
provider/dockermr/     ← depends on: llm, provider/openai  (unchanged, wraps provider)
provider/bedrock/      ← depends on: llm  (unchanged, AWS SDK)
```

**No provider → provider imports** (except dockermr wrapping openai, which is intentional).

---

## Duplication Eliminated

| Today | After |
|---|---|
| OpenRouter duplicates Chat Completions request builder (475 lines) | Deleted — uses `api/responses` instead |
| OpenRouter duplicates Chat Completions stream parser (170 lines) | Deleted — uses `api/responses` instead |
| OpenRouter duplicates Responses request builder (84 lines) | Uses `responsesapi.RequestFromLLM()` |
| OpenRouter duplicates Responses types (`orRespRequest`, etc.) | Uses `responsesapi.Request` + `WithTransform` |
| MiniMax imports `provider/anthropic.BuildRequest` | Imports `messagesapi.RequestFromLLM()` |
| MiniMax imports `provider/anthropic.ParseStreamWith` | Uses `messagesapi.NewAdapter(client).StreamTo()` |
| OpenRouter imports `provider/anthropic.ParseStreamWith` | Uses `messagesapi.NewAdapter(client).StreamTo()` |
| OpenRouter imports `provider/openai.RespParseStream` | Uses `responsesapi.NewAdapter(client).StreamTo()` |
| `mapOpenAIFinishReason()` duplicated across providers | Defined once in relevant `api/` package |
| Ollama has bespoke `/api/chat` request builder (~80 lines) | Uses `completionsapi.RequestFromLLM()` |
| Ollama has bespoke NDJSON stream parser (~100 lines) | Uses `completionsapi.NewAdapter(client).StreamTo()` |

---

## Migration Strategy

### Phase 0: Create `api/apicore` (shared generic client)

1. Create `api/apicore/` with `Client[Req]`, `StreamHandle`, `StreamResult`,
   `ClientOption[Req]`, `ParserFactory`, `EventHandler`, `ParseHook`,
   `HeaderFunc`, `TransformFunc[Req]`, `ErrorParser`, `HTTPError`,
   `AdapterConfig`, `AdapterOption`
2. Unit tests for the generic client:
   - HTTP request building (base URL + path, headers, body serialization)
   - HeaderFunc merging (static + dynamic)
   - TransformFunc application (modify in place, wrap with extra fields)
   - Non-2xx error handling (default HTTPError, custom ErrorParser)
   - SSE scanning → EventHandler dispatch → channel output
   - ParseHook integration (standalone events after standard events)
   - Context cancellation, goroutine cleanup
3. Test with a simple mock parser (e.g. `func() EventHandler { return ... }`)
4. No provider changes yet — just the foundation

### Phase 1: Extract `api/messages` (first, largest impact)

1. Create `api/messages/` with types, parser, client (thin wrapper), adapter, convert
2. Migrate `provider/anthropic` to use `api/messages`
3. Migrate `provider/minimax` — drops `provider/anthropic` import entirely
4. Migrate `provider/openrouter` messages path — drops `provider/anthropic` import
5. All existing tests must pass

### Phase 2: Extract `api/responses`

1. Create `api/responses/` with types, parser, client, adapter, convert
2. Migrate `provider/openai` responses path
3. Migrate `provider/openrouter` responses path — deletes duplicated Responses types
4. All existing tests must pass

### Phase 3: Extract `api/completions`

1. Create `api/completions/` with types, parser, client, adapter, convert
2. Migrate `provider/openai` completions path
3. Migrate `provider/ollama` — drops proprietary `/api/chat`, uses `/v1/chat/completions`
   via `api/completions`. Deletes ~240 lines of bespoke wire code.
4. All existing tests must pass

### Phase 4: OpenRouter — drop proprietary Chat Completions

1. Simplify `selectAPI`: only `orMessages` and `orResponses`
2. Delete old `buildRequest()`, `parseStream()`, and all proprietary types from
   `provider/openrouter/openrouter.go` (~500 lines)
3. Move non-inference APIs to `provider/openrouter/client.go`
4. Update tests to verify Messages + Responses paths

### Phase 5: Cleanup

1. Delete dead code from `provider/anthropic` (old `ParseStream`, `ParseStreamWith`, `BuildRequest`)
2. Delete dead code from `provider/openai` (old `ccBuildRequest`, `ccParseStream`, etc.)
3. Run `go vet`, verify no `provider/X` imports `provider/Y` (except dockermr)

Each phase is a self-contained PR. Tests must pass after every phase.

---

## Test Strategy

Each layer has a clean testing seam. No layer requires spinning up real HTTP
servers or importing things it shouldn't.

### Test Helpers (`apicore/testing.go`)

Shared utilities for testing all layers:

```go
package apicore

// RoundTripFunc adapts an ordinary function to http.RoundTripper.
// Use with WithHTTPClient to intercept HTTP in client tests.
type RoundTripFunc func(*http.Request) (*http.Response, error)
func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// FixedSSEResponse returns a RoundTripFunc that always returns the given
// status code and SSE body. Use for full client → parser integration tests.
func FixedSSEResponse(statusCode int, sseBody string) RoundTripFunc {
    return func(req *http.Request) (*http.Response, error) {
        return &http.Response{
            StatusCode: statusCode,
            Header:     http.Header{"Content-Type": {"text/event-stream"}},
            Body:       io.NopCloser(strings.NewReader(sseBody)),
        }, nil
    }
}

// NewTestHandle creates a StreamHandle from canned events.
// Use for adapter tests that bypass HTTP entirely.
func NewTestHandle(events ...StreamResult) *StreamHandle {
    ch := make(chan StreamResult, len(events))
    for _, e := range events {
        ch <- e
    }
    close(ch)
    return &StreamHandle{
        Events:  ch,
        Request: httptest.NewRequest("POST", "/test", nil),
        Headers: http.Header{},
    }
}
```

### Layer 1: Wire Types — Pure JSON Tests

No mocking needed. Test JSON conformance against API spec:

```go
func TestRequest_MarshalJSON(t *testing.T) {
    req := messages.Request{
        Model:    "claude-sonnet-4-5",
        MaxTokens: 1024,
        Messages: []messages.Message{{Role: "user", Content: "Hello"}},
        Thinking: &messages.ThinkingConfig{Type: "adaptive"},
    }
    data, err := json.Marshal(req)
    require.NoError(t, err)
    assert.Contains(t, string(data), `"thinking":{"type":"adaptive"}`)
}

func TestStreamChunk_UnmarshalJSON(t *testing.T) {
    // Real API fixture
    raw := `{"id":"chatcmpl-abc","model":"gpt-4o","choices":[{"delta":{"content":"Hi"}}]}`
    var chunk completions.StreamChunk
    require.NoError(t, json.Unmarshal([]byte(raw), &chunk))
    assert.Equal(t, "Hi", chunk.Choices[0].Delta.Content)
}
```

Tests:
- Marshal → verify matches API spec examples
- Unmarshal → feed recorded API responses, verify struct fields
- Round-trip (marshal → unmarshal → equal)
- `omitempty`: absent optional fields produce clean JSON
- Edge cases: nil pointers, empty slices, zero values

### Layer 2a: `apicore.Client[Req]` — RoundTripper Injection

Test the generic client via `http.RoundTripper` injection. No real HTTP:

```go
func TestClient_Stream_MergesHeaders(t *testing.T) {
    var captured *http.Request
    client := apicore.NewClient[testReq](testParserFactory,
        apicore.WithBaseURL[testReq]("https://api.example.com"),
        apicore.WithPath[testReq]("/v1/test"),
        apicore.WithHeader[testReq]("X-Static", "static-val"),
        apicore.WithHeaderFunc[testReq](func(ctx context.Context, req *testReq) (http.Header, error) {
            h := http.Header{}
            h.Set("Authorization", "Bearer dynamic-key")
            return h, nil
        }),
        apicore.WithHTTPClient[testReq](&http.Client{
            Transport: apicore.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
                captured = req
                return &http.Response{
                    StatusCode: 200,
                    Body:       io.NopCloser(strings.NewReader("data: {}\n\n")),
                }, nil
            }),
        }),
    )

    handle, err := client.Stream(ctx, &testReq{Field: "value"})
    require.NoError(t, err)
    for range handle.Events {} // drain

    assert.Equal(t, "static-val", captured.Header.Get("X-Static"))
    assert.Equal(t, "Bearer dynamic-key", captured.Header.Get("Authorization"))
    assert.Equal(t, "https://api.example.com/v1/test", captured.URL.String())
}

func TestClient_Stream_AppliesTransform(t *testing.T) {
    var capturedBody []byte
    client := apicore.NewClient[testReq](testParserFactory,
        apicore.WithHTTPClient[testReq](captureBodyTransport(&capturedBody, 200, "data: {}\n\n")),
        apicore.WithTransform[testReq](func(req *testReq) any {
            return struct {
                *testReq
                Extra string `json:"extra"`
            }{testReq: req, Extra: "injected"}
        }),
    )

    client.Stream(ctx, &testReq{Field: "value"})
    assert.Contains(t, string(capturedBody), `"extra":"injected"`)
}

func TestClient_Stream_Non2xx_ReturnsError(t *testing.T) {
    client := apicore.NewClient[testReq](testParserFactory,
        apicore.WithHTTPClient[testReq](&http.Client{
            Transport: apicore.FixedSSEResponse(429, `{"error":"rate limited"}`),
        }),
    )

    _, err := client.Stream(ctx, &testReq{})
    require.Error(t, err)

    var httpErr *apicore.HTTPError
    require.ErrorAs(t, err, &httpErr)
    assert.Equal(t, 429, httpErr.StatusCode)
}

func TestClient_Stream_ParseHookEmitsStandaloneEvents(t *testing.T) {
    type extraEvent struct{ Value string }

    client := apicore.NewClient[testReq](testParserFactory,
        apicore.WithHTTPClient[testReq](&http.Client{
            Transport: apicore.FixedSSEResponse(200, "event: test\ndata: {\"v\":\"hello\"}\n\n"),
        }),
        apicore.WithParseHook[testReq](func(name string, data []byte) any {
            return &extraEvent{Value: "from-hook"}
        }),
    )

    handle, _ := client.Stream(ctx, &testReq{})
    var events []apicore.StreamResult
    for e := range handle.Events {
        events = append(events, e)
    }

    // Standard event + hook event
    require.Len(t, events, 2)
    assert.IsType(t, &extraEvent{}, events[1].Event)
}
```

Tests:
- URL construction (baseURL + path)
- Header merging (static + dynamic)
- TransformFunc (modify-in-place, wrapper struct)
- JSON body serialization
- Non-2xx → error (default HTTPError, custom ErrorParser)
- SSE dispatch → EventHandler → channel
- ParseHook → standalone events after standard events
- Context cancellation → goroutine cleanup
- Empty body, malformed SSE, connection drops

### Layer 2b: Protocol Parsers (EventHandler) — Pure Function Tests

The `EventHandler` is a pure `(name, data) → StreamResult` function.
No HTTP, no channels, no mocking:

```go
func TestMessagesParser_ContentBlockDelta(t *testing.T) {
    handler := newParser()

    result := handler(EventContentBlockDelta, []byte(`{
        "type": "content_block_delta",
        "index": 0,
        "delta": {"type": "text_delta", "text": "Hello"}
    }`))

    require.NotNil(t, result.Event)
    evt, ok := result.Event.(*ContentBlockDeltaEvent)
    require.True(t, ok)
    assert.Equal(t, "Hello", evt.Delta.Text)
    assert.False(t, result.Done)
}

func TestMessagesParser_ToolCallAccumulation(t *testing.T) {
    handler := newParser()

    // Feed sequence that builds up a tool call
    handler(EventContentBlockStart, toolStartJSON)
    handler(EventContentBlockDelta, toolArgFragment1)
    handler(EventContentBlockDelta, toolArgFragment2)
    result := handler(EventContentBlockStop, toolStopJSON)

    // Verify accumulated tool call has complete arguments
    // ...
}

func TestMessagesParser_ErrorEvent(t *testing.T) {
    handler := newParser()
    result := handler(EventError, []byte(`{"type":"error","error":{"type":"overloaded","message":"server busy"}}`))
    require.Error(t, result.Err)
}

func TestCompletionsParser_DoneSignal(t *testing.T) {
    handler := newCompletionsParser()
    result := handler("", []byte("[DONE]"))
    assert.True(t, result.Done)
}
```

Tests per parser:
- Every SSE event type → correct typed struct
- Stateful accumulation (tool call arguments, block indices)
- Terminal events → `Done: true`
- Error events → `Err` set
- Malformed JSON → graceful error, not panic
- Unknown event types → ignored or logged

### Layer 3a: Convert (`llm.Request` → Wire Request) — Pure Function Tests

```go
func TestRequestFromLLM_ThinkingAdaptive(t *testing.T) {
    llmReq := llm.Request{
        Model:    "claude-sonnet-4-5",
        Messages: []llm.Message{{Role: "user", Content: "Hello"}},
    }

    wireReq, err := messages.RequestFromLLM(llmReq,
        messages.ConvertThinkingMode(messages.ThinkingAdaptive),
    )
    require.NoError(t, err)
    assert.Equal(t, "adaptive", wireReq.Thinking.Type)
    assert.Equal(t, "claude-sonnet-4-5", wireReq.Model)
    assert.True(t, wireReq.Stream)
}
```

Tests:
- Each ConvertOption produces correct wire fields
- Message conversion (user, assistant, tool results, system)
- Tool definitions → wire tool format
- Cache control placement
- Edge cases: empty messages, no tools, no system prompt

### Layer 3b: Adapter — Fake Streamer, No HTTP

The adapter accepts `Streamer` (interface). Tests provide a fake
that returns canned `StreamHandle` via `apicore.NewTestHandle`:

```go
type fakeStreamer struct {
    handle *apicore.StreamHandle
    err    error
    // captured for assertion:
    gotReq *messages.Request
}

func (f *fakeStreamer) Stream(ctx context.Context, req *messages.Request) (*apicore.StreamHandle, error) {
    f.gotReq = req
    return f.handle, f.err
}

func TestAdapter_StreamTo_TextDeltas(t *testing.T) {
    fake := &fakeStreamer{
        handle: apicore.NewTestHandle(
            apicore.StreamResult{Event: &messages.MessageStartEvent{
                Message: messages.MessageStart{Model: "claude-sonnet-4-5"},
            }},
            apicore.StreamResult{Event: &messages.ContentBlockDeltaEvent{
                Delta: messages.Delta{Type: "text_delta", Text: "Hello"},
            }},
            apicore.StreamResult{Event: &messages.MessageStopEvent{}, Done: true},
        ),
    }

    adapter := messages.NewAdapter(fake,
        []apicore.AdapterOption{apicore.WithProviderName("test")},
    )

    pub := llmtest.NewRecordingPublisher() // from llmtest package
    err := adapter.StreamTo(ctx, llmReq, pub)
    require.NoError(t, err)

    // Verify llm events were published correctly
    require.Len(t, pub.Events, 3) // Started + Delta + Completed
    assert.Equal(t, llm.EventTypeStart, pub.Events[0].Type)
    assert.Equal(t, "Hello", pub.Events[1].Delta.Text)
}

func TestAdapter_StreamTo_ToolCalls(t *testing.T) { ... }
func TestAdapter_StreamTo_ErrorEvent(t *testing.T) { ... }
func TestAdapter_StreamTo_UsageRecord(t *testing.T) { ... }
func TestAdapter_StreamTo_ParseHookEvent(t *testing.T) { ... }
```

Tests:
- Text deltas → correct `llm.Publisher` events
- Tool calls → correct tool call events
- Error events → error published
- Usage/cost calculation → usage record published
- RequestEvent published before stream events
- ParseHook events (e.g. `*OpenRouterUsage`) handled correctly
- Stream error (HTTP failure) → error published, pub closed
- Context cancellation → graceful shutdown

### Integration Tests (full stack, per provider)

Test the complete path through a `RoundTripFunc` with recorded SSE fixtures:

```go
func TestAnthropicProvider_FullStream(t *testing.T) {
    sseFixture := loadFixture(t, "testdata/anthropic_stream.sse")

    client := messages.NewClient(
        messages.WithBaseURL("https://fake.api"),
        messages.WithHTTPClient(&http.Client{
            Transport: apicore.FixedSSEResponse(200, sseFixture),
        }),
    )

    adapter := messages.NewAdapter(client,
        []apicore.AdapterOption{apicore.WithProviderName("anthropic")},
        messages.WithThinkingMode(messages.ThinkingAdaptive),
    )

    pub := llmtest.NewRecordingPublisher()
    err := adapter.StreamTo(ctx, llmReq, pub)
    require.NoError(t, err)

    // Verify complete event sequence against golden expectations
    assert.Equal(t, llm.EventTypeStart, pub.Events[0].Type)
    // ... verify text, tool calls, usage, etc.
}
```

These use real SSE recordings from each API (stored in `testdata/`).
They verify the full pipeline: client → parser → adapter → llm events.

### Provider Tests (unchanged)

- Existing `provider/<name>/*_test.go` continue to pass
- They now exercise: provider → adapter → client → parser
- Migration is verified by test continuity, not rewrite

### Test Pyramid Summary

```
                    ┌───────────────────────┐
                    │  Provider tests     │  Existing tests, unchanged
                    │  (integration)      │  Full stack via RoundTripper
                    ├───────────────────────┤
                ┌───┤  Adapter tests       ├───┐
                │   │  (fake Streamer)     │   │  No HTTP, canned events
                │   ├───────────────────────┤   │
            ┌───┤   │  Convert tests       │   ├───┐
            │   │   │  (pure functions)    │   │   │  No HTTP, no channels
            │   │   ├───────────────────────┤   │   │
        ┌───┤   │   │  Parser tests        │   │   ├───┐
        │   │   │   │  (pure functions)    │   │   │   │  (name, data) → result
        │   │   │   ├───────────────────────┤   │   │   │
    ┌───┤   │   │   │  apicore tests       │   │   │   ├───┐
    │   │   │   │   │  (RoundTripper)      │   │   │   │   │  HTTP mocked
    │   │   │   │   ├───────────────────────┤   │   │   │   │
    │   │   │   │   │  Wire type tests     │   │   │   │   │  Pure JSON
    └───┴───┴───┴───└───────────────────────┘───┴───┴───┴───┘
```

| Layer | What's tested | Test seam | Mocking needed |
|---|---|---|---|
| Wire types | JSON conformance | None (pure structs) | None |
| Parser (EventHandler) | `(name, data) → StreamResult` | None (pure function) | None |
| apicore.Client | HTTP + SSE + hooks + errors | `http.RoundTripper` | `RoundTripFunc` |
| Convert | `llm.Request → *wire.Request` | None (pure function) | None |
| Adapter | native events → `llm.Publisher` | `Streamer` interface | `fakeStreamer` + `NewTestHandle` |
| Provider | Full pipeline | `http.RoundTripper` | `FixedSSEResponse` + SSE fixtures |

---

## Open Questions

1. **`internal/sse`** stays where it is. Go allows same-module access to `internal/`
   packages. `api/apicore` imports `internal/sse` — no move needed.

2. **Iterator vs channel for stream events?** Go 1.23 has `iter.Seq`. Worth
   evaluating but channels are consistent with the rest of the codebase.

3. **Phase ordering**: Phases 2 and 3 (responses vs completions) could be swapped.
   Responses is higher-impact (used by both openai and openrouter).

4. **Adapter in same package or sub-package?** Currently adapter lives in
   `api/messages/adapter.go` alongside types and parser. This means
   `api/messages/` depends on `llm`. If standalone (no-llm) usage of the
   wire client matters, adapter could move to `api/messages/adapter/`.
   Not a concern for now — split later if needed.
