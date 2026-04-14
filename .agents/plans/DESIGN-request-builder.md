# Design: Request Builder Pattern in `cli/infer`

**Date:** 2025-07  
**Status:** Draft — awaiting approval before implementation  
**Branch target:** feature/request-builder
**Updated:** options-based pattern added (§8)

---

## Problem Statement

`cmd/llmcli/cmds/infer.go` assembles an `llm.Request` by hand:

```go
msgs := opts.buildMessages()   // separate helper, applies per-message CacheHint
req := llm.Request{
    Model:        opts.Model,
    Messages:     msgs,
    Effort:       opts.Effort,
    Thinking:     opts.Thinking,
    ToolChoice:   toolChoice,
    Tools:        tools,
    MaxTokens:    opts.MaxTokens,
    Temperature:  opts.Temperature,
    TopP:         opts.TopP,
    TopK:         opts.TopK,
    OutputFormat: opts.OutputFormat,
}
```

A `RequestBuilder` already exists in `request_builder.go` but is incomplete and broken:

- **Has**: `Model`, `Thinking`, `Effort`, `MaxTokens`, `Temperature`, `OutputFormat`, `TopK`, `TopP`, `Coding()` preset
- **Missing**: message building, tool registration, tool choice, per-message cache hints
- **Defaults bug**: `newDefaultRequest()` sets `Temperature: 0.7`, `Effort: EffortLow`, `Thinking: ThinkingOff` — `NewRequestBuilder()` is supposed to be a neutral starting point but pre-fills opinionated values. Grep confirms no external callers depend on these defaults.
- **`BuildRequest` bug**: `BuildRequest(opts ...RequestOption)` silently ignores `opts` — calls `NewRequestBuilder().Build()` without passing them through.

`cli/infer` will serve as the canonical consumer that exercises the full builder surface.

---

## Goals

1. Extend `RequestBuilder` to cover all fields of `llm.Request`
2. Integrate message construction — replacing `inferOpts.buildMessages()`
3. Support per-message cache hints by reusing existing `msg.CacheOpt`/`msg.CacheTTL` machinery
4. Fix the constructor defaults and `BuildRequest` bugs
5. Migrate `runInfer` to use the builder end-to-end
6. Keep the builder non-breaking — `NewRequestBuilder()` still compiles; behaviour change is safe because no caller relied on the old defaults
7. Offer both a fluent interface **and** a functional-options pattern so callers can choose the style that fits their call site

---

## Design

### 1. Cache Types — Re-export from `llm`, Don't Invent New Ones

The `msg` package already has everything needed:

```go
// msg/cache.go — already exists
type CacheOpt interface { applyCacheOption(*CacheHint) }
type CacheTTL string
const CacheTTL5m CacheTTL = "5m"   // CacheTTLDefault
const CacheTTL1h CacheTTL = "1h"
```

`CacheTTL` implements `CacheOpt`. So `msg.CacheTTL1h` passed to a `...CacheOpt` parameter is all a caller needs.

Add re-exports to `message.go` (the existing llm-level alias file) so callers import only `llm`:

```go
// message.go additions
type (
    CacheOpt = msg.CacheOpt
    CacheTTL = msg.CacheTTL
)

const (
    CacheTTL5m = msg.CacheTTL5m
    CacheTTL1h = msg.CacheTTL1h
)
```

No new type `MessageOpt`. No `WithCache()` constructor. The existing `msg.CacheTTL` typed constant is the option.

---

### 2. Message Building on the Builder

Add fluent message methods. Parameter type is `...CacheOpt` (the re-exported alias), not a new type:

```go
func (b *RequestBuilder) System(text string, cache ...CacheOpt) *RequestBuilder
func (b *RequestBuilder) User(text string, cache ...CacheOpt) *RequestBuilder
func (b *RequestBuilder) Append(msgs ...Message) *RequestBuilder
```

- `System`/`User` construct a `msg.Message` via the existing `msg.System(text).Cache(cache...).Build()` chain
- `Append` accepts pre-built `Message` values (assistant turns, tool results, complex multi-part messages) — needed for callers that build beyond the simple System+User case
- Method named `Append` not `AddMessage`/`AddMessages` — shorter, unambiguous, consistent with slice semantics; avoids conflict with the `Message` / `Messages` type aliases in the same package

Usage in `cli/infer` after migration:

```go
b.System(system, llm.CacheTTL1h).
  User(opts.UserMsg, llm.CacheTTL1h)
```

The entire `inferOpts.buildMessages()` helper is deleted.

---

### 3. Tool Methods

Naming follows the existing builder convention: method name = field/concept, no `With` prefix.

```go
func (b *RequestBuilder) Tools(defs ...tool.Definition) *RequestBuilder
func (b *RequestBuilder) ToolChoice(tc ToolChoice) *RequestBuilder
```

- `ToolChoice(tc ToolChoice)` — method name collides with the type name but is valid Go and consistent with how `Effort(level Effort)` and `Thinking(mode ThinkingMode)` already work in this builder.
- A `ToolSet(*tool.Set)` convenience method is **deferred** — not needed for `cli/infer`.

Usage:

```go
b.Tools(defs...).ToolChoice(llm.ToolChoiceRequired{})
```

---

### 8. Options-Based Pattern

The fluent interface is convenient for inline call sites. The functional-options
pattern complements it for programmatic composition: collecting options in a
`[]RequestOption` slice, injecting options from middleware layers, or expressing
reusable presets. Both styles are first-class; callers choose based on context.

**`Apply` on the builder** — bridges the two styles:

```go
// Apply applies functional options and returns b for chaining.
// Build internally calls Apply, so Apply and Build(opts...) are equivalent
// for the final options — Apply is preferred when options are accumulated
// before the terminal Build call.
func (b *RequestBuilder) Apply(opts ...RequestOption) *RequestBuilder
```

This also replaces the private `applyOptions` helper — `Build` now delegates
to `Apply` directly.

**Package-level `With*` constructors** — one per field/concept, living in
`request_builder.go` alongside the builder:

```go
// Primitive fields
func WithModel(model string) RequestOption
func WithThinking(mode ThinkingMode) RequestOption
func WithEffort(level Effort) RequestOption
func WithMaxTokens(n int) RequestOption
func WithTemperature(t float64) RequestOption
func WithOutputFormat(f OutputFormat) RequestOption
func WithTopK(k int) RequestOption
func WithTopP(p float64) RequestOption

// Messages — same cache nil-guard semantics as the fluent methods
func WithSystem(text string, cache ...CacheOpt) RequestOption
func WithUser(text string, cache ...CacheOpt) RequestOption
func WithMessages(msgs ...Message) RequestOption // appends pre-built messages

// Tools
func WithTools(defs ...tool.Definition) RequestOption
func WithToolChoice(tc ToolChoice) RequestOption
```

Naming convention: `With` prefix distinguishes option *constructors* from the
fluent *setter methods* on the builder (`Model` vs `WithModel`). This follows
established Go convention (`grpc.WithInsecure`, `http.WithContext`, etc.).

`Coding()` is a compound preset with no simple `With` equivalent — it remains
fluent-only.

**Three equivalent styles**:

```go
// Style 1: fully fluent
req, err := llm.NewRequestBuilder().
    Model("claude-sonnet").
    System("You are helpful.", llm.CacheTTL1h).
    User("Hello").
    Build()

// Style 2: fully option-based (composable, slice-friendly)
req, err := llm.BuildRequest(
    llm.WithModel("claude-sonnet"),
    llm.WithSystem("You are helpful.", llm.CacheTTL1h),
    llm.WithUser("Hello"),
)

// Style 3: Apply to mix a pre-built slice with ad-hoc fluent calls
baseOpts := []llm.RequestOption{
    llm.WithModel("claude-sonnet"),
    llm.WithMaxTokens(2000),
}
req, err := llm.NewRequestBuilder().
    Apply(baseOpts...).
    System("You are helpful.", llm.CacheTTL1h).
    User("Hello").
    Build()
```

---

### 4. Constructor Cleanup

**`NewRequestBuilder()`** changes to return a zero-value builder. "Zero" means provider defaults for all numeric/enum fields. Grep confirms no external callers depend on the old defaults — the only caller was the buggy `BuildRequest`.

```go
func NewRequestBuilder() *RequestBuilder {
    return &RequestBuilder{req: &Request{}}
}
```

**`newDefaultRequest()`** is removed entirely.

**No `NewRequestBuilderWithDefaults()`** — there are no callers to preserve backwards compatibility for. The `Coding()` preset remains as-is; it already sets all the values it needs explicitly.

---

### 5. `BuildRequest` Bug Fix

```go
// Before (bug: opts silently ignored):
func BuildRequest(opts ...RequestOption) (Request, error) {
    return NewRequestBuilder().Build()
}

// After:
func BuildRequest(opts ...RequestOption) (Request, error) {
    return NewRequestBuilder().Build(opts...)
}
```

---

### 6. The `cli/infer` Migration

**Before** (`runInfer`, abbreviated):

```go
msgs := opts.buildMessages()

var tools []tool.Definition
toolChoice := opts.ToolChoice.Value
if opts.DemoTools {
    if toolChoice == nil {
        toolChoice = llm.ToolChoiceRequired{}
    }
    tools, opts.demoToolHandlers = buildDemoTools()
}

req := llm.Request{
    Model:        opts.Model,
    Messages:     msgs,
    Effort:       opts.Effort,
    Thinking:     opts.Thinking,
    ToolChoice:   toolChoice,
    Tools:        tools,
    MaxTokens:    opts.MaxTokens,
    Temperature:  opts.Temperature,
    TopP:         opts.TopP,
    TopK:         opts.TopK,
    OutputFormat: opts.OutputFormat,
}
```

**After**:

```go
system := opts.System
if system == "" && opts.DemoTools {
    system = defaultDemoSystemPrompt
}

b := llm.NewRequestBuilder().
    Model(opts.Model).
    Effort(opts.Effort).
    Thinking(opts.Thinking).
    MaxTokens(opts.MaxTokens).
    Temperature(opts.Temperature).
    TopP(opts.TopP).
    TopK(opts.TopK).
    OutputFormat(opts.OutputFormat)

if system != "" {
    b = b.System(system, llm.CacheTTL1h)
}
b = b.User(opts.UserMsg, llm.CacheTTL1h)

toolChoice := opts.ToolChoice.Value
if opts.DemoTools {
    if toolChoice == nil {
        toolChoice = llm.ToolChoiceRequired{}
    }
    defs, handlers := buildDemoTools()
    opts.demoToolHandlers = handlers
    b = b.Tools(defs...).ToolChoice(toolChoice)
} else if toolChoice != nil {
    b = b.ToolChoice(toolChoice)
}

req, err := b.Build()
if err != nil {
    return fmt.Errorf("build request: %w", err)
}
```

`inferOpts.buildMessages()` is deleted entirely. `inferOpts.demoToolHandlers` stays — tool handlers are execution-time, not part of the request.

---

### 7. What the Builder Does NOT Do

- **No multi-turn management**: the builder constructs a single `Request`. Appending turns from `result.Next()` is the caller's responsibility.
- **No tool handler registration**: `tool.NamedHandler` is execution-time, not request-time.
- **No provider-specific validation**: `Request.Validate()` is the gate; provider translation is the provider's concern.

---

## File Changes

| File | Change |
|------|--------|
| `message.go` | Add `CacheOpt`, `CacheTTL`, `CacheTTL5m`, `CacheTTL1h` re-exports |
| `request_builder.go` | Add `System`, `User`, `Append`, `Tools`, `ToolChoice`; add `Apply`; add `With*` option constructors; fix `NewRequestBuilder` (zero-value); remove `newDefaultRequest` and private `applyOptions`; fix `BuildRequest` bug |
| `request_builder_test.go` | New file — tests for all new builder and option functions |
| `cmd/llmcli/cmds/infer.go` | Migrate `runInfer` to builder; delete `buildMessages()` |

No new files beyond `request_builder_test.go`.

---

## API Summary

### New / Changed in `request_builder.go`

```go
// Constructor — now returns zero-value builder (changed behaviour, no external callers)
func NewRequestBuilder() *RequestBuilder

// Apply — new; bridges fluent and options-based styles
func (b *RequestBuilder) Apply(opts ...RequestOption) *RequestBuilder

// Message methods — new
func (b *RequestBuilder) System(text string, cache ...CacheOpt) *RequestBuilder
func (b *RequestBuilder) User(text string, cache ...CacheOpt) *RequestBuilder
func (b *RequestBuilder) Append(msgs ...Message) *RequestBuilder

// Tool methods — new
func (b *RequestBuilder) Tools(defs ...tool.Definition) *RequestBuilder
func (b *RequestBuilder) ToolChoice(tc ToolChoice) *RequestBuilder

// With* option constructors — new
func WithModel(model string) RequestOption
func WithThinking(mode ThinkingMode) RequestOption
func WithEffort(level Effort) RequestOption
func WithMaxTokens(n int) RequestOption
func WithTemperature(t float64) RequestOption
func WithOutputFormat(f OutputFormat) RequestOption
func WithTopK(k int) RequestOption
func WithTopP(p float64) RequestOption
func WithSystem(text string, cache ...CacheOpt) RequestOption
func WithUser(text string, cache ...CacheOpt) RequestOption
func WithMessages(msgs ...Message) RequestOption
func WithTools(defs ...tool.Definition) RequestOption
func WithToolChoice(tc ToolChoice) RequestOption

// Bug fix
func BuildRequest(opts ...RequestOption) (Request, error) // now passes opts through

// Removed
// func newDefaultRequest() *Request
// func (b *RequestBuilder) applyOptions(opts ...RequestOption) — replaced by Apply

// Unchanged
func (b *RequestBuilder) Model(modelID string) *RequestBuilder
func (b *RequestBuilder) Thinking(mode ThinkingMode) *RequestBuilder
func (b *RequestBuilder) Effort(level Effort) *RequestBuilder
func (b *RequestBuilder) MaxTokens(maxTokens int) *RequestBuilder
func (b *RequestBuilder) Temperature(temperature float64) *RequestBuilder
func (b *RequestBuilder) OutputFormat(format OutputFormat) *RequestBuilder
func (b *RequestBuilder) TopK(k int) *RequestBuilder
func (b *RequestBuilder) TopP(p float64) *RequestBuilder
func (b *RequestBuilder) Coding() *RequestBuilder
func (b *RequestBuilder) Build(opts ...RequestOption) (Request, error)
```

### New in `message.go`

```go
type CacheOpt = msg.CacheOpt
type CacheTTL = msg.CacheTTL

const (
    CacheTTL5m = msg.CacheTTL5m
    CacheTTL1h = msg.CacheTTL1h
)
```

---

## Deferred

- `ToolSet(*tool.Set)` convenience method — not needed for `cli/infer`
- `CodingPreset() []RequestOption` — a `Coding()` equivalent for the options pattern; deferred until there is a caller
- `Coding()` should eventually require a model to be set or document that `Model()` must still be called — low priority, no behaviour change needed now
