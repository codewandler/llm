# Design: ops package — parameterised LLM operation layer

**Date**: 2026-04-14  
**Status**: Draft

---

## Problem

Using `Provider.CreateStream` for higher-level tasks (classify text, map to struct,
detect intent) requires assembling `Request` structs, wiring tools, handling streams,
and extracting results every time. There is no reusable pattern for defining a
self-contained, parameterisable operation.

---

## Core Model

Three-type parameterisation, inspired by `tool.Spec`:

```
Factory[F, I, O]  ──.New(provider, F)──▶  Operation[I, O]  ──.Run(ctx, I)──▶  (*O, error)
```

| Symbol | Meaning | Set when |
|--------|---------|----------|
| `F`    | Factory params — static configuration | Wiring / app startup |
| `I`    | Runtime input | Each call |
| `O`    | Runtime output | Each call |

**Factory** knows the algorithm (e.g. "classify text into labels").  
**Factory params F** configure the instance (e.g. which labels, which model).  
**Operation** is a live, ready-to-call object bound to a provider + params.

This lets downstream code store `Operation[string, InvoiceData]` without caring
whether it is backed by tool-forcing, JSON mode, or a mock.

---

## Package

```
github.com/codewandler/llm/ops
```

Files:

```
ops/
  ops.go        # Factory[F,I,O], Operation[I,O], helpers
  map.go        # Map — generic text→struct extraction (tool + JSON strategies)
  classify.go   # Classify — text → one-of-N label
  intent.go     # Intent — text → intent name, with per-intent examples
  generate.go   # Generate — text prompt → free-form text
  ops_test.go   # table-driven tests (all four ops)
```

---

## Interfaces and helpers (`ops.go`)

```go
// Factory[F, I, O] creates Operations parameterised by F.
type Factory[F, I, O any] interface {
    New(provider llm.Provider, params F) Operation[I, O]
}

// Operation[I, O] executes a single LLM-backed operation.
type Operation[I, O any] interface {
    Run(ctx context.Context, input I) (*O, error)
}

// NewFactory wraps a function into a Factory — the primary helper for
// users building their own operations.
func NewFactory[F, I, O any](fn func(llm.Provider, F) Operation[I, O]) Factory[F, I, O]

// OperationFunc adapts a plain function to the Operation interface.
type OperationFunc[I, O any] func(context.Context, I) (*O, error)
func (f OperationFunc[I, O]) Run(ctx context.Context, input I) (*O, error)
```

---

## Built-in: Map (`map.go`)

Maps unstructured text to any Go struct `O`.

```go
type MapMode string
const (
    MapModeTool MapMode = "tool"  // default: tool-forced schema, more reliable
    MapModeJSON MapMode = "json"  // JSON output mode + schema system prompt
)

type MapParams struct {
    Model string  // optional; defaults to llm.ModelDefault
    Hint  string  // extra context appended to system prompt
    Mode  MapMode // default: MapModeTool
}

// MapFactory[O] implements Factory[MapParams, string, O].
// Explicit type-param form — use when storing the Factory in a variable.
type MapFactory[O any] struct{}
func (MapFactory[O]) New(provider llm.Provider, params MapParams) Operation[string, O]

// NewMap is a convenience constructor (no explicit struct needed).
func NewMap[O any](provider llm.Provider, params MapParams) Operation[string, O]
```

**Usage:**
```go
// Quick form
op := ops.NewMap[InvoiceData](provider, ops.MapParams{Hint: "Extract invoice fields."})
invoice, err := op.Run(ctx, rawText)

// Factory-interface form (for DI / testing)
var f ops.Factory[ops.MapParams, string, InvoiceData] = ops.MapFactory[InvoiceData]{}
op := f.New(provider, params)
```

---

## Built-in: Classify (`classify.go`)

Maps text to one of a fixed set of labels.

```go
type ClassifyParams struct {
    Labels []string  // at least one required
    Model  string
}

type ClassifyResult struct {
    Label string  // canonical label from Labels (case-normalised)
}

// Classify is the package-level factory singleton.
var Classify classifyFactory  // implements Factory[ClassifyParams, string, ClassifyResult]
```

**Usage:**
```go
op := ops.Classify.New(provider, ops.ClassifyParams{
    Labels: []string{"positive", "negative", "neutral"},
})
result, err := op.Run(ctx, "This is great!")
// result.Label == "positive"
```

**Building domain-specific ops on top:**
```go
var SentimentOp = ops.Classify  // reuse factory, fix params at wiring time
op := SentimentOp.New(provider, ops.ClassifyParams{Labels: []string{"pos", "neg"}})
```

---

## Built-in: Intent (`intent.go`)

Like Classify but each intent carries a description and few-shot examples —
more reliable for NLU tasks.

```go
type IntentDef struct {
    Name        string
    Description string
    Examples    []string  // example user utterances (few-shot)
}

type IntentParams struct {
    Intents []IntentDef  // at least one required
    Model   string
}

type IntentResult struct {
    Name string  // Name of the matched IntentDef
}

var Intent intentFactory  // implements Factory[IntentParams, string, IntentResult]
```

**Usage:**
```go
op := ops.Intent.New(provider, ops.IntentParams{
    Intents: []ops.IntentDef{
        {
            Name:        "book_flight",
            Description: "User wants to book a flight",
            Examples:    []string{"fly me to Paris", "book a ticket to Berlin"},
        },
        {
            Name:        "cancel_order",
            Description: "User wants to cancel an existing order",
            Examples:    []string{"cancel my order", "I want a refund"},
        },
    },
})
result, err := op.Run(ctx, "I need to get to Berlin next week")
// result.Name == "book_flight"
```

---

## Built-in: Generate (`generate.go`)

Free-form text generation.

```go
type GenerateParams struct {
    Model        string
    SystemPrompt string
    Temperature  float64  // 0 → use default (0.7)
}

type GenerateResult struct {
    Text string
}

var Generate generateFactory  // implements Factory[GenerateParams, string, GenerateResult]
```

**Usage:**
```go
op := ops.Generate.New(provider, ops.GenerateParams{
    SystemPrompt: "You are a concise technical writer.",
})
result, err := op.Run(ctx, "Explain Go channels in two sentences.")
```

---

## Custom operations (user-defined)

```go
type SummarizeParams struct {
    Language string
    MaxWords int
}

type SummaryResult struct {
    Summary   string
    KeyPoints []string
}

var Summarize = ops.NewFactory(func(p llm.Provider, params SummarizeParams) ops.Operation[string, SummaryResult] {
    return ops.NewMap[SummaryResult](p, ops.MapParams{
        Hint: fmt.Sprintf("Summarize in %s, max %d words. Extract key points.", params.Language, params.MaxWords),
    })
})

// Usage:
op := Summarize.New(provider, SummarizeParams{Language: "English", MaxWords: 100})
result, err := op.Run(ctx, emailBody)
```

---

## Internal implementation notes

All operations share a private `opRunner` helper:

```go
type opRunner struct {
    provider llm.Provider
    model    string
}

func (r *opRunner) stream(ctx context.Context, req llm.Request) (llm.Result, error) {
    if req.Model == "" {
        req.Model = llm.ModelDefault
    }
    ch, err := r.provider.CreateStream(ctx, req)
    if err != nil {
        return nil, err
    }
    res := llm.ProcessEvents(ctx, ch)
    return res, res.Error()
}
```

**Map (tool mode):**
- `tool.DefinitionFor[O]("extract", description)`
- `ToolChoice: llm.ToolChoiceTool{Name: "extract"}`
- `Temperature: 0, Thinking: ThinkingOff`
- Unmarshal `result.ToolCalls()[0].ToolArgs()` into `*O`

**Map (JSON mode):**
- `OutputFormat: llm.OutputFormatJSON`
- System prompt includes JSON schema from `tool.DefinitionFor[O]`
- `json.Unmarshal(result.Text())` into `*O`

**Classify:**
- Labels injected into system prompt as enum constraint
- `Temperature: 0, Thinking: ThinkingOff`
- Response trimmed + case-insensitively matched against labels

**Intent:**
- Each `IntentDef` rendered as: `<name>: <description>\nExamples: <...>`
- Model instructed to return only the intent name
- Validated against `IntentDef.Name` values

**Generate:**
- `Temperature: params.Temperature` (fallback 0.7)
- No tools, thinking auto

---

## Error handling

All errors wrapped: `"ops <opname>: <cause>"`.

| Condition | Error |
|-----------|-------|
| Stream error | `"ops map: <provider error>"` |
| No tool call (Map/tool) | `"ops map: model did not return structured output"` |
| JSON unmarshal failure | `"ops map: unmarshal: <err>"` |
| Unknown label (Classify) | `"ops classify: model returned unknown label %q"` |
| No labels | `"ops classify: Labels must not be empty"` |
| Unknown intent | `"ops intent: model returned unknown intent %q"` |
| No intents defined | `"ops intent: Intents must not be empty"` |

---

## Testing

- Table-driven tests using `llm.StreamFunc` + `llmtest.SendEvents`
- Each op: happy path + error paths + edge cases
- No network calls; all offline

---

## Acceptance criteria

- [ ] `ops.NewMap[T]`, `ops.Classify`, `ops.Intent`, `ops.Generate` all compile
- [ ] `NewFactory` lets users build custom factories in ~5 lines
- [ ] `OperationFunc` lets users build ad-hoc Operations without a struct
- [ ] All four built-ins pass table-driven tests with race detector
- [ ] `go vet ./...` clean
