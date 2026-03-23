# Changelog

## v0.24.2 (unreleased)

### Bug Fixes

#### Stop reason propagated through stream pipeline

`StreamEvent.Done.StopReason` is now correctly populated for all providers.
Previously the stop reason was parsed but not forwarded through the router's
pipe, so consumers always saw an empty `StopReason`.

### Chores

#### `llmcli infer` uses `ToolChoiceRequired` by default

The `llmcli infer` command now sets `ToolChoiceRequired` when tools are
provided, ensuring the model calls a tool rather than responding in plain text.

---

## v0.24.1

### Bug Fixes

#### Router — deterministic alias resolution in registration order

The router now walks providers in registration order (from `cfg.Providers` slice)
when building model indices and alias maps. This ensures that when multiple
providers share a bare alias like `"default"`, the first registered provider wins
— matching the documented auto-detect priority and making alias resolution
deterministic.

#### Auto provider — bedrock removal

Fixed duplicate registration logic; the auto provider no longer includes AWS
Bedrock unconditionally in the provider list when no explicit credentials are
available.

---

## v0.24.0

### New Features

#### `TokenCounter` — offline per-provider token estimation

All five providers now implement the optional `llm.TokenCounter` interface,
enabling callers to estimate input token usage before sending a request —
with no network call.

```go
if tc, ok := provider.(llm.TokenCounter); ok {
    count, err := tc.CountTokens(ctx, llm.TokenCountRequest{
        Model:    "claude-sonnet-4-5",
        Messages: messages,
        Tools:    tools,
    })
    if err == nil && count.InputTokens > maxTokens {
        return fmt.Errorf("request too large: %d tokens", count.InputTokens)
    }
}
```

**`TokenCountRequest`** — separate from `StreamRequest`; `Model` is required
(the provider uses it to select the correct BPE encoding).

**`TokenCount`** — full breakdown:

```go
type TokenCount struct {
    InputTokens      int            // grand total (messages + tools + overhead)
    PerMessage       []int          // [i] mirrors Messages[i], same index order
    SystemTokens     int            // Σ system messages
    UserTokens       int            // Σ user messages
    AssistantTokens  int            // Σ assistant messages
    ToolResultTokens int            // Σ tool result messages
    ToolsTokens      int            // raw tool definition tokens (sum of PerTool values)
    PerTool          map[string]int // per tool name → raw token count
    OverheadTokens   int            // provider-injected tokens the caller did not supply
}
```

**Provider accuracy:**

| Provider | Tokenizer | Notes |
|---|---|---|
| `openai` | tiktoken exact (`o200k_base` / `cl100k_base`) | +4/msg +3 reply-priming overhead |
| `openrouter` | tiktoken, model-prefix encoding detection | no per-message overhead |
| `anthropic` | `cl100k_base` approximation (±5-10%) | Anthropic's tokenizer is not public |
| `bedrock` | `cl100k_base` approximation (±5-10%) | no counting API on Bedrock |
| `ollama` | `cl100k_base` approximation (±10%) | no public tokenize endpoint |
| `claude` (OAuth) | `cl100k_base` + injected system blocks | includes billing/identity headers |

**Anthropic tool overhead compensation** — when tools are present, the
Anthropic, Bedrock, and Claude providers add the empirically-measured hidden
tool-use system preamble and per-tool framing to `OverheadTokens`:
- +330 tokens (preamble, once per request)
- +126 tokens (first tool serialisation framing)
- +85 tokens per additional tool

This reduces tool-heavy estimate drift from ~85% to ~3-8%.

**`OverheadTokens`** separates provider-injected tokens from caller-supplied
content. `ToolsTokens` is now purely the raw JSON token count of tool schemas
— `sum(PerTool) == ToolsTokens` holds for all providers. Anthropic preamble
and Claude OAuth system blocks go into `OverheadTokens` instead.

**`router.Provider`** implements `TokenCounter` by delegating to the first
resolved target's underlying provider with the native model ID. `auto.New`
gets `TokenCounter` for free since it returns `*router.Provider`.

**Convenience functions** for per-item counting (useful for context-budget
managers that count messages individually):

```go
n, err := llm.CountText("claude-sonnet-4-5", "some text")
n, err := llm.CountMessage("gpt-4o", msg)
```

**`tokencount` package** — shared offline BPE wrapper using
`github.com/pkoukk/tiktoken-go` with embedded offline loader (zero runtime
downloads). Exported for use by provider authors:

```go
enc, known := tokencount.EncodingForModel("gpt-4o") // "o200k_base", true
n, err    := tokencount.CountText(enc, "hello")
```

#### `StreamResult.Routed`

`StreamResult` now captures the `StreamEventRouted` event emitted by the
router. The `Routed` field exposes the selected backend provider, the
originally requested model alias, the resolved native model ID, and any
errors from targets that were tried and skipped before this one.

```go
result := <-llm.Process(ctx, stream).Result()
if r := result.Routed; r != nil {
    fmt.Println(r.Provider)       // "bedrock"
    fmt.Println(r.ModelRequested) // "fast"
    fmt.Println(r.ModelResolved)  // "anthropic.claude-haiku-4-5-20251001-v1:0"
}
```

#### `llmcli` verbose improvements

`llmcli infer -v` now shows two new sections:

**Token estimate** (printed before the request is sent):
```
── token estimate ──
 input (est): 684
      system: 32
        user: 1
       tools: 56
    add_fact: 28
complete_turn: 28
    overhead: 595
```

**Routing** (in the post-response metadata block):
```
routed_to: claude  fast → claude-haiku-4-5-20251001
```

**Drift** (appended to the `tokens` line):
```
tokens: 661 in, 142 out  (est 684 in, drift 3.5%)
```

**Prompt caching** — the system message is now sent with
`CacheHint{Enabled: true}` so caching activates automatically on providers
that support it once the system prompt exceeds the minimum token threshold.

---

### Bug Fixes

#### `Usage.InputTokens` contract enforced on Bedrock

`Usage.InputTokens` is defined as the total input tokens seen by the model,
including cache-read and cache-write tokens. The Anthropic provider always
honoured this. The Bedrock provider was mapping the raw wire field directly
(which is only the uncached remainder), causing `InputTokens` to show 340
instead of 2066 on a cached request.

Bedrock now adds `CacheReadTokens + CacheWriteTokens` to the wire value,
matching the Anthropic provider and the documented contract.

---

### Clarifications

#### `Usage` field semantics

| Field | Meaning |
|---|---|
| `InputTokens` | **Total** tokens seen by the model: uncached + cache-read + cache-write |
| `CacheReadTokens` | Subset of `InputTokens` served from an existing cache entry |
| `CacheWriteTokens` | Subset of `InputTokens` written to a new cache entry |
| `InputCost` | Cost of `InputTokens - CacheReadTokens - CacheWriteTokens` at regular rate |
| `CacheReadCost` | Cost of `CacheReadTokens` at the reduced cache-read rate |
| `CacheWriteCost` | Cost of `CacheWriteTokens` at the cache-write rate |

---



## v0.23.0

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
