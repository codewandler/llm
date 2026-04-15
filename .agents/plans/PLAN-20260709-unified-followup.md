# Plan: api/unified — Follow-up Work

**Date**: 2025-07-09
**Status**: Active
**Scope**: Complete `api/unified` package, integrate it into providers, and remove legacy duplication.

---

## What Was Done (Completed)

### Phase 1 — Design Alignment
- Cleaned `DESIGN-api-extraction.md`, `DESIGN-api-unified.md`, and `PLAN-20260415-unified.md` to consistently reference `api/unified` (not the retired `api/adapt` concept).
- Produced `api/README.md` describing the four-layer architecture.
- Verified no stale `api/adapt` references remain in active planning docs.

### Phase 2 — `api/unified` Package Scaffold (TDD)
New package with 7 source files and 2 test files:

| File | What it does |
|------|-------------|
| `doc.go` | Package-level godoc |
| `types_request.go` | Canonical request schema: `Request`, `Message`, `Part`, `Tool`, `ToolCall`, etc. |
| `types_extensions.go` | `RequestExtras` (protocol-specific stashes for extras) |
| `types_event.go` | Canonical event schema: `StreamEvent`, `Delta`, `Started`, `Usage`, `Completed`, `StreamError` |
| `validate.go` | `Request.Validate()` — minimal model/messages invariants |
| `llm_bridge.go` | `RequestFromLLM` / `RequestToLLM` (canonical ↔ llm.Request) |
| `messages_api.go` | `RequestToMessages` / `RequestFromMessages` / `EventFromMessages` |
| `completions_api.go` | `RequestToCompletions` / `RequestFromCompletions` / `EventFromCompletions` |
| `responses_api.go` | `RequestToResponses` / `RequestFromResponses` / `EventFromResponses` |
| `publisher_bridge.go` | `Publish(pub, ev)` — canonical event → `llm.Publisher` |
| `request_bridge_test.go` | Validate / FromLLM / ToMessages / ToCompletions / ToResponses |
| `event_bridge_test.go` | EventFrom{Messages,Completions,Responses} + Publish |

### Phase 3 — Parity Tests + Provider Build Hookup
New test files proving unified path produces identical wire JSON to legacy path:

| Test file | Validates |
|-----------|----------|
| `provider/anthropic/unified_request_test.go` | `RequestToMessages` parity with `anthropic.BuildRequest` |
| `provider/anthropic/unified_event_test.go` | `EventFromMessages` + `Publish` pipeline parity |
| `provider/minimax/unified_request_test.go` | MiniMax thinking-omit parity |
| `provider/minimax/unified_event_test.go` | MiniMax messages event pipeline |
| `provider/openai/unified_request.go` | `buildCompletionsBodyUnified` / `buildResponsesBodyUnified` helpers |
| `provider/openai/unified_request_test.go` | Completions + Responses parity with `ccBuildRequest` / `respBuildRequest` |
| `provider/openai/unified_event_test.go` | Full completions + responses event pipelines via unified |
| `provider/openrouter/unified_request.go` | Messages + Responses unified body builders |
| `provider/openrouter/unified_messages_test.go` | Messages body parity |
| `provider/openrouter/unified_integration_test.go` | Responses body via live `CreateStream` path |
| `provider/openrouter/unified_event_test.go` | `EventFromResponses` contracts + `StopReason` mapping |
| `provider/ollama/unified_event_test.go` | Completions event pipeline (compatible path) |
| `provider/ollama/unified_request_test.go` | Parity test (see: known divergence below) |

**Actual wire paths migrated** (production code):
- `provider/openai/api_completions.go` → uses `buildCompletionsBodyUnified`
- `provider/openai/api_responses.go` → uses `buildResponsesBodyUnified`
- `provider/openrouter/api_messages.go` → uses `buildOpenRouterMessagesBodyUnified`
- `provider/openrouter/api_responses.go` → uses `buildOpenRouterResponsesBodyUnified`

---

## Test Status

| Package | Status | Notes |
|---------|--------|-------|
| `api/unified` | ✅ pass (race) | All 11 tests green |
| `api/messages` | ✅ pass | Unchanged |
| `api/completions` | ✅ pass | Unchanged |
| `api/responses` | ✅ pass | Unchanged |
| `api/apicore` | ✅ pass | Unchanged |
| `provider/anthropic` | ✅ pass | All existing + new unified tests |
| `provider/minimax` | ✅ pass | All existing + new unified tests |
| `provider/openrouter` | ✅ pass | All existing + new unified tests |
| `provider/ollama` | ⚠️ **1 failing** | `TestBuildRequestUnified_Parity` — intended divergence (see below) |
| `provider/openai` | ⚠️ **2 failing** | `TestMapEffortAndThinking_UnknownModel` (pre-existing, unrelated); `TestCodexAuth_Stream_ChatCompletions` (external API rate-limit) |

---

## Known Shortcomings / Sacrifices Made

### 1. Ollama unified parity test is incorrect
`TestBuildRequestUnified_Parity` in `provider/ollama` compares Ollama's legacy wire format (`num_predict`, `format: "json"`, no `stream_options`) against `RequestToCompletions` output, which produces standard OpenAI Chat Completions format (`max_tokens`, `response_format`, `stream_options`). These are **intentionally different** — Ollama uses a proprietary dialect of the Chat Completions JSON schema. The test was written incorrectly and is currently failing.

**Fix needed**: Ollama requires its own `RequestToOllamaCompletions` variant (or a completions option/transform) that emits the Ollama wire dialect. The parity test should compare against the unified-path output, not the legacy output. Alternatively, the test should document the expected Ollama-specific fields and validate them independently.

### 2. `EventFromMessages` for `MessageDeltaEvent` emits empty `Usage`
The messages protocol `message_delta` carries the actual `stop_reason` and `output_tokens`, but `EventFromMessages` currently returns an empty `Usage{}` struct for this event. The stop reason and token counts are not extracted. This means providers still using `EventFromMessages` alone will produce incomplete usage records.

**Fix needed**: `EventFromMessages` should extract `StopReason` from `MessageDeltaEvent.Delta.StopReason` and set the `Completed` field (or a combined event). Token counts from `MessageDeltaEvent.Usage` should populate the `Usage.Tokens` field.

### 3. No `RequestFromLLM` enrichment for OpenAI models (`enrichOpts`)
The OpenAI provider applies `enrichOpts` (reasoning effort mapping, Codex strip, cache-retention set) before building the wire request. `buildCompletionsBodyUnified` / `buildResponsesBodyUnified` call `RequestFromLLM` directly, bypassing `enrichOpts`. This means the parity tests pass only because the test inputs are pre-enriched. In production, providers still call `enrichOpts` separately before these functions, so the pipeline is correct — but the enrichment is not yet inside unified.

**Fix needed**: Decide whether enrichment belongs in `unified.RequestFromLLM` (generic) or stays provider-specific (correct). Document clearly. Currently it works because providers enrich before calling unified, but it's implicit.

### 4. `Publish()` does not forward `ProviderName`, `Model`, `RequestID` from context
When a provider builds a `StreamEvent.Usage`, they typically set `Provider`, `Model`, `RequestID`. Currently `EventFromCompletions` and `EventFromResponses` set an empty `Usage{}` — the `Provider` and other dims are never populated. `Publish` faithfully forwards what it gets, which will be empty strings in usage records emitted via unified.

**Fix needed**: Event converters should accept contextual metadata (or providers should enrich the returned `Usage` struct before calling `Publish`).

### 5. `isEffortSupportedByAnthropicModel` is embedded in `api/unified`
Model-capability detection (`isAdaptiveThinkingSupportedByAnthropicModel`, `isEffortSupportedByAnthropicModel`, `isMaxEffortSupportedByAnthropicModel`) lives in `api/unified/messages_api.go`. This is a violation of the dependency direction — `api/unified` should not embed model catalog knowledge. It currently duplicates (slightly different) logic from `provider/anthropic/request.go`.

**Fix needed**: Extract model capabilities into a shared `modelinfo` package (or into `api/unified` as an explicit callable that providers can override/inject), not hard-coded string checks.

### 6. `MessageStartEvent` on completions is conflated with started+delta
`EventFromCompletions` returns a single `StreamEvent` with both `.Started` and `.Delta` set if the first chunk carries both an ID and content. `Publish` then emits both a `started` and a `delta` event from one `StreamEvent`. This is correct behavior, but subtle and worth documenting explicitly.

### 7. Ollama event pipeline test uses Chat Completions parser, not native Ollama NDJSON
`TestUnifiedCompletionsEventPipeline_OllamaCompatible` feeds OpenAI-format SSE through `completions.Client` to test the unified event pipeline in the Ollama context. Ollama's actual stream is NDJSON, not SSE with OpenAI format. The test validates the unified pipeline works but does not validate actual Ollama stream parsing.

### 8. `TestMapEffortAndThinking_UnknownModel` regression (unrelated but visible)
The test expects an error from `mapEffortAndThinking("unknown-model", ...)` but a pre-existing change in `provider/openai/models.go` (part of dockermr integration) changed that to return `nil`. This is not our regression — it's a separate in-flight change — but it must be resolved before merging.

---

## What Is Next

### Task A — Fix Ollama wire dialect (blocker for Ollama migration)
**Goal**: Ollama uses a proprietary Chat Completions dialect (`num_predict`, `format: "json"`, no `stream_options`). Options:
1. Add `OllamaOption` transforms to `RequestToCompletions` that remap fields post-conversion.
2. Create `RequestToOllamaCompletions` in `api/unified` as a dedicated converter.
3. Keep Ollama on its legacy builder and only use unified for the event path.

**Recommendation**: Option 3 for now — Ollama event path uses unified already; request path keeps legacy until an Ollama-specific completions converter is justified.

**Files**: `provider/ollama/unified_request_test.go` (fix/clarify the test intent)

### Task B — Fix `EventFromMessages` for `MessageDeltaEvent`
Fill in the stop reason and output token count from `MessageDeltaEvent`:
```go
case *messages.MessageDeltaEvent:
    idx := uint32(e.Index)
    ev := StreamEvent{
        Type: StreamEventCompleted,
        Completed: &Completed{StopReason: llm.StopReason(e.Delta.StopReason)},
        Usage: &Usage{
            Tokens: usage.TokenItems{
                {Kind: usage.KindOutput, Count: e.Usage.OutputTokens},
                {Kind: usage.KindCacheCreate, Count: e.Usage.CacheCreationInputTokens},
                {Kind: usage.KindCacheRead, Count: e.Usage.CacheReadInputTokens},
            }.NonZero(),
        },
    }
    return ev, false, nil
```
Add tests asserting stop reason and token counts survive through the pipeline.

### Task C — Contextual metadata injection into `Usage` events
Providers need to supply `Provider`, `Model`, `RequestID` when building usage records. Two options:
1. Add `WithPublishContext(provider, model, requestID string)` option to `EventFrom*` converters.
2. Have providers enrich the returned `Usage` after the conversion, before calling `Publish`.

Option 2 is simpler and keeps converters pure. Document the pattern in code.

### Task D — Extract model capabilities out of `api/unified`
Create `api/unified/modelcaps.go` with a clean interface:
```go
type ModelCaps struct {
    SupportsAdaptiveThinking bool
    SupportsEffort           bool
    SupportsMaxEffort        bool
}

type ModelCapsResolver func(model string) ModelCaps
```
Inject via `MessagesOption`. Provide a default resolver that codifies the current model-string checks. This makes the package testable without hard-coded model strings.

### Task E — Anthropic provider stream path migration
Replace `provider/anthropic/anthropic.go`'s internal `BuildRequest` + `ParseStreamWith` calls with the unified pipeline:
```go
uReq, _ := unified.RequestFromLLM(opts)
wireReq, _ := unified.RequestToMessages(uReq)
handle, _ := messagesClient.Stream(ctx, wireReq)
for r := range handle.Events {
    uEv, ignored, _ := unified.EventFromMessages(r.Event)
    if !ignored { unified.Publish(pub, uEv) }
}
```
This is the primary payoff of the unified layer — provider code becomes ~15 lines.
**Prerequisite**: Task B must be done first (correct MessageDeltaEvent handling).

### Task F — Minimax / OpenRouter messages path migration
Same as Task E but for:
- `provider/minimax/minimax.go` (currently imports `provider/anthropic` directly)
- `provider/openrouter/api_messages.go` (already using unified request body, but stream parsing still uses `anthropic.ParseStreamWith`)

### Task G — Ollama stream path migration
Replace `provider/ollama/ollama.go`'s `parseStream` (bespoke NDJSON parser) with the `api/completions` parser + unified event path. Ollama `/api/chat` produces Chat Completions-compatible NDJSON; the `completions.NewParser` can already handle it.
**Prerequisite**: Validate this experimentally with a fixture test.

### Task H — Legacy builder deprecation
Once Tasks E–G are done, mark `anthropic.BuildRequest`, `anthropic.ParseStreamWith`, and the per-provider duplicate request builders with deprecation comments. Do not delete yet — they're used by tests and OpenRouter until fully migrated.

### Task I — Commit and cleanup
- Fix `TestBuildRequestUnified_Parity` in ollama (document intent or remove the incorrect comparison)
- Document `TestMapEffortAndThinking_UnknownModel` as a pre-existing separate regression
- Commit the current work: `feat(api/unified): implement event schema, protocol event converters, and publisher bridge`
- Commit provider build hookup: `feat(providers): wire openai+openrouter request paths through api/unified`

---

## Priority Order

| Priority | Task | Effort | Risk |
|----------|------|--------|------|
| 🔴 Commit now | Task I | 1h | Low |
| 🔴 Fix | Task B (`MessageDeltaEvent`) | 1h | Low |
| 🟡 High | Task E (anthropic migration) | 3h | Medium |
| 🟡 High | Task F (minimax/openrouter migration) | 2h | Medium |
| 🟠 Medium | Task C (usage metadata) | 1h | Low |
| 🟠 Medium | Task A (ollama wire dialect) | 2h | Low |
| 🟠 Medium | Task G (ollama stream migration) | 2h | Medium |
| 🟢 Later | Task D (model caps resolver) | 2h | Low |
| 🟢 Later | Task H (legacy deprecation) | 1h | Low |
