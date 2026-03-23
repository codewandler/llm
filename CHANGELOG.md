# Changelog

## v0.23.0 (unreleased)

This is a large release with significant API changes. Several types and packages
were renamed or restructured. See the **Migration** section below.

---

### New Features

#### `StreamResponse` — client-side stream processor with typed tool dispatch

`Process(ctx, ch)` returns a `*StreamResponse` that accumulates a stream and
dispatches tool calls to registered handlers. Tool results are collected into
`StreamResult` with correct ordering even when tools run concurrently.

```go
result, err := llm.Process(ctx, ch).
    HandleTool(llm.Handle(weatherSpec, func(ctx context.Context, in WeatherParams) (*WeatherResult, error) {
        return fetchWeather(in.Location)
    })).
    HandleTool(llm.NewToolHandler("ping", func(ctx context.Context, in PingParams) (*PingResult, error) {
        return &PingResult{OK: true}, nil
    })).
    Await()
```

- `ToolDispatchSync` (default) — executes handlers sequentially in emission order
- `ToolDispatchAsync` — executes all handlers concurrently, collects results in order
- `BoundToolSpec[In, Out]` — binds a `ToolSpec` to a handler function via `llm.Handle(spec, fn)`
- `NewToolHandler[In, Out](name, fn)` — lightweight handler without a spec
- `StreamResult` — final accumulated state: `Text`, `Reasoning`, `ToolCalls`, `ToolResults`, `Usage`, `StopReason`

#### Structured `Delta` type

Token deltas are now a first-class structured type instead of a plain string.

```go
// Before
event.Content  // string

// After
event.Delta.Type       // DeltaTypeText | DeltaTypeReasoning | DeltaTypeTool
event.Delta.Text       // text content
event.Delta.Reasoning  // reasoning/thinking content
event.Delta.Index      // *uint32 block index (provider-dependent)
event.Text()           // convenience method: returns Delta.Text or ""
event.ReasoningText()  // convenience method: returns Delta.Reasoning or ""
```

Helper constructors: `TextDelta(idx, text)`, `ReasoningDelta(idx, text)`, `ToolDelta(idx, id, name, argsFragment)`, `DeltaIndex(i)`.

#### Reasoning / extended thinking support

`DeltaTypeReasoning` deltas carry model reasoning tokens (Anthropic extended
thinking, OpenAI o-series, Bedrock). Accumulated into `StreamResult.Reasoning`.

#### `EventStream` helper

All providers now create streams via `NewEventStream()` which:
- Generates a unique `RequestID` (nanoid) for every stream
- Stamps every event with `RequestID`, monotonic `Seq`, and `Timestamp`
- Emits `StreamEventCreated` immediately on construction
- Provides typed send helpers: `Start()`, `Delta()`, `Reasoning()`, `ToolCall()`, `Done()`, `Error()`, `Routed()`

#### `StreamEventCreated` and `StreamEventRouted`

Two new event types:

- `StreamEventCreated` — emitted immediately when a stream is opened, before
  any request is sent. Carries `RequestID` and `Timestamp`.
- `StreamEventRouted` — emitted by the `router` provider when a request has
  been dispatched to a backend. Carries `Routed{Provider, ModelRequested, ModelResolved, Errors}`.

#### Structured `ProviderError`

Errors from all providers are now `*ProviderError` with a sentinel that works
with `errors.Is`:

```go
if errors.Is(err, llm.ErrAPIError) { ... }
if errors.Is(err, llm.ErrContextCancelled) { ... }
```

Sentinel constants: `ErrContextCancelled`, `ErrRequestFailed`, `ErrAPIError`,
`ErrStreamRead`, `ErrStreamDecode`, `ErrProviderError`, `ErrMissingAPIKey`,
`ErrBuildRequest`, `ErrUnknownModel`, `ErrNoProviders`, `ErrUnknown`.

Constructor helpers: `NewErrAPIError`, `NewErrContextCancelled`, `NewErrProviderMsg`, etc.

`StreamEvent.Error` is now `*ProviderError` instead of `error`.

#### `llmtest` package

New `github.com/codewandler/llm/llmtest` package for testing stream consumers,
following the `net/http/httptest` convention:

```go
ch := llmtest.SendEvents(
    llmtest.TextEvent("hello"),
    llmtest.ToolEvent("call_1", "get_weather", map[string]any{"location": "Berlin"}),
    llmtest.DoneEvent(nil),
)
```

Functions: `SendEvents`, `TextEvent`, `ReasoningEvent`, `ToolEvent`, `DoneEvent`, `ErrorEvent`.

#### `Messages` helpers

Convenience constructors on the `Messages` type:

```go
msgs := llm.Messages{}.
    WithSystem("You are helpful.").
    AppendUser("Hello").
    AppendAssistant("Hi there")
```

#### Router provider (renamed from `aggregate`)

The `provider/aggregate` package is now `provider/router`. It provides
failover routing across multiple backends and emits `StreamEventRouted` when
a backend is selected.

#### Model aliases

New model alias `codex` added alongside `fast`, `default`, and `powerful`.

#### `CacheHint` on `StreamRequest`

Top-level prompt caching hint. Behaviour is provider-specific:
- Anthropic: auto cache-control mode
- Bedrock: trailing `cachePoint` insertion
- OpenAI: extended cache retention

#### OpenAI fixes

- Parallel tool calls from OpenAI and OpenRouter are now emitted in
  LLM-production order (the order the model produced them), not insertion order.
- `StreamEventStart` is now correctly emitted for empty responses (no text/tool
  deltas) on the Responses API — it fires at `response.completed`.

---

### Breaking Changes

#### 1. Package `provider/aggregate` → `provider/router`

```go
// Before
import "github.com/codewandler/llm/provider/aggregate"
p := aggregate.New(...)

// After
import "github.com/codewandler/llm/provider/router"
p := router.New(...)
```

#### 2. `StreamOptions` → `StreamRequest`

The request type passed to `CreateStream` was renamed.

```go
// Before
provider.CreateStream(ctx, llm.StreamOptions{Model: "...", Messages: msgs})

// After
provider.CreateStream(ctx, llm.StreamRequest{Model: "...", Messages: msgs})
```

#### 3. `StreamEvent.Error` is now `*ProviderError`

The `Error` field changed from `error` to `*ProviderError`. Code that directly
accessed `.Error` must be updated.

```go
// Before
if ev.Type == llm.StreamEventError {
    log.Println(ev.Error.Error())
}

// After — same call, just a different underlying type
if ev.Type == llm.StreamEventError {
    log.Println(ev.Error.Error())         // still works
    log.Println(ev.Error.Provider)        // new: identify which provider errored
    errors.Is(ev.Error, llm.ErrAPIError)  // new: sentinel matching
}
```

#### 4. Token deltas are `*Delta`, not a string

`StreamEvent` no longer has a plain text field. Delta content is under `StreamEvent.Delta`.

```go
// Before
if ev.Type == llm.StreamEventDelta {
    print(ev.Content)
}

// After
if ev.Type == llm.StreamEventDelta {
    print(ev.Text())         // text tokens
    print(ev.ReasoningText()) // reasoning tokens
}
```

#### 5. Provider `Registry` removed

The global `Registry` and `MaybeRegister` call sites were removed. Construct
providers directly and pass them to `router.New(...)` or your own dispatch logic.

```go
// Before
llm.MaybeRegister(anthropic.New())
p, _ := llm.ResolveModel("anthropic/claude-sonnet-4-5")

// After
r := router.New(
    anthropic.New(),
    openai.New(),
)
ch, err := r.CreateStream(ctx, llm.StreamRequest{Model: "anthropic/claude-sonnet-4-5", ...})
```

#### 6. `llmtest.ErrorEvent` takes `*llm.ProviderError`

```go
// Before
llmtest.ErrorEvent("something went wrong")

// After
llmtest.ErrorEvent(llm.NewErrProviderMsg("test", "something went wrong"))
```

---

### Migration Guide

1. **Rename the import path** for the aggregate/router provider:
   `provider/aggregate` → `provider/router`, `aggregate.New` → `router.New`.

2. **Rename `StreamOptions` to `StreamRequest`** everywhere you construct a request.

3. **Update delta reading**: replace any `ev.Content` access with `ev.Text()` or
   `ev.Delta.Text` / `ev.Delta.Reasoning`. Switch on `ev.Delta.Type` if you need
   to distinguish text from reasoning.

4. **Update error handling**: `StreamEvent.Error` is now `*ProviderError`. The
   `.Error()` method still works. Use `errors.Is(ev.Error, llm.ErrAPIError)` etc.
   for typed matching instead of string checks.

5. **Remove Registry usage**: construct providers explicitly and wire them through
   `router.New(...)`.

6. **Update `llmtest.ErrorEvent` calls** to pass a `*llm.ProviderError` (e.g.
   `llm.NewErrProviderMsg("test", "msg")`) instead of a plain string.
