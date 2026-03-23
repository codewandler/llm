# Changelog

## v0.23.0 (unreleased)

This is a large release with significant API changes. Several types and packages
were renamed or restructured. See the **Breaking Changes** and **Migration** sections.

---

### New Features

#### `StreamResponse` — client-side stream processor with typed tool dispatch

`Process(ctx, ch)` returns a `*StreamResponse` that accumulates a stream and
dispatches tool calls to registered handlers. Results are collected into
`*StreamResult` delivered on a channel from `.Result()`.

```go
result := <-llm.Process(ctx, ch).
    HandleTool(llm.Handle(weatherSpec, func(ctx context.Context, in WeatherParams) (*WeatherResult, error) {
        return fetchWeather(in.Location)
    })).
    HandleTool(llm.NewToolHandler("ping", func(ctx context.Context, in PingParams) (*PingResult, error) {
        return &PingResult{OK: true}, nil
    })).
    Result()
```

- `ToolDispatchSync` (default) — executes handlers sequentially in emission order
- `ToolDispatchAsync` — executes all handlers concurrently via `.DispatchAsync()`, collects results in order
- `BoundToolSpec[In, Out]` — binds a `ToolSpec` to a handler function via `llm.Handle(spec, fn)`
- `NewToolHandler[In, Out](name, fn)` — lightweight named handler without a spec
- `StreamResult` — final accumulated state: `Text`, `Reasoning`, `ToolCalls`, `ToolResults`, `Usage`, `StopReason`, `Start`
- Callback hooks: `OnStart(fn)`, `OnText(fn)`, `OnReasoning(fn)`, `OnToolDelta(fn)`

#### Structured `Delta` type replacing plain string deltas

Token content is now carried in a `*Delta` struct instead of the plain `Delta string`
and `Reasoning string` fields on `StreamEvent`.

```go
// Reading text tokens:
if ev.Type == llm.StreamEventDelta {
    print(ev.Text())          // DeltaTypeText content
    print(ev.ReasoningText()) // DeltaTypeReasoning content
}

// Or access the struct directly:
ev.Delta.Type      // DeltaTypeText | DeltaTypeReasoning
ev.Delta.Text
ev.Delta.Reasoning
ev.Delta.Index     // *uint32 block index, provider-dependent
```

Helper constructors: `TextDelta(idx, text)`, `ReasoningDelta(idx, text)`,
`ToolDelta(idx, id, name, argsFragment)`, `DeltaIndex(i)`.

#### `StreamEventReasoning` removed — reasoning now carried by `StreamEventDelta`

The dedicated `StreamEventReasoning` event type is gone. Reasoning tokens are
now `StreamEventDelta` events with `Delta.Type == DeltaTypeReasoning`.

#### New event types: `StreamEventCreated` and `StreamEventRouted`

- `StreamEventCreated` — emitted immediately when `NewEventStream()` is called,
  before any HTTP request is made.
- `StreamEventRouted` — emitted by the router provider when a backend has been
  selected. Carries `Routed{Provider, ModelRequested, ModelResolved, Errors}`.

#### Every event stamped with `RequestID`, `Seq`, and `Timestamp`

`EventStream` generates a nanoid `RequestID` once per `CreateStream` call and
stamps it on every emitted event. Events also carry a monotonic `Seq` counter
and wall-clock `Timestamp`.

#### `StreamStart` fields simplified

The `StreamStart` struct (carried by `StreamEventStart`) was simplified.
`RequestedModel`, `ResolvedModel`, and `ProviderModel` are replaced by a single
`Model` field (the model ID returned by the upstream API):

| v0.22 field | v0.23 field |
|---|---|
| `RequestedModel` | removed |
| `ResolvedModel` | removed |
| `ProviderModel` | `Model` |
| `RequestID` | `RequestID` (unchanged) |
| `TimeToFirstToken` | `TimeToFirstToken` (unchanged) |

#### Structured `ProviderError`

`StreamEvent.Error` changed from `error` to `*ProviderError`. All providers now
emit structured errors with a sentinel for `errors.Is` matching:

```go
if errors.Is(ev.Error, llm.ErrAPIError) { ... }
if errors.Is(ev.Error, llm.ErrContextCancelled) { ... }
```

Sentinels: `ErrContextCancelled`, `ErrRequestFailed`, `ErrAPIError`,
`ErrStreamRead`, `ErrStreamDecode`, `ErrProviderError`, `ErrMissingAPIKey`,
`ErrBuildRequest`, `ErrUnknownModel`, `ErrNoProviders`, `ErrUnknown`.

`ProviderError` fields: `Sentinel`, `Provider`, `Message`, `Cause`,
`StatusCode` (API errors only), `Body` (API errors only).

#### `llmtest` package

New `github.com/codewandler/llm/llmtest` package for testing stream consumers:

```go
ch := llmtest.SendEvents(
    llmtest.TextEvent("hello"),
    llmtest.ToolEvent("call_1", "get_weather", map[string]any{"location": "Berlin"}),
    llmtest.DoneEvent(nil),
)
```

Functions: `SendEvents`, `TextEvent`, `ReasoningEvent`, `ToolEvent`, `DoneEvent`, `ErrorEvent`.

#### `Messages` helpers

New mutating convenience methods on `Messages`:

```go
var msgs llm.Messages
msgs.AddSystemMsg("You are helpful.")
msgs.AddUserMsg("Hello")
msgs.AddAssistantMsg("Hi there")
msgs.AddToolCallResult(callID, output, false)
msgs.Append(msg)
```

#### `provider/auto` — zero-config multi-provider setup

`auto.New(ctx, ...Option)` auto-detects configured providers from environment
variables and returns a ready-to-use `llm.Provider`. Replaces the old
`provider.NewDefaultRegistry()` pattern.

```go
p, err := auto.New(ctx,
    auto.WithBedrock(),
    auto.WithOpenAI(),
    auto.WithOpenRouter(),
    auto.WithAnthropic(),
    auto.WithClaudeLocal(),
)
```

#### Model alias `codex` (OpenAI only)

New global alias `codex` resolves to the OpenAI Codex model. Existing aliases
`fast`, `default`, and `powerful` are unchanged.

#### OpenAI fixes

- Parallel tool calls are now emitted in LLM-production order (the order the
  model produced them), not hash-map insertion order.
- `StreamEventStart` is now correctly emitted for responses with no text or
  tool deltas (fires at `response.completed` instead of never).

---

### Breaking Changes

#### 1. `StreamOptions` → `StreamRequest`

```go
// Before
provider.CreateStream(ctx, llm.StreamOptions{Model: "...", Messages: msgs})

// After
provider.CreateStream(ctx, llm.StreamRequest{Model: "...", Messages: msgs})
```

#### 2. `StreamEvent.Delta` (string) and `StreamEvent.Reasoning` (string) removed

The plain string fields are gone. All delta content is now in `StreamEvent.Delta *Delta`.

```go
// Before
if ev.Type == llm.StreamEventDelta {
    print(ev.Delta)     // string
}
if ev.Type == llm.StreamEventReasoning {
    print(ev.Reasoning) // string
}

// After
if ev.Type == llm.StreamEventDelta {
    print(ev.Text())          // DeltaTypeText
    print(ev.ReasoningText()) // DeltaTypeReasoning
}
```

#### 3. `StreamEventReasoning` event type removed

`StreamEventReasoning` no longer exists. Reasoning tokens arrive as
`StreamEventDelta` with `ev.Delta.Type == llm.DeltaTypeReasoning`.

#### 4. `StreamEvent.Error` is now `*ProviderError`, not `error`

The field type changed from `error` to `*ProviderError`. It still satisfies the
`error` interface, so `.Error()` calls continue to work. Code that passes
`ev.Error` to a parameter typed `error` needs no change. Code that uses
`errors.As` to unwrap it should now use the sentinel constants.

```go
// Before
if ev.Type == llm.StreamEventError {
    log.Println(ev.Error.Error())
}

// After — same, plus new capabilities
if ev.Type == llm.StreamEventError {
    log.Println(ev.Error.Error())                    // unchanged
    log.Println(ev.Error.Provider)                   // which provider
    errors.Is(ev.Error, llm.ErrAPIError)             // sentinel matching
}
```

#### 5. `StreamStart` fields `RequestedModel`, `ResolvedModel`, `ProviderModel` changed

`RequestedModel` and `ResolvedModel` are removed. `ProviderModel` is renamed to `Model`.

```go
// Before
ev.Start.RequestedModel
ev.Start.ResolvedModel
ev.Start.ProviderModel

// After
ev.Start.Model  // replaces ProviderModel; RequestedModel and ResolvedModel are gone
```

#### 6. `Registry` and `MaybeRegister` removed

The `llm.Registry`, `llm.NewRegistry()`, `llm.RegisterFunc`, per-provider
`MaybeRegister` functions, and `provider.NewDefaultRegistry()` /
`provider.CreateStream()` are all removed.

```go
// Before
reg := provider.NewDefaultRegistry()
ch, err := reg.CreateStream(ctx, llm.StreamOptions{Model: "anthropic/claude-sonnet-4-5", ...})

// After
p, err := auto.New(ctx)
ch, err := p.CreateStream(ctx, llm.StreamRequest{Model: "anthropic/claude-sonnet-4-5", ...})
```

#### 7. `provider/aggregate` → `provider/router`

The package was renamed. The constructor signature (`New(cfg Config, factories map[string]Factory)`) is unchanged.

```go
// Before
import "github.com/codewandler/llm/provider/aggregate"
p, err := aggregate.New(cfg, factories)

// After
import "github.com/codewandler/llm/provider/router"
p, err := router.New(cfg, factories)
```

---

### Migration Guide

**Step 1 — Rename `StreamOptions` to `StreamRequest`**

Global find-and-replace: `llm.StreamOptions` → `llm.StreamRequest`.

**Step 2 — Update delta reading**

Replace `ev.Delta` (string access) with `ev.Text()`. Replace
`ev.Reasoning` (string) with `ev.ReasoningText()`. Remove any `case
llm.StreamEventReasoning` — reasoning now arrives as `StreamEventDelta` with
`ev.Delta.Type == llm.DeltaTypeReasoning`.

**Step 3 — Update `StreamStart` field references**

Replace `ev.Start.ProviderModel` with `ev.Start.Model`. Remove references to
`ev.Start.RequestedModel` and `ev.Start.ResolvedModel`.

**Step 4 — Update error handling**

`ev.Error` is `*ProviderError` — it satisfies `error`, so most call sites need
no change. Replace any string-based error matching with sentinel constants:
`llm.ErrAPIError`, `llm.ErrContextCancelled`, etc.

**Step 5 — Replace Registry with `auto.New`**

Remove all `provider.NewDefaultRegistry()`, `reg.RegisterAll(...)`, and
per-provider `MaybeRegister` calls. Replace with:

```go
import "github.com/codewandler/llm/provider/auto"

p, err := auto.New(ctx) // auto-detects from environment variables
```

Or pass explicit options (`auto.WithOpenAI()`, `auto.WithBedrock()`, etc.) for
full control.

**Step 6 — Rename `provider/aggregate` to `provider/router`**

Update the import path and rename `aggregate.New` to `router.New`.
