# Design: Model Registry and Provider Offerings

> **Scope**: Rich model identity, capabilities, provider offerings, smart routing.
> **Out of scope**: API client extraction (see DESIGN-api-extraction.md).
> **Depends on**: Can be done independently of or after the API extraction.

> **Status note**: This design predates the final `catalog` package shape. The
> current in-repo implementation lives under `catalog/`, is being shaped for
> future extraction to `codewandler/modeldb`, and uses a `modelsdev` source
> adapter for the `models.dev` upstream feed.

---

## Problem

### 1. `llm.Model` conflates identity with offering

```go
// Current — one type tries to be both
type Model struct {
    ID       string         // "claude-sonnet-4-6" or "openrouter/anthropic/claude-sonnet-4-6"?
    Name     string
    Provider string         // ← offering concern, not identity
    Aliases  []string       // ← provider-local aliases
    Pricing  *usage.Pricing // ← provider-specific pricing
}
```

There's no way to express: "claude-sonnet-4-6 exists as a model, and Anthropic serves it
at $3/MTok, OpenRouter at $3.3/MTok, and Bedrock at $3/MTok with AWS auth."

### 2. No model capabilities

The router can't answer "give me any model with tool_use + ≥128k context + reasoning"
because `llm.Model` has no capability fields. Each provider hardcodes model-specific
logic (e.g. `openai.modelCategory`, `anthropic.isAdaptiveThinkingSupported`).

### 3. Model addressing is ambiguous

Models are addressed as `<provider>/<model_id>` in our router, but:
- OpenRouter uses `<creator>/<model>` (e.g. `anthropic/claude-sonnet-4-6`)
- Our router uses `<provider_instance>/<provider_type>/<model>` (e.g. `work-claude/anthropic/claude-sonnet`)
- Nested routing: `openrouter/openai/gpt-5.3` means "ask our openrouter provider to use the openai/gpt-5.3 model"
- OpenRouter variants: `openai/gpt-5.2:nitro`, `anthropic/claude-sonnet-4.5:free`

These are different namespaces conflated into a single string.

### 4. Provider-specific model metadata is scattered

Each provider maintains its own model registry:
- `provider/openai/models.go`: `modelRegistry` with `modelCategory`, `UseResponsesAPI`, `SupportsExtendedCache`
- `provider/anthropic/models.go`: `allModelsWithAliases` with flat list
- `provider/anthropic/request.go`: `isAdaptiveThinkingSupported()`, `isEffortSupported()` — capability checks by string matching
- `usage/pricing.go`: `KnownPricing` — a flat list of provider+model+pricing triples
- `catalog/`: Rich canonical model graph and source adapters, only partly wired
  into provider selection policy

---

## Key Concepts

### Model (the LLM itself)

**What it is** — independent of any provider:
- Canonical ID: `claude-sonnet-4-6`, `gpt-5.4`, `llama-4-maverick-17b`
- Creator: `anthropic`, `openai`, `meta`
- Family: `claude-4.6`, `gpt-5.4`, `llama-4`
- Capabilities: tool_use, reasoning, vision, structured_output, caching, interleaved thinking
- Limits: context window, max output tokens
- Modalities: text/image/audio in, text/image out
- Lifecycle: release date, knowledge cutoff, deprecated
- Reference pricing (MSRP from the creator)

### Offering (how a provider serves the model)

**What it is** — provider-specific:
- Which provider: `anthropic`, `openrouter`, `bedrock`
- Which model (canonical ID): `claude-sonnet-4-6`
- Provider's model ID (may differ): `anthropic/claude-sonnet-4-6` (OpenRouter), `anthropic.claude-sonnet-4-6-v1:0` (Bedrock)
- Which API types: `[messages]`, `[completions, responses]`, `[completions, messages]`
- Pricing override: provider may charge different rates
- Limits override: provider may reduce context window
- Quantization: provider may run a quantized version
- Availability: currently accepting requests

### Variant (a routing/parameter hint, NOT a different model)

Based on OpenRouter's system, variants are **not separate models**. They're syntactic
sugar for routing preferences and parameter overrides:

| Variant | Meaning | Equivalent |
|---------|---------|------------|
| `:free` | Free tier, different rate limits | Routing: free endpoints only |
| `:extended` | Extended context window | Routing: extended-context endpoints |
| `:nitro` | Max throughput | `provider.sort = "throughput"` |
| `:floor` | Lowest price | `provider.sort = "price"` |
| `:exacto` | Quality-first tool calling | `provider.sort = "exacto"` |
| `:thinking` | Enable reasoning | Parameter: reasoning enabled |

**Design decision**: Variants are parsed at the routing layer, not modeled as separate
entries in the model registry. The router strips the variant suffix and applies the
corresponding routing hint or parameter override.

---

## Type Design

### `model/model.go` — Canonical Model Identity

```go
package model

import "time"

// ID is a canonical model identifier: "claude-sonnet-4-6", "gpt-5.4".
type ID string

// Family groups related models: "claude-4.6", "gpt-5", "llama-4".
type Family string

// Creator identifies who trained the model: "anthropic", "openai", "meta".
type Creator string

// Modality describes an input or output modality.
type Modality string

const (
    ModalityText  Modality = "text"
    ModalityImage Modality = "image"
    ModalityAudio Modality = "audio"
    ModalityVideo Modality = "video"
)

// Capabilities describes what a model can do — independent of any provider.
type Capabilities struct {
    Reasoning           bool `json:"reasoning"`
    ToolUse             bool `json:"tool_use"`
    StructuredOutput    bool `json:"structured_output"`
    Vision              bool `json:"vision"`
    Streaming           bool `json:"streaming"`
    Caching             bool `json:"caching"`
    InterleavedThinking bool `json:"interleaved_thinking,omitempty"`
    AdaptiveThinking    bool `json:"adaptive_thinking,omitempty"`
    CodeExecution       bool `json:"code_execution,omitempty"`
    // Temperature indicates whether the model supports temperature control.
    Temperature         bool `json:"temperature,omitempty"`
}

// Limits describes the model's token limits.
type Limits struct {
    ContextWindow int `json:"context_window"` // max input tokens
    MaxOutput     int `json:"max_output"`     // max output tokens
}

// Pricing is per-million-token USD pricing.
type Pricing struct {
    Input       float64 `json:"input"`
    Output      float64 `json:"output"`
    CachedInput float64 `json:"cached_input,omitempty"`
    CacheWrite  float64 `json:"cache_write,omitempty"`
    Reasoning   float64 `json:"reasoning,omitempty"` // 0 = same as output
}

// Model is the canonical identity of an LLM — what it IS, not how to reach it.
type Model struct {
    ID      ID      `json:"id"`
    Name    string  `json:"name"`
    Family  Family  `json:"family"`
    Creator Creator `json:"creator"`

    Capabilities Capabilities `json:"capabilities"`
    Limits       Limits       `json:"limits"`

    InputModalities  []Modality `json:"input_modalities"`
    OutputModalities []Modality `json:"output_modalities"`

    ReleaseDate     *time.Time `json:"release_date,omitempty"`
    KnowledgeCutoff *time.Time `json:"knowledge_cutoff,omitempty"`
    Deprecated      bool       `json:"deprecated,omitempty"`
    OpenWeights     bool       `json:"open_weights,omitempty"`

    // ReferencePricing is the MSRP from the model creator.
    // Individual providers may override this in their offerings.
    ReferencePricing *Pricing `json:"reference_pricing,omitempty"`
}
```

### `model/registry.go` — Queryable Model Database

```go
package model

// Registry is a queryable collection of canonical models.
type Registry struct {
    models map[ID]*Model
}

func NewRegistry(models ...*Model) *Registry { ... }

// Get returns a model by canonical ID.
func (r *Registry) Get(id ID) (*Model, bool) { ... }

// Search returns models matching the given criteria.
func (r *Registry) Search(q Query) []*Model { ... }

// All returns all registered models.
func (r *Registry) All() []*Model { ... }

// Query describes search criteria for models.
type Query struct {
    Creator            Creator  `json:"creator,omitempty"`
    Family             Family   `json:"family,omitempty"`
    MinContext          int      `json:"min_context,omitempty"`
    RequireToolUse     bool     `json:"require_tool_use,omitempty"`
    RequireVision      bool     `json:"require_vision,omitempty"`
    RequireReasoning   bool     `json:"require_reasoning,omitempty"`
    RequireCaching     bool     `json:"require_caching,omitempty"`
    ExcludeDeprecated  bool     `json:"exclude_deprecated,omitempty"`
    InputModality      Modality `json:"input_modality,omitempty"`
    OutputModality     Modality `json:"output_modality,omitempty"`
}
```

### `model/seed.go` — Seeding from catalog/modelsdev + hardcoded

```go
package model

import "github.com/codewandler/llm/catalog"

// DefaultRegistry returns a Registry populated from all known sources:
// 1. Embedded catalog snapshot built from curated sources + models.dev
// 2. Hardcoded additions for models not yet in models.dev
func DefaultRegistry() *Registry { ... }

// FromModelsDev converts a models.dev-backed catalog fragment to a list of Models.
func FromModelsDev(c catalog.Catalog) []*Model { ... }

// fromModelsDevModel converts a single models.dev-derived record to our canonical Model.
func fromModelsDevModel(providerID string, m catalog.ModelRecord) *Model {
    return &Model{
        ID:      toCanonicalID(m.Key),
        Name:    m.Name,
        Family:  Family(m.Key.Family),
        Creator: Creator(providerID),
        Capabilities: Capabilities{
            Reasoning:        m.Capabilities.Reasoning,
            ToolUse:          m.Capabilities.ToolUse,
            StructuredOutput: m.Capabilities.StructuredOutput,
            Temperature:      m.Capabilities.Temperature,
            Vision:           containsModality(m.InputModalities, "image"),
        },
        Limits: Limits{
            ContextWindow: m.Limits.ContextWindow,
            MaxOutput:     m.Limits.MaxOutput,
        },
        InputModalities:  toModalities(m.InputModalities),
        OutputModalities: toModalities(m.OutputModalities),
        OpenWeights:      m.OpenWeights,
        ReferencePricing: toPricing(m.ReferencePricing),
    }
}
```

---

## Offering Type

```go
// In root llm package or in a new offering/ package

// Offering describes how a specific provider serves a specific model.
type Offering struct {
    // Which provider (our internal name: "anthropic", "openrouter", "bedrock")
    ProviderName string `json:"provider_name"`

    // Canonical model ID (matches model.Model.ID)
    ModelID model.ID `json:"model_id"`

    // Provider's own ID for this model (sent on the wire)
    // e.g. "anthropic/claude-sonnet-4-6" (OpenRouter),
    //      "anthropic.claude-sonnet-4-6-v1:0" (Bedrock),
    //      "claude-sonnet-4-6" (direct Anthropic)
    ProviderModelID string `json:"provider_model_id"`

    // API types available for this model from this provider
    APITypes []ApiType `json:"api_types"`

    // Provider-specific overrides (nil = use canonical/reference values)
    Pricing        *model.Pricing `json:"pricing,omitempty"`
    LimitsOverride *model.Limits  `json:"limits,omitempty"`

    // Metadata
    Quantized bool     `json:"quantized,omitempty"`
    Aliases   []string `json:"aliases,omitempty"`
    Available bool     `json:"available"`
}
```

---

## Model Addressing: A Layered Namespace

The key insight: model addressing has **layers**, and each layer resolves one level:

```
User says:    "openrouter/openai/gpt-5.3:nitro"
                  │          │         │    │
Layer 1 (router): │          └─────────┴────┤
  Our provider instance: "openrouter"       │
  Pass to OpenRouter:    "openai/gpt-5.3:nitro"
                                            │
Layer 2 (OpenRouter):                       │
  Variant parsed:        ":nitro" → sort=throughput
  Creator namespace:     "openai"
  Model at OpenRouter:   "gpt-5.3"
  Canonical model:       model.ID("gpt-5.3")
```

**Our router** resolves the first slash-separated component as the provider instance.
Everything after it is opaque to our router and passed verbatim to the provider's
`CreateStream()`. The provider then does its own resolution.

**Variant parsing** happens at two possible levels:
1. **OpenRouter-level**: They handle `:nitro`, `:free`, etc. natively.
2. **Our router-level**: We could parse variants from the model string and translate
   them to `llm.Request` fields (e.g. `:thinking` → `Thinking: ThinkingOn`).
   This is optional and provider-agnostic.

### Proposed Variant Parsing (our router)

```go
// model/variant.go

// Variant represents a routing/parameter hint suffix.
type Variant string

const (
    VariantFree      Variant = "free"
    VariantExtended  Variant = "extended"
    VariantNitro     Variant = "nitro"
    VariantFloor     Variant = "floor"
    VariantExacto    Variant = "exacto"
    VariantThinking  Variant = "thinking"
    VariantOnline    Variant = "online"  // deprecated at OpenRouter
)

// ParseModelAndVariant splits "openai/gpt-5.2:nitro" into ("openai/gpt-5.2", "nitro").
func ParseModelAndVariant(s string) (modelID string, variant Variant, ok bool) {
    if i := strings.LastIndexByte(s, ':'); i > 0 {
        return s[:i], Variant(s[i+1:]), true
    }
    return s, "", false
}
```

The router can then apply variant effects:
```go
modelID, variant, _ := model.ParseModelAndVariant(requested)
switch variant {
case model.VariantThinking:
    req.Thinking = llm.ThinkingOn
case model.VariantNitro:
    routeOpts.Strategy = StrategyCheapest // or fastest
// For OpenRouter, pass through as-is — they handle it natively
}
```

---

## Provider-Specific Model Knowledge

Some providers need model-specific behavior that goes beyond the canonical model:

| Provider | Model-specific need | Where it lives |
|---|---|---|
| OpenAI | `UseResponsesAPI` routing | `provider/openai/models.go` — stays there as provider concern |
| OpenAI | `modelCategory` for effort mapping | Could move to `model.Capabilities` fields |
| Anthropic | `isAdaptiveThinkingSupported` | Becomes `model.Capabilities.AdaptiveThinking` |
| Anthropic | `isEffortSupported` | Becomes a new capability field on the model |
| Bedrock | `cacheHint` per-message handling | Provider-specific, stays in `provider/bedrock` |

The goal is to push as much as possible into the canonical model (capabilities), and keep
only genuinely provider-specific routing logic in the provider package.

---

## Smart Router Enhancement

With offerings and capabilities, the router can answer complex queries:

```go
// RouteRequest describes what the caller needs.
type RouteRequest struct {
    // Specific model request (may include provider prefix and/or variant)
    Model string

    // Capability requirements (when Model is empty or as additional filter)
    Require model.Query

    // Routing strategy
    Strategy RouteStrategy

    // Provider constraints
    ExcludeProviders []string
    OnlyProviders    []string
}

type RouteStrategy string

const (
    StrategyCheapest  RouteStrategy = "cheapest"   // by offering pricing
    StrategyFastest   RouteStrategy = "fastest"     // by observed latency
    StrategyPreferred RouteStrategy = "preferred"   // user-defined priority order
    StrategyFailover  RouteStrategy = "failover"    // try in order, skip on error
)
```

Example flows:

```go
// "Give me claude-sonnet-4-6, cheapest provider"
router.Route(RouteRequest{
    Model:    "claude-sonnet-4-6",
    Strategy: StrategyCheapest,
})
// → Compares offerings from Anthropic, OpenRouter, Bedrock → picks cheapest

// "Give me any model with tool_use + reasoning + ≥128k context"
router.Route(RouteRequest{
    Require: model.Query{
        MinContext:        128_000,
        RequireToolUse:   true,
        RequireReasoning: true,
    },
    Strategy: StrategyCheapest,
})
// → Searches model registry, finds matching models, compares offerings

// "Give me gpt-5.3-codex via OpenRouter with nitro"
router.Route(RouteRequest{
    Model:         "openrouter/openai/gpt-5.3-codex:nitro",
    Strategy:      StrategyPreferred,
})
// → Resolved: provider=openrouter, upstream model=openai/gpt-5.3-codex, variant=nitro
```

---

## Compatibility with `llm.Model`

The existing `llm.Model` type is in the public API. During migration:

```go
// model/compat.go

// ToLLMModel converts a canonical Model + Offering to the existing llm.Model format.
// Used for backward compatibility with Models() and Resolve().
func ToLLMModel(m *Model, o *Offering) llm.Model {
    pricing := m.ReferencePricing
    if o != nil && o.Pricing != nil {
        pricing = o.Pricing
    }
    return llm.Model{
        ID:       string(m.ID),
        Name:     m.Name,
        Provider: o.ProviderName,
        Aliases:  o.Aliases,
        Pricing:  toUsagePricing(pricing),
    }
}
```

Providers can implement both `Models()` (old) and `Offerings()` (new) during the
transition. The router uses `Offerings()` when available.

---

## Migration Plan

### Phase 1: `model/` package with types (additive)

1. Create `model/model.go` with `Model`, `Capabilities`, `Limits`, `Pricing`.
2. Create `model/registry.go` with `Registry` and `Query`.
3. Create `model/seed.go` with `FromModelsDev()` / catalog integration.
4. Create `model/variant.go` with variant parsing.
5. Write tests: registry search, modelsdev/catalog conversion, variant parsing.

### Phase 2: Populate capabilities from existing hardcoded logic

6. Migrate `provider/openai/models.go` model metadata → canonical `Model` entries.
7. Migrate `provider/anthropic` capability checks → `Capabilities` fields.
8. Ensure `model.DefaultRegistry()` covers all models known to all providers.

### Phase 3: Offering type + provider integration

9. Add `Offering` type to `llm` (or `model/` or new `offering/` package).
10. Add `Offerings()` method to providers (alongside existing `Models()`).
11. Move pricing from `usage/KnownPricing` into offerings.

### Phase 4: Smart router

12. Router uses `model.Registry` for capability queries.
13. Router uses `Offering` for cost-based routing.
14. Variant parsing at router level.
15. Deprecate `llm.Model.Provider` (provider is on the offering, not the model).

---

## Open Questions

1. **Package location of `Offering`**: Root `llm` package? `model/` package? New
   `offering/` package? The Offering type references both `model.ID` and provider
   names, so it sits between domains. Leaning toward root `llm` since it's part of
   the Provider interface.

2. **Model ID authority**: Creator is source of truth for canonical IDs. But what
   about versioned IDs? Anthropic uses date suffixes (`claude-sonnet-4-5-20250929`).
   Should the canonical ID be the versionless form (`claude-sonnet-4-5`) with dated
   variants as separate entries? Or keep dated IDs as canonical?

3. **How to handle OpenRouter's `:free` tier**: Is it a separate offering with
   different pricing (price = $0), or a variant hint that the provider handles
   internally? Probably: if we know the pricing is different, it's a separate offering
   keyed as `provider_model_id: "anthropic/claude-sonnet-4.5:free"`.

4. **Dynamic model discovery**: OpenRouter's `/v1/models` returns thousands of models.
   Do we populate the model registry from that, or keep it curated? Probably: curated
   built-in registry + dynamic overlay from `FetchModels()` / `FetchOfferings()`.
