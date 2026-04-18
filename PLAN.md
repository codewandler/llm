# Refined Plan: simplify router + auto with a provider registry, centralize modeldb access, and improve end-user ergonomics

## Executive summary

This refactor should not just simplify internals. It should make the library easier to use correctly.

Today, a lot of internal complexity leaks into the user experience indirectly:

- model naming can feel inconsistent across providers
- the difference between provider type, provider instance, and provider path is subtle
- aliases come from several places and are hard to reason about
- `auto` is powerful, but its behavior is not always obvious
- some catalog-backed behavior exists, but it is not exposed as a clear user-facing mental model

The refined goal is therefore:

- simplify internals **in a way that produces a simpler and more predictable UX**

A concise target statement:

- **catalog owns model truth**
- **registry owns provider construction**
- **auto owns composition defaults**
- **service owns resolution + failover**
- **the public API should feel predictable, discoverable, and boring**

---

# 1. UX problems the refactor should solve

These are the user-facing issues this plan should explicitly improve.

## 1.1 Too many naming concepts leak into usage

Today there are several overlapping concepts:

- provider type
- provider instance name
- model ID
- provider-prefixed model ID
- full multi-instance model path
- provider-local aliases
- top-level aliases

All of these are understandable internally, but for an end user the desired mental model should be much simpler:

- “I can select a model by a stable string”
- “I can optionally target a specific provider instance”
- “shortcuts like `fast` and `default` are predictable”

The refactor should preserve current compatibility while making the intended hierarchy much clearer.

## 1.2 `auto` should feel like the default happy path

For most users, `auto.New(...)` should be the primary entry point.

That means users should not need to understand:

- router config compilation
- factory keys
- provider build details
- where aliases came from

Instead, `auto` should feel like:

- “detect sensible providers”
- “apply my preferences”
- “give me a provider that routes predictably”

## 1.3 Alias behavior should feel intentional, not accidental

Users should be able to form reliable expectations such as:

- `fast`, `default`, `powerful` mean top-level intent aliases
- `openai/mini` means a provider-scoped alias
- `work/openai/gpt-4o` means a specific configured instance
- if something is ambiguous, the error should explain how to disambiguate it

The internals can stay flexible, but the UX should feel coherent.

## 1.4 Catalog-backed behavior should increase trust

When the system knows about:

- factual aliases
- runtime-visible models
- pricing
- tokenization family

that should improve the user experience consistently.

Users should not have to care whether this came from:

- a provider-local constant map
- the built-in catalog
- a runtime overlay

They should only notice that the library gives better defaults and better answers.

---

# 2. UX principles to optimize for

These principles should guide design decisions during the refactor.

## 2.1 Progressive disclosure

Simple use should stay simple:

```go
p, err := auto.New(ctx)
```

More advanced use should remain possible, but complexity should only appear when needed.

```go
p, err := auto.New(ctx,
    auto.WithOpenAI(),
    auto.WithClaudeAccount("work", store),
    auto.WithAlias("review", "work/claude/sonnet"),
)
```

Users should not pay an API complexity cost for advanced capabilities they are not using.

## 2.2 Predictability over cleverness

When multiple interpretations are possible, prefer behavior that is easy to explain.

For example:

- explicit configuration should beat auto-detection
- explicit aliases should beat generated convenience aliases when there is conflict
- ambiguity should produce actionable errors, not silent surprising resolution

## 2.3 Stable names over implementation details

Users should think in terms of:

- provider names they chose
- provider families they recognize
- model references they can type and remember

They should not be exposed to synthetic keys or internal construction hacks.

## 2.4 Good defaults, explicit escape hatches

Examples:

- `auto.New(ctx)` should do something useful without extra setup
- built-in aliases should work out of the box
- advanced users can still disable detection, disable providers, or force exact instance selection

## 2.5 Errors should teach the user what to do next

Especially for routing and alias resolution, error messages should answer:

- what failed?
- why was it ambiguous or unavailable?
- what exact string should the user use instead?

This is a significant ergonomics improvement even if no public API changes.

---

# 3. User-facing target mental model

After the refactor, the intended model for users should be:

## 3.1 There are three levels of model selection

### Level 1: intent aliases

Top-level aliases represent user intent:

- `fast`
- `default`
- `powerful`

These are convenience selectors chosen by composition policy.

### Level 2: provider-scoped references

Users can target a provider family directly:

- `openai/gpt-4o`
- `bedrock/claude-sonnet...`
- `openai/mini`

These are predictable and portable across most setups.

### Level 3: instance-scoped references

Users can target a specific configured instance when needed:

- `work/openai/gpt-4o`
- `personal/claude/sonnet`

This is the escape hatch for advanced routing and multi-account setups.

## 3.2 `auto` decides defaults, router preserves explicitness

- `auto` decides what providers exist and in what priority order
- `router` respects explicit references exactly
- if the user gives a short/ambiguous selector, router resolves it predictably or returns a useful error

## 3.3 Catalog knowledge improves defaults invisibly

Users should simply experience:

- more correct aliases
- more accurate model lists
- better pricing estimates
- better tokenization heuristics

without needing to know about `modeldb`.

---

# 4. End-user UX goals by subsystem

## 4.1 `auto` UX goals

`auto` should become the main ergonomic surface.

### Desired experience

- `auto.New(ctx)` gives a sensible multi-provider setup
- explicit `WithX(...)` options are intuitive
- explicit configuration composes cleanly with autodetect
- built-in aliases work consistently
- duplicate or colliding explicit instances are normalized in a deterministic way

### UX-focused behaviors

- explicit providers take precedence over detected duplicates
- instance names are stable and human-readable
- if a name is rewritten due to collision, it should use normal suffixes like `-2`, `-3`
- user-defined top-level aliases should be easy to understand and document

### What users should never need to know

- factory keys
- synthetic type-name combinations
- internal dedup tricks

## 4.2 `router` UX goals

`Service` owns runtime resolution behavior.

### Desired experience

- exact references behave exactly
- short references work when unambiguous
- ambiguous references fail with guidance
- failover order is unsurprising

### Example desirable error message style

Instead of only:

- `ambiguous model ID`

Prefer something more actionable, conceptually like:

- `model "sonnet" matched multiple targets: anthropic/sonnet, work/claude/sonnet; use a provider-prefixed or instance-prefixed model reference`

The exact wording can vary, but the standard should be “help the user correct the input immediately.”

## 4.3 catalog UX goals

The catalog layer is not user-facing directly, but it should make the UX better in visible ways.

### Desired experience

- model listings feel more complete and accurate
- aliases feel less stale
- pricing lookups feel more consistent
- local runtime providers expose visible/acquirable models more helpfully

### Example user-visible improvement

For Ollama/DockerMR, a user should experience:

- “the provider knows about installed and visible models more accurately”

not:

- “sometimes it uses curated constants, sometimes it uses catalog overlays, not sure why”

## 4.4 provider registry UX goals

The registry is internal, but it strongly affects how consistent providers feel.

### Desired experience across providers

- adding a provider does not create a one-off UX model
- provider-scoped aliases behave similarly across providers
- built-in aliases are supported deliberately, not incidentally
- docs can describe providers with one common pattern

---

# 5. Refactor goals, now framed in UX terms

## 5.1 Primary goals

1. Make `auto` the obvious and ergonomic default path
2. Make model selection strings more understandable and predictable
3. Make alias behavior easier to explain to users
4. Centralize catalog/model truth so defaults stay consistent across features
5. Reduce internal complexity that currently leaks into user-visible quirks

## 5.2 Non-goals

These should not be bundled unless needed:

- redesigning the public `llm.Provider` interface
- inventing an entirely new model reference syntax
- changing existing path semantics without compatibility support
- overhauling all docs at once

---

# 6. Target architecture, with UX intent

## 6.1 Layered design

```text
+-----------------------------+
| app code / end users        |
| - use auto.New()            |
| - pass model refs           |
+-------------+---------------+
              |
              v
+-----------------------------+
| auto               |
| - ergonomic defaults        |
| - explicit preferences      |
| - instance composition      |
+-------------+---------------+
              |
              v
+-----------------------------+
| internal/providerregistry   |
| - provider kind defs        |
| - consistent construction   |
| - consistent detection      |
| - alias metadata policy     |
+-------------+---------------+
              |
              v
+-----------------------------+
+-----------------------------+
| llm.Service                  |
| - clear resolution rules     |
| - good ambiguity errors      |
| - failover                   |
+-----------------------------+

+-----------------------------+
| internal/modelcatalog        |
| - model truth               |
| - factual aliases           |
| - pricing/profile lookups   |
| - runtime model visibility  |
+-----------------------------+
```

## 6.2 Ownership boundaries

### `internal/modelcatalog`
Owns:

- all runtime `modeldb` imports
- built-in catalog caching
- resolved catalog creation
- service/runtime projections
- pricing views
- token-profile lookup support
- provider/service canonicalization rules used for catalog work

UX reason:

- users get one consistent source of truth behind aliases, pricing, and visible models

### `internal/providerregistry`
Owns:

- provider kind definitions
- shared build logic for each provider kind
- autodetect rules
- default alias metadata for each provider kind

UX reason:

- providers feel consistent instead of each one having slightly different behavior

### `auto`
Owns:

- merging explicit + detected instances
- applying user-facing options
- naming/dedup strategy
- assembling `router.Instance` values
- passing explicit top-level aliases to router

UX reason:

- this becomes the place where “sensible defaults” and “my explicit preferences” are reconciled

### `llm.Service`
Owns:

- model resolution
- candidate generation and ranking
- request-time target resolution
- retry/failover sequencing

UX reason:

- resolution behavior becomes easier to describe and debug

---

# 7. Centralize modeldb access first, with user benefits in mind

This is the best first move because it improves consistency in several user-visible areas at once.

## 7.1 Rule to enforce

For non-test code:

- only the catalog implementation package may import `github.com/codewandler/modeldb`

## 7.2 Why users benefit

Centralization means these experiences become more aligned:

- `Models()` results
- factual aliases
- pricing lookup
- token estimation profiles
- runtime-visible local models

Instead of each package behaving slightly differently, users get one shared notion of model truth.

## 7.3 What should move behind catalog helpers

### Built-in catalog loading

Already partly present. Make this the single path.

### Resolved runtime overlays

Ollama/DockerMR runtime source construction should move behind catalog-owned helpers.

### Factual alias lookup

This should stay in the catalog boundary.

### Pricing lookup

`usage/pricing.go` should stop loading `modeldb` directly.

### Token profile lookup

`tokencount/estimate.go` should stop resolving wire models directly through `modeldb`.

## 7.4 UX-oriented llm-facing APIs

These names are illustrative, but the shape matters.

```go
func BuiltInCatalogCostCalculator() usage.CostCalculator
func TokenProfileFor(provider, model string) (TokenProfile, bool)
func ResolveCatalogWireModel(provider, model string) (CatalogModelInfo, bool)
func VisibleRuntimeModels(ctx context.Context, query RuntimeModelQuery) (Models, error)
```

The ergonomic goal is that higher-level packages ask for what they actually need:

- cost calculation
- token profile
- visible runtime models

not raw catalog plumbing.

## 7.5 Specific call-site UX wins

### `usage/pricing.go`

User-visible win:

- pricing behavior feels more consistent across providers and aliases

### `tokencount/estimate.go`

User-visible win:

- token estimates choose better defaults more often

### `provider/ollama/ollama.go` and `provider/dockermr/dockermr.go`

User-visible win:

- local/runtime model listings feel smarter and less ad hoc

---

# 8. Introduce a provider registry, with consistency as the UX goal

## 8.1 Why this helps users indirectly

A provider registry is not a user-facing object, but it makes the user experience more uniform:

- each provider follows the same lifecycle
- autodetect rules are centralized and easier to document
- alias metadata is sourced in one consistent way
- adding a provider is less likely to introduce one-off quirks

## 8.2 Core idea

A provider kind definition should answer four questions:

1. what is the provider type name?
2. how do I build one instance?
3. how do I detect instances automatically?
4. what alias metadata and built-in alias behavior does it contribute?

## 8.3 Recommended internal API shape

```go
package providerregistry

type Registry interface {
    Register(def Definition)
    MustDefinition(typeName string) Definition
    Definition(typeName string) (Definition, bool)
    Detect(ctx context.Context, env DetectEnv, disabled map[string]bool) ([]DetectedInstance, error)
}

type Definition struct {
    Type string
    Build func(ctx context.Context, req BuildRequest) (llm.Provider, InstanceMetadata, error)
    Detect func(ctx context.Context, env DetectEnv) ([]DetectedInstance, error)
}

type BuildRequest struct {
    Name   string
    Type   string
    Shared SharedBuildConfig
    Params map[string]any
}

type SharedBuildConfig struct {
    HTTPClient *http.Client
    LLMOptions []llm.Option
}

type DetectedInstance struct {
    Name   string
    Type   string
    Params map[string]any
    Order  int
}

type InstanceMetadata struct {
    ModelAliases         map[string]string
    BuiltinAliasTargets  map[string]string
    SupportsBuiltinAlias bool
}
```

## 8.4 UX-oriented design choice: metadata returned at build time

Recommendation: registry `Build` should return:

- concrete provider
- alias metadata
- built-in alias targets

Why this helps UX:

- alias behavior stays tied to the actual provider being built
- fewer split sources of truth means fewer user-visible inconsistencies

## 8.5 Provider-specific params

Keep these internal and flexible at first.

Examples:

- Claude account name/store
- Codex auth
- Ollama base URL
- DockerMR engine/base URL

UX reason:

- we can preserve a clean public API while simplifying internals gradually

---

# 9. Slim router down to a true runtime router

## 9.1 Desired router input

Router should receive already-built instances.

```go
package router

type Instance struct {
    Name                string
    Type                string
    Provider            llm.Provider
    ModelAliases        map[string]string
    BuiltinAliasTargets map[string]string
}

type Options struct {
    Name      string
    Instances []Instance
    Aliases   map[string][]AliasTarget
}

func New(opts Options) (*Provider, error)
```

## 9.2 UX benefit of this simplification

Once router only consumes concrete instances, its observable behavior becomes much easier to explain:

- router does not decide what a provider is
- router does not invent provider instances
- router only resolves references and performs failover

That clarity should translate into:

- easier docs
- easier debugging
- easier error messages

## 9.3 Keep current path compatibility

Preserve support for:

- bare model IDs: `gpt-4o`
- type-prefixed IDs: `openai/gpt-4o`
- multi-instance full IDs: `work/openai/gpt-4o`
- provider-scoped aliases: `bedrock/fast`
- top-level aliases: `fast`, `default`, `powerful`

## 9.4 Ergonomic improvement: better ambiguity errors

This is worth calling out as an explicit deliverable.

When a model reference is ambiguous, return errors that include:

- the input string
- candidate matches
- one or two suggested qualified forms

This is a high-value UX improvement with low risk.

## 9.5 Ergonomic improvement: expose resolvable model list more clearly

Consider whether `router.Models()` output should remain the primary discoverability surface for users.

If yes, ensure the resulting `Aliases` are coherent and not cluttered by accidental/internal artifacts.

The guiding principle:

- `Models()` should help a human understand what they can ask for

not merely dump internal index state.

---

# 10. Rebuild auto as the ergonomic composition layer

## 10.1 New job of `auto`

After the registry exists, `auto.New(ctx, opts...)` should be readable as a UX policy pipeline:

1. collect user option state
2. translate explicit options to requested instances
3. run detection if enabled
4. merge explicit + detected requests
5. normalize names deterministically
6. build concrete instances via registry
7. build top-level alias targets
8. return a routed provider

## 10.2 What should disappear from `auto`

- `providerEntry`
- synthetic `factoryKey`
- duplicated provider build closures
- most provider-specific implementation logic

## 10.3 UX-focused naming policy

Instance naming policy should be deterministic and human-readable.

Recommendation:

1. explicit names always win
2. detected duplicates of the same logical instance are skipped
3. true collisions get `-2`, `-3`, etc.
4. preserve registration order

Examples:

- explicit `openai` + detected `openai` => keep explicit only
- explicit `work` + explicit second `work` => `work`, `work-2`

## 10.4 Built-in aliases are composition policy

Built-in aliases should be assembled in `auto`, not invented by router.

Why this is good UX:

- top-level aliases are part of “how this multi-provider setup should feel”
- that is an orchestration concern, not a low-level routing concern

## 10.5 Potential public-API ergonomics improvements

These are optional and can be phased in later, but are worth noting.

### A. Better alias option naming

If there is or will be an alias option, prefer names that read clearly:

- `WithAlias("review", "work/claude/sonnet")`
- `WithAliases(map[string][]string{...})`

### B. Better discovery hooks

Potential future additions:

- `auto.Describe(...)`
- `auto.Instances(...)`
- `router.Resolve(...)` or debug helpers

These would help users understand what `auto` built without digging through internals.

Not required for the first refactor, but enabled by it.

---

# 11. Canonicalization policy must have one owner

One major source of end-user inconsistency is provider identity drift.

Examples already implied in tree:

- `claude` maps to Anthropic-backed model identity in some contexts
- `codex` maps to OpenAI model identity in some contexts

## 11.1 Recommendation

Create centralized helpers inside the catalog boundary, conceptually like:

```go
func CanonicalCatalogService(provider string) string
func CanonicalPricingProvider(provider string) string
func CanonicalProfileProvider(provider string) string
```

Potentially these collapse to one function if semantics align.

## 11.2 UX reason

If users ask for a model through different entry points, they should get consistent behavior for:

- alias resolution
- pricing
- token estimation
- catalog lookups

That consistency matters more than where the helper lives internally.

---

# 12. Detailed phased migration plan with UX deliverables

## Phase 0 — add characterization tests and UX assertions

Before structural changes, lock down current behavior.

### Add/strengthen tests for

- router short-ID ambiguity behavior
- router provider-prefixed alias behavior
- router multi-instance full-ID behavior
- auto built-in alias precedence
- auto detection order -> failover order relationship
- auto explicit + detected merge behavior

### Add UX-oriented assertions where feasible

- ambiguous errors include useful disambiguation hints
- instance naming is deterministic under collision
- built-in aliases resolve in documented order

### Why first

This protects both correctness and ergonomics.

---

## Phase 1 — centralize catalog/modeldb ownership

### Changes

1. create or reshape `internal/modelcatalog`
2. move all direct runtime `modeldb` usage there
3. make `llm` wrappers delegate to that package
4. refactor call sites:
   - `usage/pricing.go`
   - `tokencount/estimate.go`
   - `provider/ollama/ollama.go`
   - `provider/dockermr/dockermr.go`

### UX deliverables

- pricing consistency improves
- token-profile selection improves
- local runtime visible models become more coherent
- factual aliases come from one shared notion of truth

### Success criteria

- non-test runtime code has one `modeldb` owner package

---

## Phase 2 — introduce provider registry without changing router yet

### Changes

1. add `internal/providerregistry`
2. implement definitions for current providers
3. move duplicated provider construction code into registry definitions
4. make `auto` use registry for explicit providers and autodetect output
5. adapt to old router temporarily if needed

### UX deliverables

- provider behavior becomes more uniform
- autodetect behavior becomes easier to document and trust
- alias metadata becomes more consistently sourced

### Success criteria

- adding a provider kind mostly means editing registry files
- duplicated provider build logic in `auto` is dramatically reduced

---

## Phase 3 — change router to concrete instances

### Changes

1. add `router.Instance` and `router.Options`
2. change `router.New` to consume instances directly
3. remove factory-map support
4. remove synthetic factory key logic from `auto`
5. preserve path and alias semantics

### UX deliverables

- clearer resolution model
- better ambiguity errors
- easier to explain docs for advanced routing

### Success criteria

- router no longer knows about factories/config compilation
- ambiguity errors are more actionable

---

## Phase 4 — simplify auto fully around ergonomics

### Changes

1. remove transitional compatibility helpers
2. reduce `auto.New` to orchestration + policy
3. keep built-in alias policy in `auto`
4. clean up obsolete files/types
5. review docs/examples for the new mental model

### UX deliverables

- `auto.New(ctx)` is clearly the happy path
- advanced options feel layered, not tangled
- docs can explain model selection in three levels: intent, provider, instance

### Success criteria

- `auto` reads like composition code
- no synthetic construction artifacts remain

---

# 13. Recommended documentation outcomes

The refactor will be most valuable if it also makes docs easier to write.

## 13.1 Recommended user-facing documentation structure

After implementation, docs should be able to explain model selection like this:

### A. Quick start

- use `auto.New(ctx)`
- ask for `fast`, `default`, or `powerful`

### B. Choose a provider family

- use `openai/gpt-4o`
- use `bedrock/...`
- use `anthropic/...`

### C. Choose a specific configured instance

- use `work/openai/gpt-4o`
- use `personal/claude/sonnet`

### D. Understand ambiguity

- short names work when unique
- if ambiguous, qualify with provider or instance prefix

### E. Understand aliases

- top-level aliases are composition shortcuts
- provider-scoped aliases are provider shortcuts
- full paths are explicit instance targeting

If the architecture supports this explanation cleanly, the ergonomics goal is being met.

---

# 14. Decisions to make explicitly before implementation

## 14.1 Should registry `Build` return metadata together with provider?

Recommendation: **yes**.

UX reason:

- it keeps alias behavior coupled to the actual built provider, reducing inconsistencies

## 14.2 Should runtime source creation for Ollama/DockerMR be generic or specialized?

Recommendation: **specialized first, generic later**.

UX reason:

- clearer code usually leads to more trustworthy behavior and easier docs

## 14.3 Should `auto.New` keep returning `*router.Provider`?

Recommendation: **yes for now**.

UX reason:

- avoid unrelated API churn during a refactor whose main user value is predictability

## 14.4 Should provider params be strongly typed immediately?

Recommendation: **not initially**.

UX reason:

- keep the migration focused on behavior and clarity, not type-system perfection

## 14.5 Should better router errors be part of scope?

Recommendation: **yes, explicitly**.

UX reason:

- this is one of the highest-value ergonomic wins available in this refactor

---

# 15. Risks and mitigations, framed as UX risks

## 15.1 Risk: alias behavior changes subtly

User impact:

- model references people already use may resolve differently

Mitigation:

- characterization tests
- preserve resolution order rules
- document any intentional changes very clearly

## 15.2 Risk: detection behavior changes unexpectedly

User impact:

- `auto.New()` may start choosing a different provider by default

Mitigation:

- preserve current detection order
- test explicit vs detected precedence carefully
- call out any intentional change in changelog/docs

## 15.3 Risk: `Models()` output becomes noisier rather than clearer

User impact:

- discoverability worsens

Mitigation:

- treat `Models()` as a discoverability surface, not merely an internal dump
- review alias lists for clarity while preserving compatibility

## 15.4 Risk: centralized catalog layer becomes too abstract

User impact:

- indirect: harder maintenance can reintroduce inconsistency later

Mitigation:

- expose only the helpers current callers actually need
- prefer clear feature-oriented helpers over generic plumbing APIs

---

# 16. What “done” looks like from an end-user perspective

The refactor is successful if a user experiences the system like this:

- `auto.New(ctx)` is the obvious default and usually just works
- `fast`, `default`, and `powerful` are reliable and easy to explain
- provider-scoped references like `openai/gpt-4o` feel natural
- instance-scoped references like `work/openai/gpt-4o` are available when needed
- ambiguous inputs produce helpful errors with clear next steps
- pricing/token/model-list behavior feels consistent across the library

## Final mental model

What users should feel after this refactor:

- I can start simple
- I can get more explicit when needed
- the names are consistent
- the defaults are sensible
- the errors tell me how to fix things

That is the ergonomics bar this plan should optimize for, not just internal cleanup.

---

# 17. Recommended public API and UX spec

This section makes the plan more concrete from the end-user point of view.

The question it answers is:

- after this refactor, what should the API feel like to someone using the library for the first time?

## 17.1 Primary user journeys to optimize

### Journey A: “I just want a good default provider setup”

Desired code:

```go
p, err := auto.New(ctx)
if err != nil {
    return err
}

resp, err := llm.GenerateText(ctx, p,
    llm.WithPrompt("Summarize this document"),
    llm.WithModel("default"),
)
```

Desired user expectation:

- `auto.New(ctx)` detects sensible providers
- `default` works out of the box
- the chosen provider is sensible, stable, and documented

### Journey B: “I want a specific provider family”

Desired code:

```go
p, err := auto.New(ctx)
if err != nil {
    return err
}

resp, err := llm.GenerateText(ctx, p,
    llm.WithPrompt("Write a test plan"),
    llm.WithModel("openai/gpt-4o"),
)
```

Desired user expectation:

- provider-prefixed references are the standard way to be explicit without caring about instance names
- if OpenAI is configured, this should route there directly
- if not configured, the error should say so clearly

### Journey C: “I have multiple accounts/instances and want exact targeting”

Desired code:

```go
p, err := auto.New(ctx,
    auto.WithClaudeAccount("work", workStore),
    auto.WithClaudeAccount("personal", personalStore),
)
if err != nil {
    return err
}

resp, err := llm.GenerateText(ctx, p,
    llm.WithPrompt("Draft a client email"),
    llm.WithModel("work/claude/sonnet"),
)
```

Desired user expectation:

- instance targeting is available when needed
- instance prefixes are stable, human-chosen names
- exact targeting never silently falls back to another instance

### Journey D: “I want my own semantic alias”

Desired code:

```go
p, err := auto.New(ctx,
    auto.WithOpenAI(),
    auto.WithClaudeAccount("work", workStore),
    auto.WithAlias("review", "work/claude/sonnet"),
    auto.WithAlias("draft", "openai/gpt-4o-mini"),
)
```

Desired user expectation:

- custom aliases are first-class and easy to understand
- aliases are stable app-level vocabulary
- if an alias is invalid, the error should identify the bad target

---

## 17.2 Recommended model reference UX spec

This is the proposed end-user-facing resolution model.

## A. Top-level intent aliases

Supported and documented:

- `fast`
- `default`
- `powerful`

Meaning:

- these are composition-level shortcuts chosen by `auto`
- they route according to provider order and configured built-in alias participation

User promise:

- they should always be easy to explain
- they should never depend on synthetic internal keys
- when disabled, the failure mode should be explicit

## B. Provider-scoped references

Examples:

- `openai/gpt-4o`
- `openai/mini`
- `anthropic/sonnet`
- `bedrock/claude-sonnet-4-5`

Meaning:

- target a provider family/type, not a specific configured instance
- if only one matching instance exists, resolution is straightforward
- if multiple instances of that family exist, the router can still resolve via documented precedence rules or require an instance prefix when needed

Recommended user guidance:

- this should be the default “explicit but portable” selector form in docs

## C. Instance-scoped references

Examples:

- `work/openai/gpt-4o`
- `personal/claude/sonnet`
- `local/ollama/llama3.2`

Meaning:

- target exactly one configured instance
- bypass ambiguity entirely

User promise:

- exact instance references should never be reinterpreted creatively

## D. Bare model IDs

Examples:

- `gpt-4o`
- `claude-sonnet-4-5`
- `sonnet`

Meaning:

- allowed as convenience inputs
- only reliable when unambiguous

User guidance:

- bare IDs are convenient, but provider-scoped refs are preferred in production code and docs

This is important ergonomically:

- keep bare IDs for convenience and compatibility
- steer users toward provider-scoped refs as the more stable explicit form

---

## 17.3 Recommended public API stance

The public API should keep the current style, but its intended usage should become clearer.

## Keep simple constructor ergonomics

Recommended happy path:

```go
p, err := auto.New(ctx)
```

Recommended explicit path:

```go
p, err := auto.New(ctx,
    auto.WithOpenAI(),
    auto.WithBedrock(),
    auto.WithAlias("review", "openai/gpt-4o"),
)
```

## Keep option names user-task oriented

Good option naming should answer “what is the user trying to do?” rather than “what internal mechanism is being configured?”

Examples of good direction:

- `WithOpenAI()`
- `WithClaudeAccount(name, store)`
- `WithClaude(store)`
- `WithoutAutoDetect()`
- `WithoutProvider(auto.ProviderBedrock)`
- `WithoutBuiltinAliases()`
- `WithAlias(name, target)`
- `WithAliases(map[string][]string)`

If alias helpers do not exist yet, adding them would be a good ergonomic follow-up enabled by this refactor.

## Potential follow-up ergonomic additions

These are optional, but worth considering once the architecture is simpler.

### `Describe()` / `Instances()` / debug surfaces

Possible examples:

```go
summary := auto.Describe(ctx, opts...)
fmt.Println(summary)
```

or

```go
instances := router.Instances()
models := router.Models()
```

Use case:

- help users understand what `auto` configured
- help CLIs and debugging tools expose a friendly summary

### `ResolveModel()` debug helper

Possible example:

```go
resolved, err := router.ResolveModel("sonnet")
```

Useful for:

- debugging ambiguity
- powering CLI commands like `llm models resolve sonnet`

Not required for the refactor, but the new architecture makes it easier.

---

## 17.4 Error UX spec

This should be treated as a real deliverable, not a nice-to-have.

## A. Unknown provider family

Current idea:

- if user writes `openai/gpt-4o` but OpenAI is not configured, say that directly

Desired message shape:

- `provider "openai" is not configured`
- optionally: `available providers: anthropic, bedrock, ollama`

## B. Unknown model

Desired message shape:

- `model "openai/gpt-9" not found`
- optionally: suggest close matches or the provider's available models if cheap to compute

## C. Ambiguous model

Desired message shape:

- `model "sonnet" is ambiguous`
- `matches: anthropic/sonnet, work/claude/sonnet, personal/claude/sonnet`
- `use a provider-prefixed or instance-prefixed reference`

This is likely the single highest-value router UX improvement.

## D. Invalid custom alias target

Desired message shape:

- `alias "review" points to unknown target "work/claude/missing-model"`

Prefer validating alias targets eagerly during `auto.New(...)` when possible.

That improves startup-time feedback dramatically.

## E. No providers available

Desired message shape:

- `no providers available`
- `auto-detection found none; configure a provider explicitly or set required credentials`

This is much more helpful than a bare sentinel error.

---

## 17.5 Discoverability UX spec

A good end-user experience is not just successful routing. It is also helping users discover what they can ask for.

## A. `Models()` should be human-meaningful

The output of `Models()` should serve users and tooling as a discoverability surface.

That means:

- model IDs should be recognizable
- aliases should be useful, not just exhaustive internal artifacts
- provider names should be readable and stable

A useful rule of thumb:

- if an alias is shown in `Models()`, a user should feel comfortable trying it

## B. Prefer curated clarity over maximal noise

Do not turn `Aliases` into an unreadable dump of every derivable string unless compatibility absolutely requires it.

A balanced goal:

- preserve compatibility in routing
- present the most useful aliases in discovery surfaces

This may imply a future distinction between:

- “resolvable strings”
- “recommended display aliases”

That distinction is optional now, but worth keeping in mind.

## C. Local/runtime providers should feel particularly clear

For Ollama/DockerMR, discoverability is especially important.

Users benefit if the provider can distinguish conceptually between:

- installed models
- visible but not yet acquired models
- curated fallback defaults

Even if the current `llm.Models` type does not express all of that yet, centralizing catalog/runtime logic moves the codebase closer to exposing that clearly later.

---

## 17.6 Recommended docs/examples after refactor

The architecture should support a small, crisp set of examples.

## Example 1: simplest possible use

```go
p, _ := auto.New(ctx)
resp, _ := llm.GenerateText(ctx, p,
    llm.WithPrompt("Explain Raft consensus"),
    llm.WithModel("default"),
)
```

## Example 2: explicit provider family

```go
p, _ := auto.New(ctx)
resp, _ := llm.GenerateText(ctx, p,
    llm.WithPrompt("Write a migration plan"),
    llm.WithModel("openai/gpt-4o"),
)
```

## Example 3: exact instance targeting

```go
p, _ := auto.New(ctx,
    auto.WithClaudeAccount("work", store),
    auto.WithClaudeLocal(),
)
resp, _ := llm.GenerateText(ctx, p,
    llm.WithPrompt("Draft a proposal"),
    llm.WithModel("work/claude/sonnet"),
)
```

## Example 4: custom app aliases

```go
p, _ := auto.New(ctx,
    auto.WithOpenAI(),
    auto.WithAlias("draft", "openai/gpt-4o-mini"),
    auto.WithAlias("review", "openai/gpt-4o"),
)
```

## Example 5: ambiguity and correction

```go
_, err := llm.GenerateText(ctx, p,
    llm.WithPrompt("hello"),
    llm.WithModel("sonnet"),
)
// err:
// model "sonnet" is ambiguous; matches: anthropic/sonnet, work/claude/sonnet
// use a provider-prefixed or instance-prefixed reference
```

If the refactor leads to docs this straightforward, that is a strong sign the architecture improved the UX.

---

## 17.7 Recommended acceptance criteria from the user perspective

The refactor should be considered successful when these statements are true.

### Defaults

- a new user can usually start with `auto.New(ctx)` and `default`
- built-in aliases behave consistently and are easy to explain

### Explicitness

- provider-scoped model refs are the standard explicit form
- instance-scoped refs are available for advanced users and never behave surprisingly

### Errors

- ambiguity errors tell users exactly how to disambiguate
- missing-provider errors are clear
- invalid custom aliases fail early with actionable messages

### Discoverability

- `Models()` is helpful as a discovery surface
- local providers expose model availability more coherently

### Consistency

- pricing, token estimates, aliases, and visible models feel like they come from one coherent system

---

## 17.8 Practical recommendation: define a “preferred model reference style” in docs

One subtle but important ergonomics improvement is to stop treating all valid model strings as equally recommended.

Recommended documentation stance:

- for quick experimentation: `fast`, `default`, `powerful`
- for explicit production usage: `provider/model`
- for advanced multi-instance routing: `instance/provider/model`
- for convenience only: bare model IDs when unambiguous

This gives users a clear ladder of increasing explicitness.

That guidance alone will make the system feel much easier to understand.

---

# 18. Implementation checklist: required changes vs optional UX follow-ups

This section translates the plan into a practical execution checklist.

The goal is to make it obvious:

- what must happen to achieve the architectural refactor
- what should happen to achieve the most important UX improvements
- what can wait until after the core migration is complete

## 18.1 Required changes for the core refactor

These are the changes required to achieve the intended architecture.

### A. Catalog centralization

Required:

- [ ] Create or reshape a single internal catalog owner package (`internal/modelcatalog` or equivalent)
- [ ] Move all non-test runtime `modeldb` imports behind that package
- [ ] Keep built-in catalog caching in one place only
- [ ] Keep resolved runtime overlay construction in one place only
- [ ] Add llm-facing wrappers for the catalog helpers current callers need

Required call-site migrations:

- [ ] Refactor `usage/pricing.go` to stop importing `modeldb` directly
- [ ] Refactor `tokencount/estimate.go` to stop importing `modeldb` directly
- [ ] Refactor `provider/ollama/ollama.go` to stop importing `modeldb` directly
- [ ] Refactor `provider/dockermr/dockermr.go` to stop importing `modeldb` directly

### B. Provider registry introduction

Required:

- [ ] Add `internal/providerregistry`
- [ ] Add a registry type and definition type
- [ ] Add build functions for all current auto-supported provider kinds
- [ ] Add autodetect hooks for all currently autodetected provider kinds
- [ ] Move provider-specific alias metadata sourcing into the registry build path
- [ ] Ensure shared `llm.Option` and HTTP client propagation are handled consistently by registry build logic

Initial provider kinds to cover:

- [ ] claude
- [ ] anthropic
- [ ] bedrock
- [ ] openai
- [ ] openrouter
- [ ] minimax
- [ ] ollama
- [ ] codex
- [ ] dockermr

### C. Router simplification

Required:

- [ ] Introduce `router.Instance`
- [ ] Introduce a simplified `router.Options`
- [ ] Change `router.New` to consume concrete instances rather than factories
- [ ] Remove factory lookup from router
- [ ] Remove provider instantiation from router
- [ ] Preserve current path compatibility and alias resolution semantics
- [ ] Preserve failover ordering semantics

### D. Auto simplification

Required:

- [ ] Replace `providerEntry`-oriented assembly with requested-instance assembly
- [ ] Make `auto` consume the provider registry rather than embedding provider construction logic
- [ ] Keep existing public `auto.WithX(...)` API working
- [ ] Normalize explicit + detected instances deterministically
- [ ] Build built-in top-level aliases in `auto`
- [ ] Remove synthetic factory key generation

### E. Canonicalization centralization

Required:

- [ ] Create one owner for provider/service canonicalization used by catalog, pricing, and token profile resolution
- [ ] Remove duplicated canonicalization rules from downstream packages where possible

---

## 18.2 Required UX changes that should be in scope

These are not just nice extras. They materially improve usability and should be considered part of the refactor.

### A. Better ambiguity errors

- [ ] When a model ref is ambiguous, include the user input in the error
- [ ] Include candidate matches in the error
- [ ] Include a short instruction on how to disambiguate (`provider/model` or `instance/provider/model`)

### B. Better missing-provider errors

- [ ] If a provider family is referenced but not configured, say so directly
- [ ] Prefer including available configured providers when practical

### C. Better invalid-alias errors

- [ ] Validate explicit alias targets during `auto.New(...)` when practical
- [ ] Fail early with alias name + invalid target included in the error

### D. Better no-provider guidance

- [ ] Keep `ErrNoProviders` compatibility if needed
- [ ] Wrap or augment it with actionable setup guidance in user-facing paths

### E. Deterministic instance naming

- [ ] Replace ad hoc duplicate-name suffixing with predictable `-2`, `-3`, ... naming
- [ ] Ensure explicit instances win over detected duplicates when logically equivalent

---

## 18.3 Optional UX follow-ups after the core refactor

These are valuable, but can happen after the main architectural work lands.

### A. Alias convenience options

Optional additions:

- [ ] Add `auto.WithAlias(name, target)` if not already present
- [ ] Add `auto.WithAliases(map[string][]string)` or equivalent bulk helper

Why optional:

- nice public ergonomics improvement
- not required to complete the registry/router/catalog cleanup

### B. Introspection / debug helpers

Optional additions:

- [ ] Add an `auto.Describe(...)` or similar summary helper
- [ ] Add a router-side introspection helper for configured instances
- [ ] Add a router model resolution debug helper (`ResolveModel`, `ExplainModel`, etc.)

Why optional:

- very useful for CLIs, docs, and debugging
- easier to build after architecture is simplified

### C. Discovery-oriented model display improvements

Optional additions:

- [ ] Review whether `Models()` should distinguish display aliases vs all resolvable strings
- [ ] Improve `Models()` output quality for multi-instance routers
- [ ] Consider better representation of installed vs visible local models in future APIs

Why optional:

- useful for polish and discoverability
- may benefit from seeing the post-refactor architecture first

### D. Doc refresh

Optional but strongly recommended:

- [ ] Update README examples to follow the preferred reference style ladder
- [ ] Add docs section explaining `fast/default/powerful`
- [ ] Add docs section explaining `provider/model` vs `instance/provider/model`
- [ ] Add docs section explaining ambiguity and disambiguation

---

## 18.4 Concrete code change checklist by file/package

This section is intentionally more tactical.

## Catalog layer

### `internal/models/all.go` or new `internal/modelcatalog/*`

- [ ] Be the only non-test runtime owner of `modeldb`
- [ ] Own built-in catalog caching
- [ ] Own resolved catalog creation
- [ ] Own runtime overlay construction helpers
- [ ] Own provider/service canonicalization helpers used by catalog lookups
- [ ] Expose internal helpers used by `llm` wrappers

### `model_catalog.go`

- [ ] Delegate cleanly to the internal catalog owner package
- [ ] Keep the public llm-level wrapper surface coherent
- [ ] Avoid leaking raw `modeldb` construction details into unrelated packages

## Usage / pricing

### `usage/pricing.go`

- [ ] Replace direct `modeldb.LoadBuiltIn()` use with llm/catalog helper(s)
- [ ] Remove local duplication of provider canonicalization if centralized helper covers it
- [ ] Preserve behavior of `usage.Default()` as a best-effort calculator

## Token counting

### `tokencount/estimate.go`

- [ ] Replace direct built-in catalog lookup with `llm.TokenProfileFor(...)` or equivalent
- [ ] Keep fallback heuristic path when catalog/profile lookup fails
- [ ] Preserve current estimate behavior where catalog data is unavailable

## Local runtime providers

### `provider/ollama/ollama.go`

- [ ] Replace direct runtime source creation with llm/catalog helper(s)
- [ ] Preserve current fallback to curated model list
- [ ] Keep visible-model behavior robust on runtime overlay errors

### `provider/dockermr/dockermr.go`

- [ ] Replace direct runtime source creation with llm/catalog helper(s)
- [ ] Preserve current fallback to curated model list

## Provider registry

### `internal/providerregistry/*`

- [ ] Add registry storage and lookup
- [ ] Add provider kind definitions
- [ ] Add autodetect orchestration
- [ ] Add metadata generation for aliases and built-in aliases

## Auto

### `auto/options.go`

- [ ] Refactor options to produce normalized requested instances rather than embedded provider constructor closures
- [ ] Keep public option compatibility

### `auto/detect.go`

- [ ] Shrink to a thin registry call or remove entirely
- [ ] Preserve current detection order semantics

### `auto/aliases.go`

- [ ] Reduce to composition policy helper if metadata now comes from registry/catalog
- [ ] Keep built-in alias assembly behavior easy to understand

### `auto/auto.go`

- [ ] Make it a thin orchestration flow
- [ ] Remove factory-key generation
- [ ] Build concrete router instances directly

## Router

### `router (removed)/config.go`

- [ ] Replace or slim down current factory-oriented config shape
- [ ] Center the package around instance-oriented inputs

### `router (removed)/router.go`

- [ ] Stop instantiating providers
- [ ] Keep index-building and routing behavior
- [ ] Improve ambiguity and missing-provider error messages

### `router (removed)/routing.go`

- [ ] Keep retry/failover behavior intact
- [ ] Improve user-facing resolution errors where appropriate

---

## 18.5 Suggested execution plan with “must-have” vs “nice-to-have” boundaries

A practical way to execute this without scope creep:

## Step 1 — tests first

Must-have:

- [ ] Characterization tests for alias precedence
- [ ] Characterization tests for multi-instance routing
- [ ] Characterization tests for failover order
- [ ] Characterization tests for auto explicit + detected merge behavior

Nice-to-have in same step:

- [ ] Add assertions for better ambiguity error formatting

## Step 2 — catalog centralization

Must-have:

- [ ] Centralize runtime `modeldb` ownership
- [ ] Migrate the four main call sites (`usage`, `tokencount`, `ollama`, `dockermr`)

Nice-to-have in same step:

- [ ] Add explicit canonicalization helpers with tests

## Step 3 — provider registry introduction

Must-have:

- [ ] Introduce registry skeleton
- [ ] Migrate provider build logic into registry definitions
- [ ] Migrate autodetect logic into registry definitions

Nice-to-have in same step:

- [ ] Make registry definition metadata easy to introspect for docs/debugging later

## Step 4 — auto migration

Must-have:

- [ ] Make `auto` assemble requested instances via registry
- [ ] Preserve public API compatibility
- [ ] Normalize names deterministically

Nice-to-have in same step:

- [ ] Eager validation for explicit aliases

## Step 5 — router migration

Must-have:

- [ ] Change router to consume concrete instances
- [ ] Remove factory-key plumbing
- [ ] Preserve routing semantics

Nice-to-have in same step:

- [ ] Improve ambiguity/missing-provider errors

## Step 6 — cleanup and polish

Must-have:

- [ ] Delete obsolete transitional types/files
- [ ] Confirm non-test runtime `modeldb` imports are centralized

Nice-to-have in same step:

- [ ] Add optional alias convenience helpers
- [ ] Add optional introspection/debug helpers
- [ ] Refresh docs/examples

---

## 18.6 Proposed acceptance gates for merging

These gates make the refactor easier to review and safer to land.

### Gate 1: architecture gate

Must be true:

- [ ] router no longer instantiates providers
- [ ] auto no longer owns duplicated provider constructor logic
- [ ] runtime `modeldb` access is centralized

### Gate 2: compatibility gate

Must be true:

- [ ] existing public `auto.WithX(...)` options still work
- [ ] existing valid model reference forms still resolve
- [ ] failover order remains compatible

### Gate 3: UX gate

Must be true:

- [ ] ambiguity errors are more actionable than before
- [ ] missing-provider errors are clearer than before
- [ ] duplicate instance naming is deterministic and readable

### Gate 4: docs/examples gate

Strongly recommended:

- [ ] docs show `auto.New(ctx)` as the default path
- [ ] docs recommend `provider/model` as the preferred explicit reference style
- [ ] docs explain `instance/provider/model` for advanced setups

---

## 18.7 Recommended “do not overdo it yet” list

To keep this execution focused, avoid over-expanding the first implementation.

Do not overdo initially:

- [ ] inventing a brand new public routing DSL
- [ ] redesigning `llm.Models` in the same change
- [ ] making every provider param strongly typed on day one
- [ ] adding too many generic catalog abstractions before specific call sites are migrated
- [ ] mixing large doc rewrites into early structural commits

The best version of this refactor is:

- structurally cleaner
- behaviorally compatible
- noticeably more usable
- but still incremental and reviewable


## Status update: first pass implemented

The first-pass centralization is now in place:

- public catalog/modeldb helper methods were removed from the `llm` package
- runtime catalog loading/canonicalization/wire-model lookup moved into `internal/modelcatalog`
- catalog-to-`llm.Models` projections and runtime visibility helpers moved into `internal/modelview`
- internal runtime callers (`usage`, `tokencount`, `auto`, `anthropic`, `openai`, `openrouter`, `ollama`, `dockermr`, `minimax`) now depend on those internal packages instead of a public `llm` catalog surface
- non-test runtime `modeldb` imports are reduced to:
  - `internal/modelcatalog`
  - `internal/modelview`
  - `cmd/llmcli/main.go` (intentional CLI exception for now)

This means the plan should now treat public `llm` catalog wrappers as removed, not as a target state.

---

# 19. Revised concrete design: replace auto/router-as-provider with llm.Service

This section supersedes the earlier direction that treated `auto` and `router`
as provider-shaped runtime objects.

## 19.1 Core decision

`auto` and `router` should no longer be modeled as `llm.Provider` implementations.

Instead:

- actual providers remain implementations of `llm.Provider`
- orchestration moves into a new `llm.Service`
- `llm.New(opts...)` becomes the main constructor for end users
- autodetect, provider preference, intent aliases, retry, and fallback become
  service concerns rather than synthetic provider concerns

## 19.2 Target public API

The intended public API should converge toward:

```go
svc, err := llm.New(
    llm.WithAutoDetect(),
    llm.WithProvider(openai.New(...)),
    llm.WithProviderNamed("work", anthropic.New(...)),
    llm.WithIntentAlias("fast", llm.IntentSelector{Model: "openai/gpt-4o-mini"}),
)
```

The service should then be used for execution.

Two compatibility options are acceptable during migration:

### Option A: Service itself satisfies the execution surface

```go
stream, err := svc.CreateStream(ctx, req)
```

### Option B: New top-level helpers accept service

```go
resp, err := llm.GenerateText(ctx, svc, ...)
```

Either is fine short term. The main architectural requirement is that users hold
`Service`, not synthetic routed providers.

---

## 19.3 New main runtime object: `llm.Service`

### Responsibilities

`Service` owns:

- registered provider instances
- provider registry access for autodetection/building
- model string resolution
- intent alias resolution
- provider preference ordering
- same-request candidate generation
- cross-provider fallback sequencing
- provider wrappers/middleware application
- service-level retry/fallback policy

### Non-responsibilities

`Service` should not:

- implement a real backend provider protocol itself
- pretend to be a catalog-backed provider
- expose provider-construction hacks like factory maps or synthetic type keys

### Suggested shape

```go
type Service struct {
    registry     *providerregistry.Registry
    providers    []RegisteredProvider
    intents      IntentResolver
    preferences  PreferencePolicy
    wrappers     []ProviderWrapper
    retryPolicy  RetryPolicy
    logger       Logger
}
```

This shape can evolve, but the ownership split is the key point.

---

## 19.4 New constructor flow: `llm.New(opts...)`

### Goal

Users should have one clear entry point.

```go
svc, err := llm.New(opts...)
```

Internally this should delegate to a dedicated service constructor, for example:

```go
service.NewService(opts...)
```

### High-level construction flow

1. collect service options
2. instantiate or use a provider registry
3. collect explicit provider requests
4. run autodetection if enabled
5. build concrete provider instances
6. register providers with service metadata
7. compile intent aliases and preference rules
8. attach wrappers/middleware
9. return `*llm.Service`

---

## 19.5 Provider registry design

The provider registry should become the single owner of:

- provider kind definitions
- autodetectors
- provider builders

It should not own routing.

### Suggested internal package

- `internal/providerregistry`

### Suggested types

```go
type Registry struct {
    defs map[string]Definition
}

type Definition struct {
    Type string

    Detect func(ctx context.Context, env DetectEnv) ([]DetectedProvider, error)
    Build  func(ctx context.Context, cfg BuildConfig) (llm.Provider, error)
}

type DetectEnv struct {
    HTTPClient *http.Client
    LLMOptions []llm.Option
}

type BuildConfig struct {
    Name       string
    Type       string
    Params     map[string]any
    HTTPClient *http.Client
    LLMOptions []llm.Option
}

type DetectedProvider struct {
    Name   string
    Type   string
    Params map[string]any
    Order  int
}
```

### Design rule

A provider kind should be definable in one place.

Adding a provider should mostly mean:

- add a registry definition
- add tests for detection/build behavior
- optionally add docs

not editing many files in `auto`.

---

## 19.6 Registered provider model

The service needs richer metadata than plain `llm.Provider`.

### Suggested type

```go
type RegisteredProvider struct {
    Name      string // instance name, e.g. "work"
    ServiceID string // modelcatalog service id, e.g. "openai", "anthropic"
    Provider  llm.Provider

    Priority   int
    Tags       map[string]string
    Wrappers   []ProviderWrapper
    Preference ProviderPreference
}
```

### Why this matters

The service needs to know:

- which modelcatalog service each provider maps to
- whether a provider is a named instance
- how to rank it relative to others
- what wrappers should apply to it

---

## 19.7 Explicit model reference semantics

The system should preserve the current three-level user model, but service-owned
resolution should replace router-owned string tables.

### Level 1: intent aliases

Examples:

- `fast`
- `default`
- `powerful`
- app-defined aliases like `draft`, `review`

These are service policy.

### Level 2: provider-scoped references

Examples:

- `openai/gpt-4o`
- `anthropic/sonnet`
- `bedrock/claude-sonnet-4-6`

These constrain candidate generation to one service family.

### Level 3: instance-scoped references

Examples:

- `work/openai/gpt-4o`
- `personal/claude/sonnet`

These constrain candidate generation to one registered provider instance.

### Bare model IDs

Examples:

- `gpt-4o`
- `sonnet`
- `claude-sonnet-4-6`

These remain supported as convenience selectors, but should only resolve when
unambiguous or when service policy explicitly defines ordering.

---

## 19.8 Intent alias design

Intent aliases should be a first-class service concern.

### Suggested type

```go
type IntentSelector struct {
    Model          string
    PreferredKinds []string
    PreferredNames []string
    Tags           map[string]string
}
```

### Examples

```go
llm.WithIntentAlias("fast", llm.IntentSelector{Model: "openai/gpt-4o-mini"})
llm.WithIntentAlias("review", llm.IntentSelector{Model: "work/anthropic/sonnet"})
llm.WithIntentAlias("cheap", llm.IntentSelector{Model: "openrouter/auto"})
```

### Rule

Intent aliases should resolve before catalog candidate generation, so they act
as policy shortcuts rather than provider-local aliases.

---

## 19.9 Request resolution and execution flow

This is the core algorithm.

### Step 1: normalize input

The service should accept either:

- `llm.Request`
- any value that can build into `llm.Request`

Result: one normalized request.

### Step 2: resolve the raw model string once

Given `req.Model`, service should:

1. check explicit instance-prefixed form
2. check explicit provider-prefixed form
3. check configured intent aliases
4. use `internal/modelcatalog` + `internal/modelview` to resolve factual aliases,
   offerings, and service candidates
5. apply controlled provider-policy alias fallback only if needed

This produces a normalized `ResolvedModelSpec`.

### Suggested type

```go
type ResolvedModelSpec struct {
    RawModel       string
    ExactName      string // instance name if explicitly targeted
    ExactServiceID string // service id if explicitly targeted
    RequestedModel string // normalized requested model string
    Offerings      []OfferingCandidate
    FromIntent     string
}
```

### Offering candidate type

```go
type OfferingCandidate struct {
    Ref       modeldb.OfferingRef
    ServiceID string
    WireModel string
    Aliases   []string
}
```

### Step 3: intersect with registered providers

From the resolved offerings, service finds which registered providers can serve
those services.

This becomes a list of execution candidates.

### Step 4: rank candidates

Ranking should consider, in order:

1. explicit instance targeting
2. explicit provider/service targeting
3. configured service preferences
4. configured instance preferences
5. default detection/registration order

### Step 5: execute candidates in order

For each candidate:

1. derive the provider-specific model ID to send
2. apply wrappers/middleware
3. execute request
4. if success, return
5. if same-provider retry is allowed, retry per policy
6. if cross-provider fallback is allowed and error is retryable/fallbackable, continue
7. otherwise stop and return error

---

## 19.10 Retry vs fallback

These should be explicitly separate concepts.

### Same-provider retry

Definition:

- retry the same provider and same offering for transient errors

Examples:

- HTTP 429
- HTTP 503
- temporary overloaded backend

This can be implemented as a wrapper/middleware.

### Cross-provider fallback

Definition:

- after one provider candidate fails, try the next ranked candidate

Examples:

- Anthropic direct fails, try Bedrock Claude
- Bedrock fails, try OpenRouter/OpenAI equivalent if policy allows

This belongs in `Service`, not middleware.

---

## 19.11 Provider wrappers / middleware

Wrappers are still a good idea, but should be scoped carefully.

### Good wrapper concerns

- logging
- metrics
- tracing
- timeout enforcement
- same-provider retry
- circuit breaking
- request/response hooks

### Not wrapper concerns

- cross-provider fallback
- intent alias resolution
- multi-provider candidate ranking

Those require service-wide context and therefore belong in `Service`.

### Suggested wrapper shape

```go
type ProviderWrapper func(RegisteredProvider, Executor) Executor

type Executor interface {
    CreateStream(ctx context.Context, req llm.Request) (llm.Stream, error)
}
```

Exact types can vary. The key point is:

- wrappers wrap one provider execution path
- service orchestrates across providers

---

## 19.12 Options design for `llm.New(...)`

The public options should be user-task oriented.

### Suggested public options

```go
func WithAutoDetect() Option
func WithoutAutoDetect() Option
func WithProvider(p llm.Provider) Option
func WithProviderNamed(name string, p llm.Provider, opts ...ProviderOption) Option
func WithProviderType(typeName string, opts ...ProviderBuildOption) Option
func WithIntentAlias(name string, sel IntentSelector) Option
func WithPreference(pref PreferenceRule) Option
func WithWrapper(w ProviderWrapper) Option
func WithHTTPClient(c *http.Client) Option
func WithLLMOptions(opts ...llm.Option) Option
```

### Notes

- `WithProvider(...)` is for already-built providers
- `WithProviderType(...)` is for registry-driven construction
- `WithAutoDetect()` is the default-friendly switch
- intent aliases and preference rules live on service options, not provider options

---

## 19.13 Preferences design

The system needs explicit ranking policy instead of hidden router registration order.

### Suggested type

```go
type PreferenceRule struct {
    Intent         string
    ServiceIDs     []string
    ProviderNames  []string
    PreferLocal    bool
    PreferCheapest bool
}
```

### Rule

Default behavior may still use registration order, but preferences should make
that policy explicit and configurable.

---

## 19.14 What happens to current `auto`

`auto` should stop being a runtime provider object.

### New role for `auto`

It should become one of these:

1. option helpers for `llm.New(...)`, or
2. an internal implementation detail under service/providerregistry

### Acceptable short-term shape

Keep existing user-facing `auto.WithX(...)` helpers, but make them produce
service/provider requests instead of `providerEntry` and `router.Config` data.

### End state

Users should not need to hold an `auto.Provider` value at runtime.

---

## 19.15 What happens to current `router`

`router` should stop being a provider-shaped runtime abstraction.

### Likely outcomes

- remove it entirely as a public/internal runtime type, or
- keep only tiny reusable helper logic during migration

But the conceptual owner of routing/fallback should be `Service`.

### End state

Users should not depend on a router provider object.

---

## 19.16 Compatibility strategy

To avoid too much disruption, migrate in phases.

### Phase A: introduce `Service`

- add `llm.New(opts...)`
- add `Service`
- keep existing providers unchanged
- keep old auto/router code available temporarily

### Phase B: add provider registry

- centralize detector/build logic
- migrate existing auto-detect behavior into registry definitions

### Phase C: move candidate generation and fallback into `Service`

- service resolves model strings
- service creates ranked provider candidates
- service executes them in order

### Phase D: deprecate synthetic provider abstractions

- stop returning `*router.Provider` from user-facing constructors
- shrink/remove `auto` and `router` as runtime types

### Phase E: wrap providers cleanly

- add wrappers for logging/metrics/retry/tracing
- keep fallback in service layer

---

## 19.17 Concrete migration checklist

### Required

- [ ] Add `Service` type and `llm.New(opts...)`
- [ ] Add internal service package if desired (`internal/service`)
- [ ] Add `providerregistry.Registry`
- [ ] Move autodetect logic out of `auto/detect.go`
- [ ] Move provider build logic out of `auto/options.go`
- [ ] Introduce `RegisteredProvider`
- [ ] Introduce intent alias support at service layer
- [ ] Introduce candidate generation from catalog offerings + registered providers
- [ ] Move cross-provider fallback into service execution loop
- [ ] Remove synthetic factory key logic entirely
- [ ] Stop modelling `auto` and `router` as providers

### Strongly recommended

- [ ] Add same-provider retry wrapper
- [ ] Add logging/metrics wrapper hooks
- [ ] Add better ambiguity and unavailable-provider errors from service resolution
- [ ] Add tests for candidate ranking and fallback ordering

---

## 19.18 Tests to add for the new design

### Service construction tests

- `WithAutoDetect()` builds expected provider set
- explicit providers override or coexist with detected providers predictably
- duplicate instance naming is deterministic

### Resolution tests

- `fast/default/powerful` resolve through intent aliases
- `provider/model` restricts to one service
- `instance/provider/model` restricts to one instance
- ambiguous bare model IDs return actionable errors

### Candidate generation tests

- offerings are intersected correctly with registered providers
- preference rules change ranking as expected
- explicit targeting beats general preference rules

### Execution tests

- same-provider retry happens before cross-provider fallback
- retryable provider failure falls through to next candidate when configured
- fatal non-retryable error stops execution
- wrappers are applied in configured order

---

## 19.19 Recommended target package layout

One reasonable destination layout is:

```text
llm/
  llm.go                # llm.New(...)
  service.go            # public service surface if kept in root package
  option.go             # service/user-facing options

internal/
  modelcatalog/
  modelview/
  providerregistry/
  service/
    service.go
    resolve.go
    candidates.go
    execute.go
    wrappers.go
```

This is only a suggested organization, but the separation of concerns should
look roughly like this.

---

## 19.20 Final target mental model

After this redesign, the system should be explainable in one paragraph:

- providers are real backend implementations
- the provider registry knows how to detect and build them
- the service resolves a requested model once against catalog truth and intent aliases
- the service finds matching registered providers, ranks them, and tries them in order
- wrappers add logging/metrics/retry around provider execution
- cross-provider fallback is owned by the service, not by a fake router provider

That is the architecture this plan should now target.


## Status update: service/runtime migration progress

Additional progress after the first model-catalog pass:

- `llm.Service` now exists as the main orchestration runtime
- `llm.New(opts...)` now builds a `Service`
- `internal/providerregistry` exists and drives provider autodetect/building for `Service`
- `auto.New(...)` now returns `*llm.Service` and acts as a thin convenience wrapper over `llm.New(...)`
- service resolution now produces `ResolvedModelSpec` and `OfferingCandidate` values
- candidate ranking now supports basic `PreferenceRule`s
- `Service.ExplainModel(...)` exists as an initial debug/explain surface
- `router (removed)` has been removed; `llm.Service` is the only runtime orchestration path

This means the plan should now assume:

- `Service` is the target runtime abstraction
- `auto` is a convenience configuration layer
- `router` should be removed or reduced to migration-only compatibility code


## Status update: router deprecation progress

Additional migration progress:

- `cmd/llmcli` now uses `*llm.Service` directly instead of treating `auto.New(...)` as returning a router-backed provider
- `auto` no longer needs router alias target types for built-in aliases; those are now expressed as service intent selectors
- `router (removed)` has been removed from the repository; `llm.Service` is the only runtime orchestration path

Next cleanup target:

- remove or quarantine remaining router-only package usage and tests once no runtime entrypoints depend on it


## Status update: router audit result

A runtime audit showed no remaining non-test runtime imports of `router (removed)`, and the package has now been removed.


## Status update: real CLI sanity check

A real end-to-end CLI smoke test now succeeds through the new architecture:

- `go run ./cmd/llmcli infer -m anthropic/claude-sonnet-4-6 "say exactly: sanity-check-ok"`
- observed output: `sanity-check-ok`

This confirms that the current `llm.Service` + `auto` + registry-based runtime path works in practice against a real provider.
