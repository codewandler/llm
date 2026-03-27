# Changelog

## v0.28.0

### Bug Fixes

#### Haiku thinking defaults
- Haiku now defaults to `thinking: {type: "enabled", budget_tokens: 31999}` instead of `disabled`
- This matches Claude Code's default behavior for agentic use cases

#### Max tokens default
- Default `max_tokens` changed from 16384 to 32000 across all Anthropic providers
- Matches Claude Code's default for better response capacity

#### Metadata user_id format
- `metadata.user_id` now uses JSON object format `{"device_id": "...", "account_uuid": "...", "session_id": "..."}`
- Previously used a flattened string format that didn't match Claude Code

#### Output effort only on supported models
- `output_config.effort` is now only sent for Sonnet 4.6, Opus 4.5, and Opus 4.6
- Sonnet 4.5 does not support effort and would return HTTP 400 if sent

### New Features

#### Adaptive thinking support
- Sonnet 4.6 and Opus 4.6 now default to `thinking: {type: "adaptive"}`
- `ThinkingEffort` values (low/medium/high) use adaptive on 4.6 models
- Older models use `enabled` mode with `budget_tokens` mapping

#### OutputEffort for response thoroughness
- New `OutputEffort` field in `llm.Request` controls response depth
- Values: `low`, `medium`, `high`, `max` (max only on Opus 4.6)
- Defaults to `medium` effort on supported models
- New `--effort` CLI flag for `llmcli infer`

#### Prompt caching improvements
- System prompts now include `cache_control: {type: "ephemeral", ttl: "1h"}`
- User messages in CLI also include cache control with 1h TTL
- Claude Code uses 1-hour cache TTL for better cost optimization

#### Brotli and zstd decompression support
- HTTP transport now handles `gzip`, `deflate`, `br` (brotli), and `zstd` compression
- Added `github.com/andybalholm/brotli` and `github.com/klauspost/compress/zstd` dependencies

### Alignment with Claude Code

#### Request headers
- `Accept-Encoding: gzip, deflate, br, zstd` now matches Claude Code exactly
- `Connection: keep-alive` header added for connection reuse
- `User-Agent` updated to `claude-cli/2.1.85`

#### Request body
- System blocks reduced to 2: billing header + systemCore (removed extra identity block)
- Billing header version updated to `2.1.85.613`

### Tests

#### Comprehensive thinking and effort tests
- Added `TestIsEffortSupported`, `TestIsMaxEffortSupported`, `TestIsAdaptiveThinkingSupported`
- Added `TestBuildRequest_OutputEffort` with 16 model/effort combinations
- Added `TestBuildRequest_ThinkingEffort_Defaults` for all model variants

---

## v0.27.0

### Bug Fixes

#### OpenRouter stream termination
- Emit `CompletedEvent` after scanner loop ends (fixes missing `CompletedEvent` when server closes stream without `[DONE]` line)
- Return after chunk error to properly terminate stream

#### MiniMax provider
- Eliminate intermediate relay publisher; pass pub directly to anthropic ParseStream with costInjector wrapper for FillCost injection

#### Bedrock
- Populate `StreamStartedEvent.Model` from `meta.ResolvedModel`

### Refactoring

#### Unified SSE stream parsing (`provider/internal/sse/lines.go`)
- New `sse.Lines` helper for robust line-based SSE parsing across providers
- Shared by OpenAI (completions), OpenRouter, and MiniMax
- Handles chunked transfer encoding and malformed lines gracefully

#### MiniMax HTTP client wiring
- Fixed HTTP client configuration to use proper transport settings

### New Features

#### ClaudeForge CLI analysis tool (`.agents/`)
- HTTP proxy tool for capturing Claude Code CLI traffic
- Logs requests/responses to `.agents/logs/claudeforge/` for diff analysis
- Used to track changes in Claude CLI behavior vs provider implementation
- See `.agents/skills/claudeforge/SKILL.md` for usage

### Tests

#### llmcli command tests
- Added `auth_test.go`, `infer_test.go`, `models_test.go`
- Refactored CLI code to be more testable

### Documentation

#### README.md and AGENTS.md
- Fixed architecture diagrams with correct file names
- Updated API examples to match actual code (`Envelope`/`StreamProcessor` pattern)
- Fixed `tool.` package references

---

## v0.26.0

### Breaking Changes

#### Event system overhaul — new type hierarchy replaces `StreamEvent`

The `StreamEvent` struct and its raw field access pattern have been replaced
with a typed event model based on the `Event` interface and an `Envelope` wrapper.

**Before:**
```go
for ev := range ch {
    switch ev.Type {
    case llm.StreamEventDelta:
        fmt.Print(ev.Text())
    case llm.StreamEventCompleted:
        fmt.Println(ev.Done.StopReason)
    }
}
```

**After:**
```go
for env := range ch {
    switch e := env.Data.(type) {
    case *llm.DeltaEvent:
        fmt.Print(e.Text)
    case *llm.CompletedEvent:
        fmt.Println(e.StopReason)
    }
}
```

New event types (`event.go`):
- `StreamCreatedEvent` — emitted once at publisher creation (replaces `StreamEventCreated`)
- `StreamStartedEvent` — emitted when the first byte arrives (replaces `StreamEventStart`)
- `DeltaEvent` — carries incremental text/reasoning/tool content (`event_delta.go`)
- `CompletedEvent` — stream end with `StopReason` and `RequestID`
- `UsageUpdatedEvent` — usage figures from the provider

Each event type has a typed `Type() EventType` method. The `Envelope` wrapper
carries `Meta EventMeta` with `RequestID`, `Seq`, `CreatedAt`, `After`, and `TraceID`.

#### `tool` package extracted to `github.com/codewandler/llm/tool`

All tool-related types that previously lived in the root package have been moved:

| Before | After |
|---|---|
| `llm.ToolCall` | `tool.Call` |
| `llm.ToolResult` | `tool.Result` |
| `llm.ToolSpec` / `llm.BoundToolSpec` | `tool.Spec[In]` / `tool.BoundToolSpec[In,Out]` |
| `llm.ToolHandler` / `llm.NewToolHandler` | `tool.Handler` / `tool.New[In,Out]` |
| `llm.ToolSet` | `tool.Set` |
| `llm.ToolChoiceAuto` / `ToolChoiceRequired` | `tool.ChoiceAuto` / `tool.ChoiceRequired` |

`tool.Definition` is the new name for a tool schema carried in a `StreamRequest`.

#### `NewEventProcessor` replaces `ProcessChan`

```go
// Before
proc := llm.ProcessChan(ctx, ch)

// After
proc := llm.NewEventProcessor(ctx, ch)
```

`ProcessEvents(ctx, ch, handler)` is a new helper that iterates over a stream
and calls a single `EventHandler` for each envelope.

#### `Publisher` interface — new canonical stream writer

Providers now write events via a `Publisher` interface instead of writing to
a raw channel. `NewEventPublisher()` returns a `(Publisher, <-chan Envelope)`
pair. Provider authors should use the typed helpers instead of raw channel sends:

```go
pub, ch := llm.NewEventPublisher()
pub.Delta(&llm.DeltaEvent{Kind: llm.DeltaKindText, Text: token})
pub.Completed(llm.CompletedEvent{StopReason: llm.StopReasonEndTurn})
pub.Close()
```

#### `json.go` — shared JSON helpers extracted

Internal JSON helpers (`unmarshalJSON`, `parsePartialJSON`) are now in `json.go`
so all providers share a single copy.

#### `response.go` and `usage.go` extracted

`StopReason` constants, the `Response` interface, and `Usage` struct are now in
their own files (`response.go`, `usage.go`) instead of `stream.go`, which has
been removed.

#### Integration tests moved to `integration/` sub-package

`cache_integration_test.go`, `integration_test.go`, and
`token_counter_drift_test.go` have been moved to the `integration/` directory,
separating long-running / network-dependent tests from unit tests.

---

### New Features

#### `tool.AsyncDispatcher` — concurrent tool execution

`tool.AsyncDispatcher` executes all tool calls in a batch concurrently, one
goroutine per call, and collects results in emission order:

```go
d := &tool.AsyncDispatcher{Handlers: tool.NewHandlers(myHandler)}
results, err := d.Dispatch(ctx, toolCalls...)
```

`tool.NewSyncDispatcher(h)` is the sequential counterpart (the original behaviour).

#### `sortmap.SortedMap` — deterministic JSON for stable cache fingerprints

`github.com/codewandler/llm/sortmap.SortedMap` serialises `map[string]any`
trees with alphabetically ordered keys at every nesting level. This produces
stable JSON for tool schema definitions, which is required to hit the prompt
cache on providers that fingerprint tool schemas (Anthropic, Bedrock):

```go
sm := sortmap.NewSortedMap(schemaMap)
b, _ := json.Marshal(sm) // keys always sorted, deep
```

#### `auto.WithoutProvider` / `auto.WithoutBedrock`

Explicitly exclude a provider from auto-detection without affecting others:

```go
p, err := auto.New(ctx,
    auto.WithoutProvider(auto.ProviderBedrock),
)

// Convenience shorthand:
p, err := auto.New(ctx, auto.WithoutBedrock())
```

#### `EventHandlerFunc` and `TypedEventHandler[T]` — lightweight handler adapters

```go
// Any function with the right signature:
h := llm.EventHandlerFunc(func(e llm.Event) { ... })

// Or typed — only called when the event matches type T:
h := llm.TypedEventHandler[*llm.DeltaEvent](func(e *llm.DeltaEvent) {
    fmt.Print(e.Text)
})
```

#### OpenRouter — updated model registry

`provider/openrouter/models.json` has been refreshed with new model additions
and updated context window / pricing metadata.

#### MiniMax — BPE tokenizer and calibrated token counter

`provider/minimax` now ships a `TokenCounter` implementation backed by a BPE
tokenizer (same `cl100k_base` encoding used by other providers), with calibrated
overhead constants to reduce count drift.

### Bug Fixes

#### `Usage` emitted before `CompletedEvent` in all providers

Previously some providers emitted usage figures after the stream-end marker,
meaning consumers that stopped reading at `CompletedEvent` would miss usage data.
All providers now guarantee `UsageUpdatedEvent` is published before
`CompletedEvent`.

---

## v0.25.0

### New Features

#### MiniMax — new provider

Added `provider/minimax`, a new LLM backend using MiniMax's Anthropic-compatible
API endpoint (`https://api.minimax.io/anthropic`). The provider delegates stream
parsing to the existing Anthropic parser, so all Anthropic features (tools,
reasoning, caching) are available out of the box. Full generation parameter
support: MaxTokens, TopP, TopK, OutputFormat.

Available models include the MiniMax-M2 family with standard and highspeed
variants.

#### StreamRequest — generation parameters across all providers

Added new fields to `llm.StreamRequest` for controlling model output, now
wired up in all providers:

- **MaxTokens** (`int`) — limits the maximum number of tokens in the response.
  When 0, the provider's default is used.
- **Temperature** (`float64`) — controls randomness in sampling (0.0–2.0).
- **TopP** (`float64`) — nucleus sampling threshold (0.0–1.0).
- **TopK** (`int`) — restricts token selection to the K most likely tokens.
- **OutputFormat** (`OutputFormat`) — `OutputFormatText` (default) or
  `OutputFormatJSON` to constrain the model to produce valid JSON.

**Provider support matrix:**

| Parameter | OpenAI | Ollama | Anthropic | MiniMax | Bedrock | OpenRouter |
|-----------|--------|--------|-----------|---------|---------|------------|
| MaxTokens | ✅ | ✅ | ✅ (default 16384) | ✅ | ✅ | ✅ |
| Temperature | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ |
| TopP | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| TopK | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| OutputFormat | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| ReasoningEffort | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ |

### Bug Fixes

#### StopReason correctness across providers

Fixed `StopReasonMaxTokens` and `StopReasonContentFilter` being silently
swallowed in two providers:

- **OpenAI Responses API** — `response.completed` events now parse
  `status`/`incomplete_details.reason` to emit `StopReasonMaxTokens` or
  `StopReasonContentFilter`. Previously both cases fell back to
  `StopReasonEndTurn`.
- **Ollama** — the `done_reason` field (`"length"`, `"stop"`) is now parsed
  from the final stream chunk. `"length"` maps to `StopReasonMaxTokens`.
  Falls back to tool-call inference for older Ollama versions that omit the
  field.
- **OpenRouter** — tool call accumulations are now flushed only on
  `finish_reason == "tool_calls"`, not on `"stop"`, aligning with the OpenAI
  completions parser.

#### Stop reason propagated through stream pipeline

`StreamEvent.Done.StopReason` is now correctly populated for all providers.
Previously the stop reason was parsed but not forwarded through the router's
pipe, so consumers always saw an empty `StopReason`.

#### MiniMax — correct cache cost calculation

`FillCost` now calculates `InputCost` using only non-cache input tokens
(`InputTokens - CacheReadTokens - CacheWriteTokens`), avoiding double-counting
cache tokens at the full input price. Cache reads and writes are now correctly
charged at their respective `CacheReadPrice` and `CacheWritePrice` rates.

### Chores

#### Core refactor — `request.go` extraction

Moved generation parameter types (`OutputFormat`, `ReasoningEffort`, etc.) and
`StreamRequest` field definitions into a dedicated `request.go` file, reducing
the size of `stream.go` and making the request API easier to navigate.

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
