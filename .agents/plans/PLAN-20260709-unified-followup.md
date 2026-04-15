# Plan: api/unified — Follow-up Work

**Date**: 2025-07-09
**Updated**: 2025-07-09
**Status**: Active
**Scope**: Complete `api/unified` package, integrate it into all providers, and remove legacy duplication.

---

## What Was Done (Completed)

### Phase 1 — Design Alignment ✅
- Cleaned `DESIGN-api-extraction.md`, `DESIGN-api-unified.md`, and `PLAN-20260415-unified.md`
  to consistently reference `api/unified` (not the retired `api/adapt` concept).
- Produced `api/README.md` describing the four-layer architecture.

### Phase 2 — `api/unified` Package (request layer) ✅
- `types_request.go` — canonical `Request`, `Message`, `Part`, `Tool`, `ToolCall`, etc.
- `types_extensions.go` — `RequestExtras`
- `validate.go` — `Request.Validate()`
- `llm_bridge.go` — `RequestFromLLM` / `RequestToLLM`
- `messages_api.go` — `RequestToMessages` / `RequestFromMessages`
- `completions_api.go` — `RequestToCompletions` / `RequestFromCompletions`
- `responses_api.go` — `RequestToResponses` / `RequestFromResponses`
- `model_caps.go` — `ModelCaps`, `ModelCapsFunc`, `DefaultAnthropicModelCaps`, `WithModelCaps`

### Phase 3 — `api/unified` Package (event layer) ✅
- `types_event.go` — canonical `StreamEvent`, `Delta`, `Started`, `Usage`, `Completed`, `StreamError`
- `publisher_bridge.go` — `Publish(pub, ev)` → `llm.Publisher`
- `EventFromMessages` — with `mapMessagesStopReason`; `MessageStartEvent` emits input/cache tokens,
  `MessageDeltaEvent` emits output tokens + stop reason
- `EventFromCompletions` — with `mapOpenAIFinishReason`
- `EventFromResponses`

### Phase 4 — Stream helpers ✅
- `stream_helpers.go` — `StreamMessages` / `StreamCompletions` / `StreamResponses`
- `StreamContext` — carries provider, model, upstream provider, cost calculator, rate limits
- Combined usage record emitted at end-of-stream
- Model resolution emits `ModelResolvedEvent` automatically
- Rate limits forwarded into `Started.Extra` and usage record `Extras`

### Phase 5 — Provider migration (partial) ✅
Request path fully on unified; event path mixed:

| Provider | Request path | Event path |
|----------|-------------|------------|
| `provider/anthropic` | ✅ unified | ✅ `StreamMessages` |
| `provider/minimax` | ✅ unified | ✅ `StreamMessages` |
| `provider/openrouter` (messages) | ✅ unified | ✅ `StreamMessages` |
| `provider/openrouter` (responses) | ✅ unified | ❌ still `openai.RespParseStream` |
| `provider/openai` (completions) | ✅ unified | ❌ still `ccParseStream` |
| `provider/openai` (responses) | ✅ unified | ❌ still `RespParseStream` |
| `provider/anthropic/claude` | ❌ still `anthropic.BuildRequest` | ❌ still `anthropic.ParseStreamWith` |
| `provider/ollama` | ❌ legacy dialect | ✅ unified event path (experimental test) |

---

## Current Test Status

| Package | Status | Notes |
|---------|--------|-------|
| `api/unified` | ✅ pass (race) | All tests green |
| `api/messages` | ✅ pass | |
| `api/completions` | ✅ pass | |
| `api/responses` | ✅ pass | |
| `api/apicore` | ✅ pass | |
| `provider/anthropic` | ✅ pass | |
| `provider/anthropic/claude` | ✅ pass | (event path not yet migrated, passing legacy tests) |
| `provider/minimax` | ✅ pass | |
| `provider/openrouter` | ✅ pass | |
| `provider/ollama` | ✅ pass | |
| `provider/openai` | ⚠️ 2 failing | Both pre-existing, unrelated to unified work — see §Known Issues |

---

## Known Issues (not introduced by unified work)

### KI-1: `TestMapEffortAndThinking_UnknownModel` (provider/openai)
An in-flight change to `provider/openai/models.go` (dockermr integration) changed
`mapEffortAndThinking` to return nil for unknown models. The test expects an error.
Not ours to fix; tracked in dockermr plan.

### KI-2: `TestCodexAuth_Stream_ChatCompletions` (provider/openai)
Hits the real OpenAI API and fails with `insufficient_quota` (429).
Not a code issue; skipped in offline CI.

---

## Open Gaps

### Gap 1 — `provider/openai` event path not migrated
`ccParseStream` (completions) and `RespParseStream` (responses) are still bespoke in-provider
parsers. Request body already uses unified; event path does not.

- `ccParseStream`: ~110 lines of custom SSE parsing + tool accumulation + usage mapping
- `RespParseStream`: ~200 lines; exported so openrouter reuses it → must be migrated together

### Gap 2 — `provider/openrouter` responses event path not migrated
`provider/openrouter/api_responses.go` still calls `openai.RespParseStream`.
Depends on Gap 1 (can only be done once RespParseStream is replaced).

### Gap 3 — `provider/anthropic/claude` not migrated at all
`claude/provider.go` still calls:
- `anthropic.BuildRequest(RequestOptions{…})` → must become `unified.RequestToMessages`
- `anthropic.ParseStreamWith` → must become `unified.StreamMessages`
- `anthropic.DoCountTokensAPI(… anthropic.Request)` → now takes `*messages.Request`; signature
  mismatch since we updated `DoCountTokensAPI` but `claude` still builds an `anthropic.Request`

### Gap 4 — `singleResponseTransport` duplicated in 3 places
Same struct + `RoundTrip` defined independently in:
- `provider/anthropic/anthropic.go`
- `provider/minimax/minimax.go`
- `provider/openrouter/api_messages.go`

Should move to `api/apicore` as `apicore.SingleResponseTransport` (or a constructor).

### Gap 5 — Legacy builders still alive in `provider/anthropic`
`anthropic.BuildRequest`, `anthropic.BuildRequestBytes`, `provider/anthropic.Request`,
and the associated internal wire types (`Message`, `TextBlock`, `ToolUseBlock`, etc.)
are no longer used by `anthropic.go` itself. Still referenced by:
- `claude/provider.go` (Gap 3)
- `minimax/unified_request_test.go` (test helper — should use `api/messages` types directly)
- `openrouter/unified_legacy_helpers_test.go` (idem)

Removal blocked on Gap 3.

### Gap 6 — `anthropic.ParseStreamWith` / `stream_processor.go` still alive
`ParseStreamWith` is called only by `claude/provider.go` (Gap 3).
`stream_processor.go` has comprehensive tests but no production caller post-migration.
Both can be deleted once Gap 3 is done.

### Gap 7 — Dead legacy builders in `provider/openai`
`ccBuildRequest` and `respBuildRequest` are no longer called by production code.
Only referenced by the parity tests in `unified_request_test.go`.
Options: remove (update tests to compare against wire JSON directly) or keep as spec documentation.

### Gap 8 — `orRespBuildRequest` dead in `provider/openrouter`
Same situation as Gap 7. Only referenced from `unified_legacy_helpers_test.go`.

### Gap 9 — `StreamMessages` uses `singleResponseTransport` anti-pattern
Providers do an HTTP call, get `*http.Response`, then wrap it in a fake transport and feed it
back into `messages.Client.Stream()` to get a `StreamHandle`. This is a workaround because
`StreamMessages` accepts a `*apicore.StreamHandle`, not a raw `io.ReadCloser`.

Cleaner design: `StreamMessages` (and `StreamCompletions`, `StreamResponses`) should accept
an `io.ReadCloser` + `http.Header` directly, removing the roundabout fake transport.
This is a design improvement, not a bug — current behavior is correct.

### Gap 10 — Ollama request path uses legacy dialect
Ollama's `/api/chat` uses `num_predict` / `format: "json"` instead of OpenAI-standard fields.
`RequestToCompletions` produces standard OpenAI format and cannot be used as-is.
Options:
- Add `RequestToOllamaCompletions` with Ollama-specific field mapping
- Use `WithTransform` on `completions.Client` to remap post-serialization
- Keep legacy `buildRequest` for Ollama (current state — acceptable for now)

---

## OpenRouter Migration (Next Target)

OpenRouter routes to multiple upstream backends. It currently has **two stream paths**:

### Path A — Messages API (`/v1/messages`)
Used for Anthropic models (e.g. `anthropic/claude-opus-4-5`).

**Current state**: ✅ fully migrated.
- Request: `buildOpenRouterMessagesBodyUnified` → `api/unified.RequestToMessages`
- Event: `unified.StreamMessages` with `UpstreamProvider: "anthropic"`

No further work needed on this path.

### Path B — Responses API (`/v1/responses`)
Used for OpenAI models (e.g. `openai/gpt-5.4`).

**Current state**: ❌ event path not migrated.
- Request: ✅ `buildOpenRouterResponsesBodyUnified` → `api/unified.RequestToResponses`
- Event: ❌ `openai.RespParseStream(ctx, resp.Body, pub, openai.RespStreamMeta{…})`

**Files involved**:
- `provider/openrouter/api_responses.go` — `streamResponses` function
- `provider/openai/api_responses.go` — `RespParseStream` and `RespStreamMeta` (exported, used by openrouter)

**Migration plan**:

1. Switch `provider/openrouter/api_responses.go::streamResponses` from `openai.RespParseStream`
   to `unified.StreamResponses`:

   ```go
   // before
   openai.RespParseStream(ctx, resp.Body, pub, openai.RespStreamMeta{
       RequestedModel:   opts.Model,
       ProviderName:     providerName,
       UpstreamProvider: "openai",
   })

   // after
   respClient := responses.NewClient(
       responses.WithBaseURL(p.opts.BaseURL),
       responses.WithHTTPClient(&http.Client{
           Transport: &singleResponseTransport{resp: resp},
       }),
   )
   wireReq := &responses.Request{Model: opts.Model, Stream: true}
   handle, err := respClient.Stream(ctx, wireReq)
   …
   go func() {
       defer pub.Close()
       unified.StreamResponses(ctx, handle, pub, unified.StreamContext{
           Provider:         providerName,
           Model:            opts.Model,
           UpstreamProvider: "openai",
       })
   }()
   ```

2. Once openrouter is off `RespParseStream`, check if `provider/openai` still needs it exported
   (it does, until Gap 1 is done — `provider/openai` still calls its own `RespParseStream`).

**Parity test already exists**:
- `provider/openrouter/unified_integration_test.go` — `TestStreamResponsesUnified_RequestBodyParity`
  validates the request body. Extend to cover event output parity.
- Add `TestStreamResponsesUnified_EventPipeline` that feeds a canned SSE fixture and asserts
  the same events come out as the legacy path.

**Acceptance criteria**:
- `provider/openrouter` passes all existing tests after migration.
- `openai.RespParseStream` is no longer imported by `provider/openrouter`.
- New test covers token counts, stop reason, model resolution for responses path.

---

## Task List (updated)

### ✅ Done
- Task B — `EventFromMessages` fixed (MessageDeltaEvent tokens + stop reason)
- Task C — Usage metadata injection solved via `StreamContext`
- Task D — Model caps extracted to `ModelCaps` / `ModelCapsFunc` / `DefaultAnthropicModelCaps`
- Task E — Anthropic provider migrated
- Task F — Minimax + openrouter-messages migrated
- Task I — Committed

### 🔴 Next
- **Task OR** — OpenRouter responses event path migration (see §OpenRouter Migration above)

### 🟡 High
- **Task OI** — OpenAI event path migration (`ccParseStream` + `RespParseStream`)
  - Prerequisite: none (can be done independently of openrouter)
  - Approach: same `singleResponseTransport` pattern as anthropic/minimax
  - After: `RespParseStream` can be un-exported; openrouter already off it (after Task OR)

### 🟠 Medium
- **Task CL** — `provider/anthropic/claude` migration
  - Blocks: removal of `anthropic.BuildRequest` + `ParseStreamWith`
  - Note: `DoCountTokensAPI` already updated to take `*messages.Request`; must update
    `claude/provider.go::buildRequest` to return `*messages.Request` instead of `anthropic.Request`
- **Task G** — Ollama stream path migration (NDJSON → `completions.Client` + `StreamCompletions`)
  - Prerequisite: fixture test validating `completions.NewParser` handles Ollama NDJSON

### 🟢 Later (cleanup, no behavior change)
- **Task S4** — Deduplicate `singleResponseTransport` → `apicore.SingleResponseTransport`
- **Task S5** — Remove `anthropic.BuildRequest` + `ParseStreamWith` + `stream_processor.go`
  (blocked on Task CL)
- **Task S6** — Remove `orRespBuildRequest` from openrouter (legacy test helper)
- **Task S7** — Remove `ccBuildRequest` + `respBuildRequest` from openai (or keep as spec docs)
- **Task S8** — Redesign `StreamMessages` to accept `io.ReadCloser` + `http.Header` directly
  (removes fake transport pattern; non-breaking API change)
- **Task A** — Ollama request path (`RequestToOllamaCompletions` or keep legacy)

---

## Priority Order

| Priority | Task | Effort | Depends on |
|----------|------|--------|-----------|
| 🔴 Next | **Task OR** — openrouter responses event | S | — |
| 🟡 High | **Task OI** — openai event path | M | — |
| 🟠 Medium | **Task CL** — claude provider | M | — |
| 🟠 Medium | **Task G** — ollama stream | M | fixture test |
| 🟢 Later | **Task S4** — dedup `singleResponseTransport` | S | — |
| 🟢 Later | **Task S5** — remove anthropic legacy | S | CL |
| 🟢 Later | **Task S6/S7** — remove dead builders | S | OR + OI |
| 🟢 Later | **Task S8** — stream helper redesign | L | OR + OI + CL |
| 🟢 Later | **Task A** — ollama request dialect | M | — |
