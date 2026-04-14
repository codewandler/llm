# Design: Usage & Cost Tracking Overhaul

**Date**: 2026-04-14  
**Status**: Draft (rev 8)  
**Scope**: new `usage/` package, `event.go`, `event_processor.go`, `model.go`, `ratelimit.go`, all providers

---

## Problem Statement

### 1. Tokens and cost are mixed in one struct

`llm.Usage` holds both server-reported token counts (`InputTokens`, `OutputTokens`) and
locally-derived cost values (`Cost`, `InputCost`, `CacheReadCost`, …) in the same struct.
These have different origins and certainty levels:

- Token counts are **facts** — reported by the API.
- Cost values are **derived** — calculated from a local pricing table, or API-reported
  (OpenRouter sends `cost` in its usage object), or zero because the model is unknown.

There is no way to tell from the struct whether `Cost > 0` means "locally calculated",
"API-reported", or "zero because the model has no pricing entry".

### 2. Pricing tables scattered across providers and ambiguous token representation

**Pricing scattered.** `fillCost`/`FillCost`/`calculateCost` exists verbatim in:
`provider/anthropic/models.go`, `provider/bedrock/models.go`,
`provider/openai/models.go`, `provider/minimax/models.go`.
All four implement the identical formula and maintain their own pricing maps.
There is no single place to update a price, add a new model, or verify coverage.

**Ambiguous zero values.** The current flat `Tokens` struct makes zeros unreadable:

```go
type Tokens struct {
    Input      int  // zero: not reported? or genuinely zero input?
    CacheRead  int  // zero: no cache used? or provider doesn't support cache?
    Reasoning  int  // zero: no reasoning? or subset already in Output?
}
```

A consumer cannot tell whether `CacheRead == 0` means the provider returned it as zero,
or simply didn't report it. `Reasoning` is a subset of `Output` (same billing rate on all
current providers) — having it as a sibling field implies it might be independently billed
or excludes it from `Output`, neither of which is true.

### 3. Cost calculator is not pluggable

Pricing tables are static, provider-internal, and non-extensible. Library users cannot:
- Swap in pricing data fetched live from a provider API.
- Use model specs returned by provider `/models` endpoints (e.g. OpenRouter returns
  `pricing.prompt`, `pricing.completion` per model).
- Override or extend pricing for custom deployments.
- Attach a calculator to a Tracker for retroactive cost enrichment.

### 4. `llm.Model` carries no pricing

The `llm.Model` struct (`ID`, `Name`, `Provider`, `Aliases`) has no pricing field.
Providers that fetch model lists dynamically (OpenRouter, Ollama) already receive pricing
from the API but have nowhere to store it.  `modeldb.Model` (models.dev database) has a
full `Cost` struct — but that type lives in a separate package disconnected from
`llm.Model`.

### 5. Ollama emits empty usage (bug)

`provider/ollama/ollama.go` declares `var usage llm.Usage` and never populates it.
Ollama's final chunk carries `prompt_eval_count` and `eval_count` — these are never read.

### 6. Token estimate is llmcli-only; drift is never tracked

All seven providers already implement `tokencount.TokenCounter` with compile-time
assertions (`var _ tokencount.TokenCounter = (*Provider)(nil)`), but the library
never calls them. `cmd/llmcli/cmds/infer.go` is the only caller; library users who
want pre-request estimates must replicate the detection and call themselves.

Further, the difference between an estimate and the provider-reported actual —
called **drift** — is never recorded. A one-off display in llmcli verbose output
is not the same as tracking drift over time. Without a history of drift you cannot
answer: "are my Bedrock estimates systematically 8% low?" or "which model gives me
the most reliable estimates?" or "have my context sizes grown enough that I need
to revisit my budget estimates?"

### 7. Provider-specific stats are discarded

Providers return rich per-request metadata beyond token counts. The Anthropic/Claude
provider already parses `Anthropic-Ratelimit-Unified-*` response headers into a fully
structured `llm.RateLimits` value (5-hour window utilisation, 7-day window utilisation,
overage status, fallback percentage, representative claim). This data is currently placed
only in `StreamStartedEvent.Extra["rate_limits"]` — it is not attached to the usage
record and therefore cannot be correlated with request cost or token consumption in a
Tracker history.

Without this correlation there is no way to answer: "which specific request brought my
5-hour window from 60% to 95% utilisation?" or "what model and token count triggered the
first `over_budget` status this week?"

Other providers may expose equivalent data in future (remaining quota headers, etc.).
The design must accommodate this generically without coupling the `usage` package to any
specific provider type.

### 8. No attribution, no history, no budget

`UsageUpdatedEvent` carries a bare `llm.Usage` with no provider name, no model, no
request ID, no turn or session. `EventProcessor.applyUsage` keeps only the **first**
event; later events are silently dropped. There is no Tracker, no multi-turn aggregation,
no budget guardrail.

---

## Design

### Package layout

```
llm/
├── model.go                  # llm.Model gains Pricing *usage.Pricing
└── usage/
    ├── record.go             # TokenKind, TokenItem, TokenItems, Cost, Pricing, Dims, Record, CalcCost
    ├── pricing.go            # KnownPricing registry; Static, ModelDB, Compose, Default calculators
    ├── calculator.go         # CostCalculator interface, CostCalculatorFunc
    ├── drift.go              # Drift, DriftStats
    ├── tracker.go            # Tracker, FilterFunc helpers, TrackerOption
    └── budget.go             # Budget
```

Import graph (no cycles):

```
llm (root) ──imports──▶ usage
usage      ──imports──▶ modeldb
providers  ──imports──▶ llm, usage
```

`usage` has zero imports from the `llm` root package.
`llm` root imports `usage` (same pattern as existing `llm → msg`, `llm → tool`).

---

### `usage/record.go`

#### `usage.TokenKind` and `usage.TokenItem`

Instead of a flat struct with ambiguous zero fields, tokens are represented as an ordered
slice of items. **An item is only created when something independently billable actually
occurred and is not already counted inside another item.**

```go
// TokenKind identifies one independently-priced token category.
type TokenKind string

const (
    // KindInput are regular (non-cache) input tokens.
    // For providers with cache support this is:
    //   total_input_tokens - cache_read_tokens - cache_write_tokens
    // Priced at the model's standard input rate.
    KindInput TokenKind = "input"

    // KindOutput are generated/completion tokens, EXCLUDING reasoning tokens.
    // When the upstream API reports reasoning tokens separately, providers MUST
    // subtract them here so that KindOutput + KindReasoning == total completions
    // with no overlap. When the API cannot distinguish, KindOutput holds the
    // full completion count and KindReasoning is omitted.
    // Priced at the model's standard output rate.
    KindOutput TokenKind = "output"

    // KindReasoning are thinking/reasoning tokens reported by the provider.
    // Only emitted when the upstream API reports them as a distinct value.
    // They are NOT included in KindOutput (no double-counting).
    // Priced at the model's reasoning rate; falls back to output rate when
    // Pricing.Reasoning == 0 (true for all current providers).
    KindReasoning TokenKind = "reasoning"

    // KindCacheRead are input tokens served from an existing prompt cache entry.
    // Anthropic: cache_read_input_tokens.
    // OpenAI:    prompt_tokens_details.cached_tokens.
    // Priced at a reduced cache-read rate; NOT included in KindInput.
    KindCacheRead TokenKind = "cache_read"

    // KindCacheWrite are input tokens written to a new prompt cache entry.
    // Anthropic / Bedrock only (cache_creation_input_tokens).
    // Priced at a cache-write rate; NOT included in KindInput.
    KindCacheWrite TokenKind = "cache_write"
)

// TokenItem is one independently-priced token entry.
type TokenItem struct {
    Kind  TokenKind `json:"kind"`
    Count int       `json:"count"`
}
```

#### `usage.TokenItems`

```go
// TokenItems is the ordered list of token items for a Record.
// Each kind appears at most once per record.
type TokenItems []TokenItem

// Count returns the count for the item with the given kind.
// Returns 0 if no item with that kind exists.
func (t TokenItems) Count(kind TokenKind) int

// TotalInput returns Input + CacheRead + CacheWrite.
func (t TokenItems) TotalInput() int

// TotalOutput returns Output + Reasoning.
func (t TokenItems) TotalOutput() int

// Total returns TotalInput + TotalOutput.
func (t TokenItems) Total() int

// NonZero returns a new slice with zero-count items removed.
func (t TokenItems) NonZero() TokenItems
```

**No-overlap invariant** (providers must satisfy):
- `Count(KindOutput) + Count(KindReasoning) == total_completion_tokens`
- `Count(KindInput) + Count(KindCacheRead) + Count(KindCacheWrite) == total_input_tokens`

**Provider mapping — actual usage items (provider-reported):**

| Provider | Items emitted | KindReasoning separation |
|---|---|---|
| Anthropic / Claude-OAuth | `KindInput`, `KindCacheRead`, `KindCacheWrite`, `KindOutput` | API does not report separately → omit |
| Bedrock | `KindInput`, `KindCacheRead`, `KindCacheWrite`, `KindOutput` | Not reported → omit |
| OpenAI o-series | `KindInput`, `KindCacheRead`, `KindOutput`, `KindReasoning` | `completion_tokens_details.reasoning_tokens` → `KindOutput = completions - reasoning` |
| OpenAI non-o-series | `KindInput`, `KindCacheRead`, `KindOutput` | No reasoning field |
| MiniMax | `KindInput`, `KindOutput` | Not reported |
| OpenRouter | `KindInput`, `KindOutput` | Not forwarded by OpenRouter API |
| Ollama | `KindInput`, `KindOutput` | Not applicable |

**Provider mapping — estimate method:**

All providers implement `tokencount.TokenCounter` with compile-time assertions.
Estimate quality varies by method:

| Provider | Method | Accuracy |
|---|---|---|
| Anthropic / Claude-OAuth | Anthropic `/v1/messages/count_tokens` API (network call) | Exact |
| OpenAI | tiktoken per-model (o200k for GPT-4o/o-series; cl100k for GPT-4) | Exact for text; small margin for tools |
| MiniMax | MiniMax BPE tokenizer (200K vocab), calibrated constants | Calibrated, tracks drift in tests |
| OpenRouter | BPE, model-family aware (strips `vendor/` prefix for encoding selection) | Approximate |
| Bedrock | cl100k_base BPE (Anthropic models on Bedrock) | Approximate ±5–10% |
| Ollama | cl100k_base BPE (covers most hosted models) | Approximate ±10% |

#### `usage.Cost`

```go
// Cost holds monetary amounts in USD.
// All fields are derived UNLESS Source == "reported" (provider sent the value directly).
type Cost struct {
    Total      float64 `json:"total"`
    Input      float64 `json:"input,omitempty"`
    Output     float64 `json:"output,omitempty"`
    Reasoning  float64 `json:"reasoning,omitempty"`  // zero when KindReasoning not present
    CacheRead  float64 `json:"cache_read,omitempty"`
    CacheWrite float64 `json:"cache_write,omitempty"`

    // Source describes how the cost was determined.
    //   "calculated" — via CalcCost from KnownPricing or ModelDB
    //   "reported"   — API-provided total (OpenRouter)
    //   "estimated"  — pre-request estimate from CountTokens
    //   ""           — no pricing available (Ollama local, unknown model)
    Source string `json:"source,omitempty"`
}

func (c Cost) IsZero() bool { return c.Source == "" && c.Total == 0 }
```

#### `usage.Dims`

```go
// Dims carries attribution context for a Record.
type Dims struct {
    Provider  string            `json:"provider,omitempty"`
    Model     string            `json:"model,omitempty"`
    RequestID string            `json:"request_id,omitempty"`
    TurnID    string            `json:"turn_id,omitempty"`    // caller-assigned turn identifier
    SessionID string            `json:"session_id,omitempty"` // caller-assigned session identifier

    // Labels are arbitrary string key-value annotations on the Record.
    // Used to distinguish sub-breakdowns within a request, e.g. in estimates:
    //   {"category": "system"}       — system prompt tokens
    //   {"category": "conversation"} — conversation history tokens
    //   {"category": "tools"}        — tool definition tokens
    // Provider-reported records carry no labels.
    Labels    map[string]string `json:"labels,omitempty"`
}
```

#### `usage.Record`

```go
// Record is a single, fully-attributed usage record.
type Record struct {
    Tokens     TokenItems     `json:"tokens"`
    Cost       Cost           `json:"cost"`
    Dims       Dims           `json:"dims"`
    IsEstimate bool           `json:"is_estimate,omitempty"`
    RecordedAt time.Time      `json:"recorded_at"`

    // Extras holds provider-specific metadata captured at request time.
    // Mirrors the StreamStartedEvent.Extra convention (same map[string]any type).
    // Keys and value types are defined per provider:
    //
    //   Anthropic / Claude-OAuth: "rate_limits" -> *llm.RateLimits
    //     Contains 5h/7d window utilisation, overage status, fallback percentage,
    //     and representative claim. Populated from HTTP response headers.
    //
    // nil for estimate records and for providers that return no extras.
    Extras map[string]any `json:"extras,omitempty"`
}
```

#### `usage.CalcCost` — one canonical formula

Takes `TokenItems` — the "no item = not present" semantics eliminate the need for
zero-clamping and make the formula trivially correct:

```go
// CalcCost computes a Cost from token items and pricing.
// Each item contributes exactly one cost component.
// KindReasoning falls back to p.Output rate when p.Reasoning == 0.
// Sets Cost.Source = "calculated".
func CalcCost(items TokenItems, p Pricing) Cost {
    var c Cost
    for _, item := range items {
        switch item.Kind {
        case KindInput:
            c.Input = float64(item.Count) / 1_000_000 * p.Input
        case KindCacheRead:
            c.CacheRead = float64(item.Count) / 1_000_000 * p.CachedInput
        case KindCacheWrite:
            c.CacheWrite = float64(item.Count) / 1_000_000 * p.CacheWrite
        case KindOutput:
            c.Output = float64(item.Count) / 1_000_000 * p.Output
        case KindReasoning:
            rate := p.Reasoning
            if rate == 0 {
                rate = p.Output // current providers price reasoning at output rate
            }
            c.Reasoning = float64(item.Count) / 1_000_000 * rate
        }
    }
    c.Total  = c.Input + c.CacheRead + c.CacheWrite + c.Output + c.Reasoning
    c.Source = "calculated"
    return c
}
```

---

### `usage/pricing.go` — centralized pricing registry

All known model pricing lives here. Providers no longer maintain their own pricing maps.

#### `usage.Pricing`

```go
// Pricing holds per-token rates in USD per million tokens.
type Pricing struct {
    Input       float64 `json:"input"`
    Output      float64 `json:"output"`
    Reasoning   float64 `json:"reasoning,omitempty"`   // 0 = same rate as Output
    CachedInput float64 `json:"cached_input,omitempty"`
    CacheWrite  float64 `json:"cache_write,omitempty"`
}
```

#### `usage.PricingEntry` and `usage.KnownPricing`

```go
// PricingEntry associates a provider+model pair with its pricing.
type PricingEntry struct {
    Provider string
    Model    string  // exact ID or prefix (e.g. "claude-sonnet-4-6")
    Pricing  Pricing
}

// KnownPricing is the built-in registry of well-known model prices.
// Entries from all providers are kept here; providers no longer carry their own tables.
// Source: provider pricing pages. Update as new models launch.
var KnownPricing = []PricingEntry{
    // Anthropic
    {"anthropic", "claude-opus-4-6",            Pricing{Input: 5.0,   Output: 25.0,  CachedInput: 0.50, CacheWrite: 6.25}},
    {"anthropic", "claude-sonnet-4-6",          Pricing{Input: 3.0,   Output: 15.0,  CachedInput: 0.30, CacheWrite: 3.75}},
    {"anthropic", "claude-haiku-4-5-20251001",  Pricing{Input: 1.0,   Output: 5.0,   CachedInput: 0.10, CacheWrite: 1.25}},
    // ... (all entries currently in provider/anthropic/models.go and others)
    // OpenAI
    {"openai", "gpt-4o",                        Pricing{Input: 2.50,  Output: 10.0,  CachedInput: 1.25}},
    {"openai", "gpt-4o-mini",                   Pricing{Input: 0.15,  Output: 0.60,  CachedInput: 0.075}},
    // ... (all entries currently in provider/openai/models.go)
    // Bedrock (same as Anthropic; different provider key)
    {"bedrock", "anthropic.claude-sonnet-4-6",  Pricing{Input: 3.0,   Output: 15.0,  CachedInput: 0.30, CacheWrite: 3.75}},
    // ...
}
```

Prefix matching (stripping date suffixes like `-20250929`) is applied when an exact match
is not found, mirroring the current logic in `provider/anthropic/models.go`.

#### Built-in calculators

```go
// Static returns a CostCalculator backed by the provided pricing entries.
// When called with no arguments it uses KnownPricing.
// Entries are matched: exact model ID first, then longest-prefix match after
// stripping a trailing 8-digit date suffix.
func Static(entries ...PricingEntry) CostCalculator

// ModelDB returns a CostCalculator backed by the embedded models.dev database.
// It is useful as a broader fallback for models not in KnownPricing.
func ModelDB() CostCalculator

// Compose returns a CostCalculator that tries each given calculator in order,
// returning the first successful result.
// Example: Compose(Static(), ModelDB()) — static prices first, modeldb as fallback.
func Compose(calculators ...CostCalculator) CostCalculator

// Default returns the recommended default calculator:
//   Compose(Static(KnownPricing...), ModelDB())
// Static() is checked first because KnownPricing is manually maintained and
// verified against provider docs. ModelDB() provides broader coverage for
// newer or less common models.
func Default() CostCalculator
```

**Usage by providers.** Instead of an `anthropicStaticCalculator` type per provider,
every provider simply calls `usage.Default()` (or its own `Compose` variant if it wants
to inject dynamic `llm.Model.Pricing` from a live fetch):

```go
// In anthropic/stream_processor.go onMessageStop, calculating cost at emit time:
if cost, ok := usage.Default().Calculate(llm.ProviderNameAnthropic, model, tokens); ok {
    rec.Cost = cost
}
```

Providers no longer own pricing code. All per-provider pricing tables migrate to
`usage/pricing.go` as `PricingEntry` rows in `KnownPricing`.

---

### `usage/calculator.go`

#### `usage.CostCalculator` interface

```go
// CostCalculator computes a Cost for a given provider, model, and token items.
type CostCalculator interface {
    // Calculate returns (Cost, true) when pricing is known,
    // (Cost{}, false) when the provider+model has no entry.
    Calculate(provider, model string, tokens TokenItems) (Cost, bool)
}
```

#### `usage.CostCalculatorFunc`

```go
type CostCalculatorFunc func(provider, model string, tokens TokenItems) (Cost, bool)

func (f CostCalculatorFunc) Calculate(p, m string, t TokenItems) (Cost, bool) { return f(p, m, t) }
```

---

### `llm.Model` — add optional pricing

`model.go` gains `Pricing *usage.Pricing`:

```go
type Model struct {
    ID       string         `json:"id"`
    Name     string         `json:"name"`
    Provider string         `json:"provider"`
    Aliases  []string       `json:"aliases,omitempty"`
    Pricing  *usage.Pricing `json:"pricing,omitempty"` // nil unless fetched dynamically
}
```

This field is only populated by providers whose `FetchModels` API returns pricing
(e.g. OpenRouter `/models` returns `pricing.prompt`/`pricing.completion`). The static
model lists in provider `models.go` files leave it nil — those prices are in `KnownPricing`.

#### `llm.ModelListCalculator` helper

Lives in `model.go` (can import `usage`; avoids reverse cycle):

```go
// ModelListCalculator returns a CostCalculator backed by a dynamically-fetched model list.
// Intended to be composed after Static() for providers that populate Model.Pricing:
//
//   usage.Compose(usage.Static(), llm.ModelListCalculator(p.Models()), usage.ModelDB())
func ModelListCalculator(models Models) usage.CostCalculator {
    return usage.CostCalculatorFunc(func(_, model string, tokens usage.TokenItems) (usage.Cost, bool) {
        for _, m := range models {
            if m.ID == model && m.Pricing != nil {
                return usage.CalcCost(tokens, *m.Pricing), true
            }
        }
        return usage.Cost{}, false
    })
}
```

---

### Provider-level `CostCalculator` — optional interface

```go
// CostCalculatorProvider is an optional interface providers may implement to
// expose a calculator. Useful for Tracker injection and offline estimation.
type CostCalculatorProvider interface {
    CostCalculator() usage.CostCalculator
}
```

For providers with no dynamic model pricing, this is trivially:

```go
// provider/anthropic/anthropic.go
func (*Provider) CostCalculator() usage.CostCalculator { return usage.Default() }
```

For providers that do dynamic fetches (e.g. a future OpenRouter implementation that
calls `/models` and populates `Model.Pricing`):

```go
func (p *Provider) CostCalculator() usage.CostCalculator {
    return usage.Compose(usage.Static(), llm.ModelListCalculator(p.Models()), usage.ModelDB())
}
```

Providers that receive API-reported costs (OpenRouter) and providers without pricing
(Ollama) do **not** implement `CostCalculatorProvider`.

---

### OpenRouter — reported cost passthrough

OpenRouter already sends `cost float64` in the stream's usage object (line 424 of
`openrouter.go`). This must be preserved as `Source: "reported"`, not recalculated.

In OpenRouter's `parseStream`, when building the record:

```go
rec := usage.Record{
    RecordedAt: time.Now(),
    Dims:       usage.Dims{Provider: providerName, Model: resolvedModel},
    Tokens: usage.TokenItems{
        {Kind: usage.KindInput,  Count: chunk.Usage.PromptTokens - cached},
        {Kind: usage.KindCacheRead, Count: cached},        // omitted if cached == 0
        {Kind: usage.KindOutput, Count: chunk.Usage.CompletionTokens},
    },
    Cost: usage.Cost{
        Total:  chunk.Usage.Cost,
        Source: "reported",
    },
    // Breakdown cost fields are zero — OpenRouter reports total only.
    // Source="reported" makes this unambiguous; the Tracker will not recalculate.
}
pub.UsageRecord(rec)
```

The `CostCalculator` is NOT called for OpenRouter records where `Source == "reported"`.
The tracker must not overwrite a "reported" cost with a calculated one.

---

### Changes to `UsageUpdatedEvent`

The legacy `Usage Usage` field is **deleted**. The event carries only the attributed record:

```go
type UsageUpdatedEvent struct {
    Record usage.Record `json:"record"`
}
```

`Publisher` loses `Usage(u Usage)` entirely. The only method for emitting usage is:

```go
UsageRecord(r usage.Record)
```

All provider call-sites are updated to call `pub.UsageRecord(rec)` directly.
The old `pub.Usage(llm.Usage{...})` call pattern is removed everywhere.

---

### New stream event: `StreamEventTokenEstimate`

```go
// event.go
StreamEventTokenEstimate EventType = "token_estimate"

type TokenEstimateEvent struct {
    // Estimate is one pre-request estimate record.
    // The event is emitted once per record; multiple events may be emitted per request
    // when a labeled breakdown is provided (each with distinct Dims.Labels).
    Estimate usage.Record `json:"estimate"` // IsEstimate == true
}

func (e TokenEstimateEvent) Type() EventType { return StreamEventTokenEstimate }
```

`Publisher` interface gains:

```go
TokenEstimate(est usage.Record)
```

#### Emission pattern

Each provider that implements `tokencount.TokenCounter` emits this event in `CreateStream`,
immediately after `RequestEvent`, before the HTTP round-trip.

**Simple form** — one record with the total input count (always emitted):

```go
if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
    Model: opts.Model, Messages: opts.Messages, Tools: opts.Tools,
}); err == nil {
    tokens := usage.TokenItems{{Kind: usage.KindInput, Count: est.InputTokens}}
    rec := usage.Record{
        IsEstimate: true,
        RecordedAt: time.Now(),
        Dims:       usage.Dims{Provider: llm.ProviderNameAnthropic, Model: opts.Model},
        Tokens:     tokens,
    }
    if c, ok := usage.Default().Calculate(llm.ProviderNameAnthropic, opts.Model, tokens); ok {
        rec.Cost = c
        rec.Cost.Source = "estimated"
    }
    pub.TokenEstimate(rec)
}
```

**Rich form** — multiple labeled records per request (optional, when the caller
wants a breakdown of where input tokens come from):

```go
for _, breakdown := range []struct{ category string; count int }{
    {"system", systemTokens},
    {"conversation", conversationTokens},
    {"tools", toolTokens},
} {
    tokens := usage.TokenItems{{Kind: usage.KindInput, Count: breakdown.count}}
    pub.TokenEstimate(usage.Record{
        IsEstimate: true,
        RecordedAt: time.Now(),
        Dims: usage.Dims{
            Provider:  llm.ProviderNameAnthropic,
            Model:     opts.Model,
            RequestID: requestID,
            Labels:    map[string]string{"category": breakdown.category},
        },
        Tokens: tokens,
    })
}
```

The labeled records share the same `RequestID`. `Result.TokenEstimates()` returns all
estimate records in order — first the unlabeled total, then any labeled breakdowns.
Cost is typically only calculated for the unlabeled total (it equals the sum).

All seven providers emit at least the simple-form estimate. The labeled breakdown
is only emitted when the caller explicitly constructs per-category items before
calling `CreateStream` (it is not automatic).

---

### Changes to `EventProcessor` / `Result`

`llm.Usage` is **deleted**. `result` stores records directly:

```go
type result struct {
    // ... existing text/tool fields unchanged ...
    usageRecords  []usage.Record
    estimateRecs  []usage.Record // all estimate records, in order
}

func (r *result) applyUsage(rec usage.Record) {
    r.usageRecords = append(r.usageRecords, rec)
}

func (r *result) applyEstimate(rec usage.Record) {
    r.estimateRecs = append(r.estimateRecs, rec)
}
```

`Result` interface — `Usage() *Usage` is **removed**. Replacement:

```go
type Result interface {
    Response
    Next()           msg.Messages
    UsageRecords()   []usage.Record  // provider-reported, in arrival order
    TokenEstimates() []usage.Record  // pre-request estimates, in order
    Drift()          *usage.Drift    // nil if no estimate received
}
```

`Response` also loses its `Usage() *Usage` method.

```go
case *UsageUpdatedEvent:
    r.result.applyUsage(ev.Record)
case *TokenEstimateEvent:
    r.result.applyEstimate(ev.Estimate)
```

---

### Provider-specific stats — `Extras`

`Record.Extras` is the provider's slot for structured data that arrived alongside the
response but belongs to neither `Tokens`, `Cost`, nor `Dims`. The field uses
`map[string]any` — the same convention as `StreamStartedEvent.Extra` — so the `usage`
package stays import-free from `llm` root, and providers can add keys without any change
to the package API.

#### Why `map[string]any` and not a typed interface

- The `usage` package has zero imports from `llm` root (import graph constraint). Putting
  `*llm.RateLimits` directly in `usage.Record` would create a cycle.
- The stated goal is **record-and-correlate**, not programmatic inspection inside the
  `usage` package. Callers who need typed access cast from the map:
  ```go
  if rl, ok := rec.Extras["rate_limits"].(*llm.RateLimits); ok {
      fmt.Printf("5h utilisation: %.0f%%\n", rl.Unified.FiveHour.Utilization*100)
  }
  ```
- New providers add new keys without changing any shared interface.
- Serialises cleanly: `json.Marshal` of a `map[string]any` holding a `*llm.RateLimits`
  produces the full struct fields, readable in logs and storage.

#### How Anthropic populates Extras

`llm.RateLimits` is already parsed in `newStreamProcessor` from HTTP response headers and
stored in `p.rateLimits`. Currently it only reaches `StreamStartedEvent.Extra`. With this
change the same pointer is also written into the `usage.Record` at end-of-stream, giving
the Tracker a per-request snapshot at the moment the response completed:

```go
// provider/anthropic/stream_processor.go — onMessageStop:
regular := p.usage.InputTokens - p.usage.CacheReadTokens - p.usage.CacheWriteTokens
tokens := usage.TokenItems{
    {Kind: usage.KindInput,      Count: regular},
    {Kind: usage.KindCacheRead,  Count: p.usage.CacheReadTokens},
    {Kind: usage.KindCacheWrite, Count: p.usage.CacheWriteTokens},
    {Kind: usage.KindOutput,     Count: p.usage.OutputTokens},
}
// Drop zero-count items so absence == not reported:
tokens = tokens.NonZero()

var extras map[string]any
if p.rateLimits != nil {
    extras = map[string]any{"rate_limits": p.rateLimits}
}
rec := usage.Record{
    Dims:       usage.Dims{Provider: ..., Model: p.meta.Model, RequestID: ...},
    Tokens:     tokens,
    Extras:     extras,
    RecordedAt: time.Now(),
}
if cost, ok := usage.Default().Calculate(..., p.meta.Model, tokens); ok {
    rec.Cost = cost
}
pub.UsageRecord(rec)
```

(`TokenItems.NonZero()` is a helper that returns a new slice with zero-count items removed.)

#### Tracker does not interpret Extras

`Tracker.Aggregate()` sums `Tokens` and `Cost` only. `Extras` is never merged,
aggregated, or inspected by the tracker. Individual records retain their full `Extras`
value. Consumers call `tracker.Records()` or `tracker.Filter(...)` and read `Extras`
themselves for correlation.

#### Other providers

All other providers emit `Extras: nil` for now. When they gain per-request quota or
subscription-state headers they will populate their own keys under the same pattern,
documented by a comment in their respective provider file.

---

### `usage/tracker.go`

```go
type Tracker struct {
    mu         sync.Mutex
    records    []Record
    budget     Budget
    calculator CostCalculator // optional; enriches cost-less records on Record()
    sessionID  string
}

func NewTracker(opts ...TrackerOption) *Tracker

// Record appends r to the history.
// If r.Cost.IsZero() and a CostCalculator is configured, the tracker attempts
// to fill cost before storing. Records with Source == "reported" are never
// recalculated.
func (t *Tracker) Record(r Record)

func (t *Tracker) Records() []Record
func (t *Tracker) Aggregate() Record          // sum of all non-estimate records
func (t *Tracker) Filter(fs ...FilterFunc) []Record
func (t *Tracker) WithinBudget() bool
func (t *Tracker) Reset()
```

#### TrackerOptions

```go
func WithBudget(b Budget) TrackerOption
func WithSessionID(id string) TrackerOption
func WithCostCalculator(c CostCalculator) TrackerOption
```

#### Tracker cost enrichment rule

```
if rec.Cost.IsZero() AND rec.Cost.Source != "reported" AND t.calculator != nil:
    if cost, ok := t.calculator.Calculate(rec.Dims.Provider, rec.Dims.Model, rec.Tokens); ok:
        rec.Cost = cost
```

This makes the tracker useful for providers without provider-side cost calculation
(e.g., Ollama), and allows library users to inject their own pricing logic.

#### FilterFunc helpers

```go
type FilterFunc func(Record) bool

func ByProvider(name string) FilterFunc
func ByModel(model string) FilterFunc
func ByTurnID(id string) FilterFunc
func BySessionID(id string) FilterFunc
func EstimatesOnly() FilterFunc
func ExcludeEstimates() FilterFunc
func Since(t time.Time) FilterFunc

// ByLabel returns records whose Dims.Labels[key] == value.
func ByLabel(key, value string) FilterFunc
```

---

### `usage/budget.go`

```go
type Budget struct {
    MaxCostUSD      float64
    MaxInputTokens  int
    MaxOutputTokens int
    MaxTotalTokens  int
}

// Exceeded returns true when agg violates any non-zero limit.
func (b Budget) Exceeded(agg Record) bool { ... }
```

---

### `usage/drift.go` — drift tracking

**Drift** is the difference between a pre-request input-token estimate and the
provider-reported actual. It surfaces how reliable each provider's token counting
method is over time and can detect context-growth patterns before they surprise
a budget.

Only input tokens are tracked: output tokens cannot be estimated before generation.

```go
// Drift holds the delta between the unlabeled pre-request estimate and the
// provider-reported actual for a single request.
type Drift struct {
    Dims Dims // from the actual Record (provider, model, requestID, ...)

    EstimatedInput int     // Count(KindInput) from the unlabeled estimate
    ActualInput    int     // Count(KindInput) from the actual record

    // InputDelta = ActualInput - EstimatedInput.
    // Positive = underestimate (provider used more tokens than predicted).
    // Negative = overestimate (provider used fewer tokens than predicted).
    InputDelta int

    // InputPct = InputDelta / EstimatedInput * 100.
    // math.NaN() when EstimatedInput == 0.
    InputPct float64

    Estimate Record // the matched estimate (IsEstimate == true, Labels == nil)
    Actual   Record // the matched provider-reported actual
}

// DriftStats aggregates drift across multiple matched request pairs.
type DriftStats struct {
    N       int     // number of matched pairs
    MinPct  float64 // best-case (most negative = largest overestimate)
    MaxPct  float64 // worst-case (most positive = largest underestimate)
    MeanPct float64
    P50Pct  float64 // median
    P95Pct  float64 // 95th percentile — useful for worst-case budget planning
}
```

**Matching rule**: a `Drift` is formed when:
- An estimate record exists with `Dims.RequestID == X` and `Dims.Labels == nil`
- An actual record exists with `Dims.RequestID == X` and `IsEstimate == false`

Labeleled estimate records (the per-category breakdown) do NOT participate in drift
computation — only the unlabeled total estimate is matched to the actual.

**Tracker methods:**

```go
// Drift computes the drift for a given requestID.
// Matches the first unlabeled estimate against the first actual record.
// Returns (nil, false) if no complete estimate+actual pair exists.
func (t *Tracker) Drift(requestID string) (*Drift, bool)

// Drifts returns drift for all requests with a complete pair,
// ordered by Actual.RecordedAt.
func (t *Tracker) Drifts() []Drift

// DriftStats returns aggregate statistics across all matched pairs.
// Returns zero-value DriftStats when no pairs exist.
func (t *Tracker) DriftStats() DriftStats
```

Drift is computed **lazily** — the Tracker stores no extra state. `Drift()` and
`Drifts()` scan the existing record lists. No mutation of stored records.

**`Result.Drift()` convenience method:**

```go
// Drift returns the input-token drift for this request.
// Computed from the first unlabeled TokenEstimate and the first UsageRecord.
// Returns nil if no estimate was received.
func (r *result) Drift() *usage.Drift
```

Added to the `Result` interface:

```go
type Result interface {
    Response
    Next()           msg.Messages
    UsageRecords()   []usage.Record
    TokenEstimates() []usage.Record
    Drift()          *usage.Drift   // nil if no estimate received
}
```

This lets llmcli verbose output replace its ad-hoc drift computation with
`result.Drift().InputPct` and removes the need for any manual calculation.
---

### Ollama fix

`streamChunk` gains the missing fields:

```go
type streamChunk struct {
    Message         struct{ ... } `json:"message"`
    Done            bool          `json:"done"`
    DoneReason      string        `json:"done_reason,omitempty"`
    PromptEvalCount int           `json:"prompt_eval_count"` // NEW
    EvalCount       int           `json:"eval_count"`         // NEW
}
```

On `chunk.Done`, build a proper record:

```go
rec := usage.Record{
    RecordedAt: time.Now(),
    Dims:       usage.Dims{Provider: llm.ProviderNameOllama, Model: meta.ResolvedModel},
    Tokens: usage.TokenItems{
        {Kind: usage.KindInput,  Count: chunk.PromptEvalCount},
        {Kind: usage.KindOutput, Count: chunk.EvalCount},
    },
    // No Cost: local model, no pricing entry in KnownPricing.
    // A Tracker with usage.Default() may enrich this if the model
    // happens to appear in models.dev.
}
pub.UsageRecord(rec)
```

Cost may later be enriched by a Tracker with a `ModelDBCalculator` (if the Ollama model
name appears in models.dev) or a user-supplied calculator.

---

### `llmcli` simplification

Remove the manual `CountTokens` block (~15 lines). Replace with:

```go
proc = proc.OnEvent(llm.TypedEventHandler[*llm.TokenEstimateEvent](func(ev *llm.TokenEstimateEvent) {
    if verbose {
        tokenEstimate = &ev.Estimate
        printTokenEstimate(&ev.Estimate)
    }
}))
```

`printTokenEstimate` receives `*usage.Record` (not `*tokencount.TokenCount`).
Drift display uses `result.Drift().InputPct` directly — no manual delta computation.

---

## Files to Modify

| File | Change |
|---|---|
| `usage/record.go` | **NEW** — `TokenKind`, `TokenItem`, `TokenItems`, `Cost`, `Dims` (with `Labels`), `Record`, `CalcCost` |
| `usage/pricing.go` | **NEW** — `Pricing`, `PricingEntry`, `KnownPricing`; `Static`, `ModelDB`, `Compose`, `Default` |
| `usage/calculator.go` | **NEW** — `CostCalculator`, `CostCalculatorFunc` |
| `usage/drift.go` | **NEW** — `Drift`, `DriftStats` |
| `usage/tracker.go` | **NEW** — `Tracker`, `FilterFunc` helpers, `TrackerOption`, `Drift`/`Drifts`/`DriftStats` methods |
| `usage/budget.go` | **NEW** — `Budget` |
| `model.go` | Add `Pricing *usage.Pricing` to `Model`; add `ModelListCalculator(models Models) usage.CostCalculator` |
| `provider.go` (or new `cost.go`) | Add `CostCalculatorProvider` optional interface |
| `event.go` | Add `StreamEventTokenEstimate`, `TokenEstimateEvent`; **replace** `UsageUpdatedEvent.Usage Usage` with `UsageUpdatedEvent.Record usage.Record`; **remove** `Usage(u Usage)` from `Publisher`; add `UsageRecord(r usage.Record)` and `TokenEstimate(r usage.Record)` |
| `event_publisher.go` | Implement `UsageRecord(r)` and `TokenEstimate(r)`; **delete** `Usage(u Usage)` |
| `event_processor.go` | **Delete** `usage *Usage`; add `usageRecords []usage.Record` + `estimateRecs []usage.Record`; handle both event types; add `UsageRecords()`, `TokenEstimates()`, `Drift()` to `Result` |
| `response.go` | **Remove** `Usage() *Usage` from `Response` interface |
| `usage.go` | **Delete entire file** — `llm.Usage` type is removed |
| `llmtest/events.go` | Add `TokenEstimateEvent(r usage.Record) llm.Event` |
| `provider/anthropic/models.go` | **Remove** `modelPricingRegistry`, `pricingPrefixes`, `modelPricing`, `FillCost`, `CalculateCost` — all pricing migrates to `usage/pricing.go` |
| `provider/anthropic/stream_processor.go` | Build `TokenItems` from API response fields; call `usage.Default().Calculate()` for cost; populate `Extras["rate_limits"]`; call `pub.UsageRecord(rec)` |
| `provider/anthropic/anthropic.go` | Add `CostCalculator() usage.CostCalculator { return usage.Default() }`; emit `TokenEstimateEvent` |
| `provider/anthropic/claude/provider.go` | Same as above |
| `provider/bedrock/models.go` | **Remove** internal pricing struct; prices migrate to `usage/pricing.go` |
| `provider/bedrock/bedrock.go` | Build `TokenItems`; call `usage.Default().Calculate()`; emit `UsageRecord`; add `CostCalculator()` |
| `provider/openai/models.go` | **Remove** internal pricing struct; prices migrate to `usage/pricing.go` |
| `provider/openai/api_completions.go` | Build `TokenItems`; call `usage.Default().Calculate()`; emit `UsageRecord` |
| `provider/openai/api_responses.go` | Same as above |
| `provider/openai/openai.go` | Add `CostCalculator()`; emit `TokenEstimateEvent` |
| `provider/minimax/models.go` | **Remove** internal pricing; prices migrate to `usage/pricing.go` |
| `provider/minimax/minimax.go` | Build `TokenItems`; emit `UsageRecord`; add `CostCalculator()` |
| `provider/openrouter/openrouter.go` | Build `TokenItems`; emit `UsageRecord` with `Cost.Source="reported"`; emit `TokenEstimateEvent` (already implements `TokenCounter`) |
| `provider/ollama/ollama.go` | Add `PromptEvalCount`/`EvalCount` to `streamChunk`; build `TokenItems`; emit `UsageRecord`; emit `TokenEstimateEvent` (already implements `TokenCounter`) |
| `provider/router/router.go` | Pass through `CostCalculatorProvider` if underlying provider implements it |
| `cmd/llmcli/cmds/infer.go` | Remove manual `CountTokens` block; subscribe to `TokenEstimateEvent`; pass `*usage.Record` to print helpers |

---

## Acceptance Criteria

**Token kinds and items**
1. `TokenKind` constants `KindInput`, `KindOutput`, `KindReasoning`, `KindCacheRead`, `KindCacheWrite` exist in `usage/record.go`.
2. `TokenItem` has `Kind TokenKind` and `Count int` only. No `Labels` field on `TokenItem`.
3. `TokenItems.Count(kind)` returns that item's count or 0. `NonZero()`, `TotalInput()`, `TotalOutput()`, `Total()` work correctly. Each kind appears at most once per record.
4. No-overlap invariant: `Count(KindOutput) + Count(KindReasoning) == total_completion_tokens`; `Count(KindInput) + Count(KindCacheRead) + Count(KindCacheWrite) == total_input_tokens`.
5. OpenAI o-series emits `KindReasoning` with `Count = reasoning_tokens` and `KindOutput = completion_tokens - reasoning_tokens`. All other providers omit `KindReasoning`.
6. Token items are only present when the provider actually reported that category. No zero-count items in emitted records.

**Dims and Labels**
7. `usage.Dims` has a `Labels map[string]string` field. Provider-reported records have nil `Labels`.
8. `ByLabel(key, value string) FilterFunc` returns records where `Dims.Labels[key] == value`.
9. An estimate breakdown is expressed as multiple `Record`s sharing `RequestID`, each with distinct `Dims.Labels`. `Result.TokenEstimates()` returns them all in order.

**Pricing and cost**
10. No provider file contains a pricing map, pricing struct, or cost formula. All such code is deleted.
11. `usage/pricing.go` contains `KnownPricing []PricingEntry` with entries for all models previously in the four provider `models.go` files.
12. `Static()`, `ModelDB()`, `Compose(...)`, `Default()` exist and implement `CostCalculator`. `Default()` returns `Compose(Static(), ModelDB())`.
13. `CalcCost(items TokenItems, p Pricing) Cost` is the only cost function. `Pricing.Reasoning` falls back to `Output` rate when zero. `Cost.Reasoning` is populated when `KindReasoning` is present.
14. `CostCalculator` interface and `CostCalculatorFunc` exist in `usage/calculator.go`.
15. `llm.Model.Pricing *usage.Pricing` exists; nil on static model lists, populated by dynamic `FetchModels`.
16. `llm.ModelListCalculator(models Models) usage.CostCalculator` exists in `model.go`.
17. `CostCalculatorProvider` optional interface implemented by Anthropic, Claude-OAuth, Bedrock, OpenAI, MiniMax.

**Estimates — all seven providers**
18. All seven providers emit `TokenEstimateEvent` in `CreateStream` when `CountTokens` succeeds: Anthropic, Claude-OAuth, Bedrock, OpenAI, MiniMax, OpenRouter, Ollama.
19. OpenRouter emits an estimate (BPE-based) in addition to its reported-cost actual.
20. Ollama emits an estimate (BPE-based) in addition to fixing its empty-usage bug.

**Provider-reported usage**
21. OpenRouter actual records: `Cost.Source = "reported"`, non-zero `Cost.Total`. Tracker does not recalculate.
22. Ollama actual records: `TokenItems` with correct non-zero `KindInput` and `KindOutput`.
23. Anthropic/Claude-OAuth actual records carry `Extras["rate_limits"] = *llm.RateLimits` when response headers are present; nil when absent.

**Events and processor**
24. `llm.Usage` type does not exist. `usage.go` is deleted.
25. `UsageUpdatedEvent` has only `Record usage.Record`. No legacy `Usage` field.
26. `Publisher` has no `Usage(u Usage)` method; only `UsageRecord(r usage.Record)` and `TokenEstimate(r usage.Record)`.
27. `Response` and `Result` have no `Usage() *Usage` method.
28. `EventProcessor.applyUsage` appends all records. Two `UsageUpdatedEvent`s → `len(result.UsageRecords()) == 2`.
29. `Result` interface: `UsageRecords() []usage.Record`, `TokenEstimates() []usage.Record`, `Drift() *usage.Drift`.

**Drift**
30. `usage.Drift` and `usage.DriftStats` exist in `usage/drift.go`.
31. `Result.Drift()` is non-nil when a `TokenEstimateEvent` was received. `InputDelta == ActualInput - EstimatedInput`. `InputPct == InputDelta / EstimatedInput * 100`. Returns nil when no estimate received.
32. `Tracker.Drift(requestID)`, `Tracker.Drifts()`, `Tracker.DriftStats()` are all lazy (no stored drift state; computed from existing records on call).
33. `Tracker.DriftStats().N` equals the count of requestIDs with both an unlabeled estimate and an actual record.
34. `llmcli infer -v` uses `result.Drift()` for drift display. The manual delta computation is deleted.

**Tracker and budget**
35. `Tracker.Record(r)` enriches `Cost.IsZero()` records (excluding `Source="reported"`) using the configured calculator.
36. `Budget.Exceeded(agg Record)` returns true when any non-zero limit is breached.
37. `Tracker.Aggregate()` sums `Tokens` and `Cost` only; `Extras` is never inspected or merged.

**Build**
38. `go build ./...` succeeds with zero errors.
39. `go vet ./...` reports no issues.

---

## Out of Scope

- Streaming mid-response cost accrual (cost is reported at end-of-stream only).
- Persistent storage for the Tracker (in-memory only; callers persist via `Records()`).
- Push-based budget callbacks (`WithinBudget()` is a pull predicate).
- Per-token streaming cost estimation.
- OpenRouter cost breakdown (API reports total only).
- Populating `llm.Model.Pricing` in static model lists (only populated by dynamic `FetchModels`).
- `KindReasoning` pricing differentiation (all current providers charge reasoning at the output rate; `Pricing.Reasoning` field is ready for when that changes).

---

## ChatGPT / Codex Provider — Usage Tracking Gap & Resolution

**Date added**: 2026-04-14 (post-implementation audit)

### Problem

The `CodexAuth.NewProvider()` path routes requests to `https://chatgpt.com/backend-api/codex/responses`
using a ChatGPT Plus OAuth token. This provider uses the same `*openai.Provider` struct as the
regular OpenAI API provider, but both `streamCompletions` and `streamResponses` hardcoded
`Provider: llm.ProviderNameOpenAI` in usage record `Dims`. This caused two issues:

1. **Attribution ambiguity** — usage records from the Codex backend were indistinguishable
   from records from the regular OpenAI API key provider in the Tracker.

2. **Router clash** — `WithCodexLocal()` previously registered the provider with
   `name: "openai"` and `providerType: "openai"`. If both Codex and a regular API key were
   active simultaneously, the deduplication logic silently renamed one to `"openai-2"`,
   creating confusing model paths.

3. **Model over-exposure** — the Codex provider returned all 30+ OpenAI models (GPT-4o,
   GPT-4.1, o-series, etc.) even though the `chatgpt.com/backend-api/codex/responses` endpoint
   only accepts Codex-category models.

### Resolution (implemented)

#### 1. Distinct provider name: `"chatgpt"`

- Added `llm.ProviderNameChatGPT = "chatgpt"` to `errors.go`.
- Added `auto.ProviderChatGPT = "chatgpt"` to `provider/auto/constants.go`.
- `WithCodexLocal()` now registers with `name: ProviderChatGPT, providerType: ProviderChatGPT`.
- Model paths are now `chatgpt/gpt-5.3-codex` (not `openai/gpt-5.3-codex`), cleanly separated.

#### 2. Codex-only model list

- Added `codexModelsOnly bool` field to `openai.Provider`.
- Added `WithCodexModels()` method that returns a filtered copy.
- `CodexAuth.NewProvider()` sets `codexModelsOnly = true` so `Models()` returns only
  `categoryCodex` models — the ones actually supported by the backend.
- Added `openai.CodexModelAliases` (codex/mini only) used by the chatgpt provider
  instead of the full `ModelAliases`.

#### 3. Provider-aware usage records

- Added `providerName string` field to `ccStreamMeta` (shared by `respStreamMeta` via type alias).
- Added `provider()` helper that returns `llm.ProviderNameOpenAI` when empty.
- `streamCompletions` and `streamResponses` now pass `p.Name()` as `providerName` in the meta.
- Usage record `Dims.Provider` and `usage.Default().Calculate(provider, ...)` now use
  `meta.provider()` instead of the hardcoded `llm.ProviderNameOpenAI`.
- Token estimate records also use `p.Name()` for `Dims.Provider`.

#### 4. Pricing entries for `"chatgpt"` provider

Added to `usage/pricing.go` under a `"chatgpt"` provider key:

```
gpt-5.3-codex   Input: $1.75   Output: $14.00   CachedInput: $0.175
gpt-5.2-codex   Input: $1.75   Output: $14.00   CachedInput: $0.175
gpt-5.1-codex   Input: $1.25   Output: $10.00   CachedInput: $0.125
gpt-5.1-codex-max  Input: $1.25  Output: $10.00  CachedInput: $0.125
gpt-5.1-codex-mini Input: $0.25  Output: $2.00   CachedInput: $0.025
gpt-5-codex     Input: $1.25   Output: $10.00   CachedInput: $0.125
```

Pricing mirrors the OpenAI entries for the same model IDs; a separate key allows
per-source attribution in the Tracker.

#### 5. `codex` global alias wired to ChatGPT

- `providerAliasModels` in `aliases.go` now includes `ProviderChatGPT` with
  `fast: gpt-5.1-codex-mini`, `normal/powerful/codex: gpt-5.3-codex`.
- `buildAliasTargets` wires `AliasCodex` when `models.codex != ""`.
  Previously `AliasCodex` was initialised but never populated.

### Provider name in error messages

Error constructors (`NewErrMissingAPIKey`, `NewErrAPIError`, etc.) still use
`llm.ProviderNameOpenAI`. This is intentional: these errors describe the underlying
API endpoint type, not the router-level instance. A future pass may parameterise these
if per-instance error attribution becomes important.
