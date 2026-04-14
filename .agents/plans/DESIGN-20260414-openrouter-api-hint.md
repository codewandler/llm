# Design: ApiType — Multi-Backend Dispatch for OpenRouter (and beyond)

**Date**: 2026-07-14  
**Status**: Draft — awaiting approval

---

## Problem Statement

The OpenRouter provider currently uses only the OpenAI Chat Completions API
(`/v1/chat/completions`) for all models. OpenRouter natively supports two
additional wire protocols:

1. **OpenAI Responses API** (`/v1/responses`) — required for the `phase` field
   on gpt-5.3-codex, gpt-5.4, and gpt-5.4-pro; enables richer multi-turn
   agentic workflows that can degrade without it (see the
   [GPT-5.4 migration guide](https://openrouter.ai/docs/guides/evaluate-and-optimize/model-migrations/gpt-5-4)).

2. **Anthropic Messages API** (`/v1/messages`) — native Anthropic protocol for
   Claude models; exposes `cache_control`, thinking blocks, and other
   Anthropic-native features without Chat Completions translation overhead.

Using the right protocol for each model class improves fidelity and enables
model-native features. Surfacing the chosen API in `StreamStartedEvent` gives
consumers (CLI, evaluation harnesses) full observability into which wire
protocol was actually used — useful for comparing outputs across backends.

Beyond OpenRouter, a general `ApiType` field on `llm.Request` gives callers
explicit control without coupling to provider internals. The direct OpenAI
provider already auto-dispatches between Chat Completions and Responses, but
today the caller cannot override that choice.

---

## Scope

**In scope:**

- New `ApiType` type and `ApiType` field on `llm.Request` (with validation +
  `UnmarshalText` for cobra)
- `ApiType` field on `StreamStartedEvent` — every provider sets it; consumers
  see which API was actually used regardless of what was requested
- `--api` flag on `llmcli infer`
- `ApiType()` fluent setter on `RequestBuilder` + `WithApiType` functional option
- OpenRouter: dispatch to Responses API, Anthropic Messages API, or Chat
  Completions based on `ApiType`; smart auto-dispatch in `ApiTypeAuto` mode
- New `provider/openrouter/api_responses.go`
- New `provider/openrouter/api_messages.go`
- **Bug fix** (identified during this design): `parseStream` and `onError` in
  `provider/anthropic` hardcode `llm.ProviderNameAnthropic` in error
  constructors instead of using `opts.ProviderName` — this must be fixed here
  because `api_messages.go` reuses those code paths with `ProviderName =
  "openrouter"`

**Out of scope (future work):**

- OpenAI provider respecting `ApiTypeOpenAIChatCompletion` to force downgrade
- Bedrock or Anthropic direct providers reading `ApiType` from `Request`
  (they have a fixed protocol; they ignore it on input but set it on output)
- `phase` field round-trip in `llm.Message` / `msg.Part`

---

## Current State

| Layer | Status |
|---|---|
| OpenRouter provider | Chat Completions only |
| OpenRouter `/v1/responses` | Supported by OpenRouter; unused |
| OpenRouter `/v1/messages` | Supported by OpenRouter; unused |
| `llm.Request` | No API selection field |
| `StreamStartedEvent` | No `ApiType` field |
| `llmcli infer` | No `--api` flag |
| OpenAI provider | Auto-dispatches Chat Completions ↔ Responses; no caller override |
| `anthropic.ParseOpts.ProviderName` | ✅ exists; used by MiniMax already |
| `anthropic.ParseStreamWith` | ✅ already exported |
| `anthropic.BuildRequestBytes` | ✅ already exported |
| `anthropic.parseStream` error constructors | ❌ hardcode `ProviderNameAnthropic` |

---

## Design

### 1. `ApiType` type — `request.go`

```go
// ApiType selects the wire protocol a provider should use when it supports
// multiple API backends for the same model. Providers ignore values they do
// not implement. The resolved ApiType is always reported in StreamStartedEvent.
type ApiType string

const (
    // ApiTypeAuto lets the provider select the best API. This is the default.
    ApiTypeAuto ApiType = ""
    // ApiTypeOpenAIChatCompletion requests the OpenAI Chat Completions API
    // (/v1/chat/completions). Supported by: OpenRouter.
    ApiTypeOpenAIChatCompletion ApiType = "openai-chat"
    // ApiTypeOpenAIResponses requests the OpenAI Responses API (/v1/responses).
    // Required for models that use the phase field (gpt-5.3-codex, gpt-5.4-*).
    // Supported by: OpenRouter, OpenAI direct.
    ApiTypeOpenAIResponses ApiType = "openai-responses"
    // ApiTypeAnthropicMessages requests the Anthropic Messages API (/v1/messages).
    // Provides native cache_control, thinking blocks, and anthropic-beta headers.
    // Supported by: OpenRouter (anthropic/* model IDs), Anthropic direct.
    ApiTypeAnthropicMessages ApiType = "anthropic-messages"
)

// Valid returns true if t is a known value or the zero value (auto).
func (t ApiType) Valid() bool {
    switch t {
    case ApiTypeAuto, ApiTypeOpenAIChatCompletion, ApiTypeOpenAIResponses, ApiTypeAnthropicMessages:
        return true
    default:
        return false
    }
}

// UnmarshalText implements encoding.TextUnmarshaler so ApiType works with
// cobra's TextVar flag binding.
func (t *ApiType) UnmarshalText(b []byte) error {
    v := ApiType(b)
    if !v.Valid() {
        return fmt.Errorf("invalid api type %q; must be one of: openai-chat, openai-responses, anthropic-messages, auto", v)
    }
    *t = v
    return nil
}
```

Add to `Request` struct (follows existing `Effort Effort`, `Thinking ThinkingMode` pattern):

```go
// ApiType selects which wire protocol to use when the provider supports
// multiple backends. Defaults to ApiTypeAuto (empty string = provider decides).
ApiType ApiType `json:"api_type,omitempty"`
```

Add to `Request.Validate()`:

```go
if !o.ApiType.Valid() {
    return fmt.Errorf("invalid ApiType %q; valid values: openai-chat, openai-responses, anthropic-messages, auto", o.ApiType)
}
```

### 2. `StreamStartedEvent` — `event.go`

```go
StreamStartedEvent struct {
    RequestID string `json:"request_id,omitempty"`

    // Model is the model identifier echoed back by the upstream API.
    Model string `json:"model,omitempty"`

    // ApiType is the wire protocol that was actually used for this stream.
    // Always set by the provider. Consumers can rely on this to know which
    // API backend processed the request, regardless of what was requested
    // via llm.Request.ApiType.
    ApiType ApiType `json:"api_type,omitempty"`

    // Extra holds provider-specific data such as rate-limit headers.
    Extra map[string]any `json:"extra,omitempty"`
}
```

Every provider sets `ApiType` on `pub.Started(...)`. The value is concrete —
never `ApiTypeAuto`:

| Provider | `ApiType` value set |
|---|---|
| `provider/openrouter` | Dynamic: `ApiTypeOpenAIChatCompletion`, `ApiTypeOpenAIResponses`, or `ApiTypeAnthropicMessages` based on `selectAPI` |
| `provider/openai` | `ApiTypeOpenAIResponses` (Responses path) or `ApiTypeOpenAIChatCompletion` (Chat Completions path) |
| `provider/anthropic` | `ApiTypeAnthropicMessages` |
| `provider/bedrock` | `ApiTypeAnthropicMessages` |
| `provider/minimax` | `ApiTypeAnthropicMessages` |
| `provider/ollama` | `ApiTypeOpenAIChatCompletion` |
| `provider/fake` | `ApiTypeOpenAIChatCompletion` |

The resolved `ApiType` is threaded into stream parsers via metadata structs
(see §5–7 below); `pub.Started()` is called inside the parsers, not from
`CreateStream` itself.

### 3. `RequestBuilder` changes — `request_builder.go`

Fluent method (follows existing `Effort`/`Thinking`/`OutputFormat` pattern):

```go
func (b *RequestBuilder) ApiType(t ApiType) *RequestBuilder {
    b.req.ApiType = t
    return b
}
```

Functional option:

```go
func WithApiType(t ApiType) RequestOption {
    return func(r *Request) { r.ApiType = t }
}
```

### 4. `llmcli infer` changes — `cmd/llmcli/cmds/infer.go`

Add to `inferOpts`:

```go
ApiType llm.ApiType // f.TextVar — zero value means auto
```

Add flag in `NewInferCmd`:

```go
f.TextVar(&opts.ApiType, "api", llm.ApiType(""),
    "API backend: openai-chat, openai-responses, anthropic-messages, auto")
```

Thread to the request builder (alongside `Effort`, `Thinking`, etc.):

```go
b = b.ApiType(opts.ApiType)
```

Verbose output — add to `printStreamStartedEvent`:

```go
if ev.ApiType != "" {
    fields = append(fields, kvField{"api", string(ev.ApiType)})
}
```

The `── request params ──` block picks up `api_type` automatically via
`mapFromStruct` because it reads all non-zero JSON fields from `OriginalRequest`.

---

### 5. OpenRouter dispatch — `provider/openrouter/openrouter.go`

#### 5a. `selectAPI`

```go
type orAPIBackend int

const (
    orChatCompletions orAPIBackend = iota
    orResponses
    orMessages
)

// selectAPI resolves the effective OpenRouter API backend and the concrete
// ApiType to report in StreamStartedEvent.
//
// Auto-dispatch rules:
//   - openai/* models that require Responses API  →  Responses API
//   - openai/* other                              →  Chat Completions
//   - anthropic/*                                 →  Anthropic Messages API
//   - everything else                             →  Chat Completions
func selectAPI(model string, t llm.ApiType) (orAPIBackend, llm.ApiType) {
    switch t {
    case llm.ApiTypeOpenAIResponses:
        return orResponses, llm.ApiTypeOpenAIResponses
    case llm.ApiTypeAnthropicMessages:
        return orMessages, llm.ApiTypeAnthropicMessages
    case llm.ApiTypeOpenAIChatCompletion:
        return orChatCompletions, llm.ApiTypeOpenAIChatCompletion
    }
    // ApiTypeAuto: infer from model prefix.
    if bare, ok := strings.CutPrefix(model, "openai/"); ok {
        if orUseResponsesAPI(bare) {
            return orResponses, llm.ApiTypeOpenAIResponses
        }
        return orChatCompletions, llm.ApiTypeOpenAIChatCompletion
    }
    if strings.HasPrefix(model, "anthropic/") {
        return orMessages, llm.ApiTypeAnthropicMessages
    }
    return orChatCompletions, llm.ApiTypeOpenAIChatCompletion
}

// orUseResponsesAPI reports whether the bare model ID (without "openai/" prefix)
// requires the Responses API on OpenRouter.
// Keep in sync with provider/openai.useResponsesAPI (cross-reference comment).
func orUseResponsesAPI(bare string) bool {
    switch bare {
    case "gpt-5.4", "gpt-5.4-mini", "gpt-5.4-nano", "gpt-5.4-pro",
        "gpt-5.3-codex",
        "gpt-5.2-codex",
        "gpt-5.1-codex", "gpt-5.1-codex-max", "gpt-5.1-codex-mini",
        "gpt-5-codex":
        return true
    }
    return false
}
```

Note: uses `strings.CutPrefix` (Go 1.20+) instead of the hand-rolled helper
from v2.

#### 5b. `CreateStream` dispatch

```go
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
    opts, err := src.BuildRequest(ctx)
    // ... existing: normalise, validate, resolve API key ...

    backend, resolvedApiType := selectAPI(opts.Model, opts.ApiType)

    pub, ch := llm.NewEventPublisher()
    // ... existing: emit ModelResolved, RequestEvent, token estimates ...

    switch backend {
    case orResponses:
        go p.streamResponses(ctx, opts, resolvedApiType, apiKey, pub)
    case orMessages:
        go p.streamMessages(ctx, opts, resolvedApiType, apiKey, pub)
    default: // orChatCompletions
        // HTTP request already constructed for the Chat Completions path
        go parseStream(ctx, resp.Body, pub, parseStreamMeta{
            requestedModel: opts.Model,
            apiType:        resolvedApiType,
            logger:         p.opts.Logger,
        })
    }
    return ch, nil
}
```

The `resolvedApiType` flows down into every goroutine so `pub.Started()` (called
inside the parser) can stamp it on `StreamStartedEvent`.

---

### 6. OpenRouter Responses API — `provider/openrouter/api_responses.go`

**Endpoint**: `POST {baseURL}/v1/responses`  
**Wire format**: identical to OpenAI Responses API  
**Auth**: `Authorization: Bearer <key>`

#### Reuse strategy: export `RespParseStream` from `provider/openai`

The Responses API SSE parser in `provider/openai/api_responses.go` is generic;
the only provider-specific references are `llm.ProviderNameOpenAI` and the
`prompt_cache_retention` logic (openai model registry; not relevant to
OpenRouter).

**Changes to `provider/openai/api_responses.go`:**

1. Rename the unexported `ccStreamMeta` alias → exported `RespStreamMeta` struct
   with `ProviderName string` and `ApiType llm.ApiType` fields.
2. Make `respParseStream` → exported `RespParseStream`.
3. Inside `RespParseStream`, when calling `pub.Started(...)`, set `ApiType:
   meta.ApiType`.

```go
// RespStreamMeta holds per-request metadata for the Responses API SSE parser.
type RespStreamMeta struct {
    RequestedModel string
    StartTime      time.Time
    ProviderName   string       // "openai" or "openrouter"
    ApiType        llm.ApiType  // stamped on StreamStartedEvent
    Logger         *slog.Logger
}

// RespParseStream reads a Responses API SSE body and publishes events.
// Exported so provider/openrouter can reuse the parse logic without duplication.
func RespParseStream(ctx context.Context, body io.ReadCloser, pub llm.Publisher, meta RespStreamMeta)
```

No logic changes — rename + export + thread `ApiType`.

**`streamResponses` in `provider/openrouter/api_responses.go`:**

```go
func (p *Provider) streamResponses(
    ctx context.Context,
    opts llm.Request,
    apiType llm.ApiType,
    apiKey string,
    pub llm.Publisher,
) {
    body, err := orRespBuildRequest(opts)
    if err != nil {
        pub.Error(llm.NewErrBuildRequest(providerName, err))
        pub.Close()
        return
    }

    req, err := http.NewRequestWithContext(ctx, "POST",
        p.opts.BaseURL+"/v1/responses", bytes.NewReader(body))
    if err != nil {
        pub.Error(llm.NewErrBuildRequest(providerName, err))
        pub.Close()
        return
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+apiKey)

    resp, err := p.client.Do(req)
    // ... error handling identical to Chat Completions path ...

    openai.RespParseStream(ctx, resp.Body, pub, openai.RespStreamMeta{
        RequestedModel: opts.Model,
        StartTime:      time.Now(),
        ProviderName:   providerName, // "openrouter"
        ApiType:        apiType,
        Logger:         p.opts.Logger,
    })
}
```

**`orRespBuildRequest`**: copy of `openai.respBuildRequest` minus
`prompt_cache_retention` (OpenRouter does not surface that knob). Model ID is
sent as-is (`"openai/gpt-5.4"` — OpenRouter format). Marked with
`// TODO: consolidate with openai.respBuildRequest`.

---

### 7. OpenRouter Anthropic Messages API — `provider/openrouter/api_messages.go`

**Endpoint**: `POST {baseURL}/v1/messages`  
**Wire format**: Anthropic Messages API  
**Auth**: `Authorization: Bearer <key>` ← NOT `x-api-key`  
**Model**: strip `"anthropic/"` prefix before the request body

#### Reuse strategy: call existing exported functions directly

The anthropic package already exports everything needed:
- `anthropic.BuildRequestBytes(RequestOptions{LLMRequest: opts}) ([]byte, error)` — builds the JSON body
- `anthropic.ParseStreamWith(ctx, body, pub, ParseOpts{...})` — already used by MiniMax with a custom `ProviderName`
- `anthropic.AnthropicVersion` and `anthropic.BetaInterleavedThinking` — exported constants

No new exports or options are needed. `provider/openrouter` imports
`provider/anthropic` the same way `provider/minimax` already does.

**`streamMessages` in `provider/openrouter/api_messages.go`:**

```go
func (p *Provider) streamMessages(
    ctx context.Context,
    opts llm.Request,
    apiType llm.ApiType,
    apiKey string,
    pub llm.Publisher,
) {
    // Strip "anthropic/" prefix: OpenRouter's /v1/messages expects bare model IDs.
    opts.Model = strings.TrimPrefix(opts.Model, "anthropic/")

    body, err := anthropic.BuildRequestBytes(anthropic.RequestOptions{LLMRequest: opts})
    if err != nil {
        pub.Error(llm.NewErrBuildRequest(providerName, err))
        pub.Close()
        return
    }

    req, err := http.NewRequestWithContext(ctx, "POST",
        p.opts.BaseURL+"/v1/messages", bytes.NewReader(body))
    if err != nil {
        pub.Error(llm.NewErrBuildRequest(providerName, err))
        pub.Close()
        return
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+apiKey)          // OpenRouter auth
    req.Header.Set("Anthropic-Version", anthropic.AnthropicVersion)
    req.Header.Set("Anthropic-Beta", anthropic.BetaInterleavedThinking)

    resp, err := p.client.Do(req)
    // ... error handling ...

    // ParseStreamWith is the same path MiniMax uses; ProviderName controls
    // the label on all events (errors, usage records, model-resolved).
    anthropic.ParseStreamWith(ctx, resp.Body, pub, anthropic.ParseOpts{
        Model:        opts.Model,
        ProviderName: providerName, // "openrouter"
        ApiType:      apiType,      // ApiTypeAnthropicMessages — see §8 below
    })
}
```

No `anthropicBackend` field on `Provider`. No `sync.Once`. No event forwarding.
Clean and consistent with how MiniMax reuses the anthropic parser today.

---

### 8. Bug fix: anthropic `ProviderName` in error constructors

**File**: `provider/anthropic/stream.go` and `stream_processor.go`

`ParseOpts.ProviderName` was introduced so MiniMax could reuse the parser.
However, two call sites still hardcode `llm.ProviderNameAnthropic`:

```go
// stream.go — parseStream
pub.Error(llm.NewErrContextCancelled(llm.ProviderNameAnthropic, err)) // ❌
pub.Error(llm.NewErrStreamRead(llm.ProviderNameAnthropic, err))       // ❌

// stream_processor.go — onError
p.pub.Error(llm.NewErrProviderMsg(llm.ProviderNameAnthropic, evt.Error.Message)) // ❌
```

Fix: replace the hardcoded constant with `opts.ProviderName` (in `parseStream`)
and `p.meta.ProviderName` (in `onError`). This is a pure correctness fix; it
makes the existing MiniMax usage correct as well.

**Add `ApiType` to `ParseOpts` and `StreamStartedEvent` stamping:**

```go
// stream.go
type ParseOpts struct {
    Model           string
    ProviderName    string
    ApiType         llm.ApiType  // NEW: stamped on StreamStartedEvent
    ResponseHeaders map[string]string
    RequestParams   llm.ProviderRequest
    LLMRequest      llm.Request
}
```

In `streamProcessor.onMessageStart`:

```go
p.pub.Started(llm.StreamStartedEvent{
    Model:     evt.Message.Model,
    RequestID: evt.Message.ID,
    ApiType:   p.meta.ApiType,   // NEW
    Extra:     extra,
})
```

All existing callers (`provider/anthropic/anthropic.go`, `provider/minimax/`,
`provider/anthropic/claude/`) pass `ParseOpts{...}` with no `ApiType` field —
the zero value `ApiTypeAuto` / `""` would be wrong. They each get a concrete
constant added:

- `provider/anthropic/anthropic.go` → `ApiType: llm.ApiTypeAnthropicMessages`
- `provider/minimax/minimax.go` → `ApiType: llm.ApiTypeAnthropicMessages`
- `provider/anthropic/claude/provider.go` → `ApiType: llm.ApiTypeAnthropicMessages`

---

### 9. Auto-dispatch summary for OpenRouter

| `Request.ApiType` | Model prefix | API Used | `StreamStartedEvent.ApiType` |
|---|---|---|---|
| `openai-responses` | any | Responses | `openai-responses` |
| `anthropic-messages` | any | Messages | `anthropic-messages` |
| `openai-chat` | any | Chat Completions | `openai-chat` |
| `""` (auto) | `openai/gpt-5.4*`, `openai/*-codex*` | Responses | `openai-responses` |
| `""` (auto) | `openai/*` other | Chat Completions | `openai-chat` |
| `""` (auto) | `anthropic/*` | **Messages** | `anthropic-messages` |
| `""` (auto) | anything else | Chat Completions | `openai-chat` |

**Behaviour change in auto mode**: existing OpenRouter users calling
`anthropic/*` models move from Chat Completions to Anthropic Messages API.
Output is semantically equivalent or better; the change is observable via
`StreamStartedEvent.ApiType`. Documented in changelog as intentional.

---

## Files Changed

| File | Change |
|---|---|
| `request.go` | Add `ApiType` type, constants, `Valid()`, `UnmarshalText()`, field on `Request`, validation in `Validate()` |
| `request_builder.go` | Add `ApiType()` fluent method, `WithApiType()` functional option |
| `event.go` | Add `ApiType ApiType` field to `StreamStartedEvent` |
| `provider/anthropic/stream.go` | Add `ApiType llm.ApiType` to `ParseOpts`; fix `parseStream` error constructors to use `opts.ProviderName` |
| `provider/anthropic/stream_processor.go` | Set `ApiType: p.meta.ApiType` in `onMessageStart`; fix `onError` to use `p.meta.ProviderName` |
| `provider/anthropic/anthropic.go` | Pass `ApiType: llm.ApiTypeAnthropicMessages` in `ParseOpts` |
| `provider/anthropic/claude/provider.go` | Pass `ApiType: llm.ApiTypeAnthropicMessages` in `ParseOpts` |
| `provider/minimax/minimax.go` | Pass `ApiType: llm.ApiTypeAnthropicMessages` in `ParseOpts` |
| `provider/openai/api_responses.go` | Export `RespStreamMeta` (add `ApiType`, `ProviderName`); export `RespParseStream`; set `ApiType` on `pub.Started()` |
| `provider/openai/openai.go` | Pass correct `ApiType` (`ApiTypeOpenAIResponses` / `ApiTypeOpenAIChatCompletion`) when calling parsers |
| `provider/bedrock/bedrock.go` | Set `ApiType: llm.ApiTypeAnthropicMessages` in `pub.Started()` |
| `provider/ollama/ollama.go` | Set `ApiType: llm.ApiTypeOpenAIChatCompletion` in `pub.Started()` |
| `provider/fake/fake.go` | Set `ApiType: llm.ApiTypeOpenAIChatCompletion` in `pub.Started()` |
| `provider/openrouter/openrouter.go` | Add `selectAPI`, `orUseResponsesAPI`; refactor `CreateStream` dispatch; pass `resolvedApiType` + `apiType` to `parseStream` meta |
| `provider/openrouter/api_responses.go` | New: `streamResponses`, `orRespBuildRequest` |
| `provider/openrouter/api_messages.go` | New: `streamMessages` |
| `cmd/llmcli/cmds/infer.go` | Add `ApiType` to `inferOpts`, `--api` flag, builder thread-through, verbose display |

**No changes to**: `event_delta.go`, `msg/`, `tool/`, `provider/auto/`, `provider/router/`.

---

## Testing Strategy

### Unit tests

**`request_test.go`**:
- `TestApiType_Valid` — table-driven; all known values → true; unknown → false.
- `TestApiType_UnmarshalText` — valid values parse; unknown returns error.
- `TestRequest_Validate_ApiType` — valid passes; unknown fails with message.

**`provider/openrouter/openrouter_test.go`** — `selectAPI` table test:

| Test | Model | `ApiType` input | Expected backend | Expected resolved `ApiType` |
|---|---|---|---|---|
| explicit responses | any | `openai-responses` | `orResponses` | `openai-responses` |
| explicit messages | any | `anthropic-messages` | `orMessages` | `anthropic-messages` |
| explicit chat | any | `openai-chat` | `orChatCompletions` | `openai-chat` |
| auto / openai codex | `openai/gpt-5.3-codex` | `""` | `orResponses` | `openai-responses` |
| auto / gpt-5.4 | `openai/gpt-5.4` | `""` | `orResponses` | `openai-responses` |
| auto / gpt-4o | `openai/gpt-4o` | `""` | `orChatCompletions` | `openai-chat` |
| auto / anthropic | `anthropic/claude-opus-4-5` | `""` | `orMessages` | `anthropic-messages` |
| auto / unknown | `meta/llama-4-maverick` | `""` | `orChatCompletions` | `openai-chat` |

**`provider/openrouter/api_responses_test.go`**:
- `TestOrRespBuildRequest_Basic` — model, `stream: true`, `input` array shape.
- `TestOrRespBuildRequest_NoPromptCacheRetention` — field absent.
- `TestOrRespBuildRequest_Tools` — tools + tool choice.
- `TestOrRespBuildRequest_Reasoning` — `reasoning.effort` present when `Effort` set.

**`provider/openrouter/api_messages_test.go`**:
- `TestStreamMessages_StripAnthropicPrefix` — `anthropic/claude-opus-4-5` →
  model in JSON body is `claude-opus-4-5`.

**`provider/anthropic/stream_test.go`** (extend):
- `TestParseOpts_ProviderName_InErrors` — use a synthetic SSE fixture with an
  `error` event; verify the error event carries the custom `ProviderName`, not
  `"anthropic"`.
- `TestParseOpts_ApiType_InStreamStarted` — verify `StreamStartedEvent.ApiType`
  matches the value in `ParseOpts`.

**`provider/openai/api_responses_test.go`** (extend):
- `TestRespStreamMeta_ApiType` — verify `ApiType` flows through to
  `StreamStartedEvent` in a synthetic SSE fixture.

**`cmd/llmcli/cmds/infer_test.go`** (extend):
- `TestInferCmd_ApiFlag` — `--api openai-responses` sets `inferOpts.ApiType`;
  `--api bad` returns an error from `TextVar`.

### Integration tests (manual)

```bash
# Auto → Anthropic Messages for anthropic/* model
go run ./cmd/llmcli infer -v -m openrouter/anthropic/claude-opus-4-5 "Hi"
# ── stream started ── shows: api: anthropic-messages

# Auto → Responses for gpt-5.4
go run ./cmd/llmcli infer -v -m openrouter/openai/gpt-5.4 "Hi"
# ── stream started ── shows: api: openai-responses

# Auto → Chat Completions for legacy model
go run ./cmd/llmcli infer -v -m openrouter/openai/gpt-4o "Hi"
# ── stream started ── shows: api: openai-chat

# Explicit override: Responses API for a non-required model
go run ./cmd/llmcli infer -v -m openrouter/openai/gpt-4o --api openai-responses "Hi"
# ── stream started ── shows: api: openai-responses

# Explicit override: Chat Completions for an Anthropic model (compare outputs)
go run ./cmd/llmcli infer -v -m openrouter/anthropic/claude-opus-4-5 --api openai-chat "Hi"
# ── stream started ── shows: api: openai-chat
```

---

## Open Questions

1. **Does OpenRouter's `/v1/messages` accept `anthropic/claude-*` model IDs or
   bare `claude-*`?** We strip the prefix regardless (safe default). Verify
   during integration testing; revert the strip if OpenRouter needs the prefix.

2. **Does OpenRouter's `/v1/messages` endpoint require the `Anthropic-Version`
   header?** The design sends it defensively (same as the direct provider).
   If OpenRouter rejects it or ignores it, simply drop the header.

3. **Does OpenRouter surface `phase` in its Responses API stream?** Out of scope
   now; would motivate a follow-up design to model `phase` in `msg.Part`.

4. **Should thinking work through OpenRouter's Messages endpoint?** The
   `Anthropic-Beta: interleaved-thinking-2025-05-14` header is set, matching
   what the direct anthropic provider sends. OpenRouter likely proxies this
   through. Validate during integration.

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Auto-dispatch to Anthropic Messages breaks existing anthropic/* users | Semantically equivalent output; `StreamStartedEvent.ApiType` makes the change visible; explicit `--api openai-chat` override available |
| OpenRouter `/v1/responses` SSE format differs from OpenAI's | Caught in integration tests; fall back to `--api openai-chat` |
| `orRespBuildRequest` drifts from `openai.respBuildRequest` | `// TODO` cross-reference comment in both files |
| `anthropic.ParseOpts.ApiType` zero-value `""` instead of a concrete constant | Compile-time: adding `ApiType` without a default means the field defaults to `""`. Every call site that creates `ParseOpts` must be updated. CI build + grep for `ParseOpts{` confirms completeness. |
| `provider/openrouter` now depends on `provider/openai` and `provider/anthropic` | No import cycle. Binary size increases for users who import openrouter. Acceptable trade-off; avoids code duplication. |

---

## Acceptance Criteria

- [ ] `ApiType` type with four constants, `Valid()`, `UnmarshalText()`, field on `Request`, validation
- [ ] `RequestBuilder.ApiType()` and `WithApiType()` work
- [ ] `StreamStartedEvent.ApiType` is set by every provider; never `""` after dispatch
- [ ] `llmcli infer --api openai-responses` routes OpenRouter to `/v1/responses`
- [ ] `llmcli infer --api anthropic-messages` routes OpenRouter to `/v1/messages`
- [ ] `llmcli infer` (auto) dispatches `anthropic/*` → Messages API
- [ ] `llmcli infer` (auto) dispatches `openai/gpt-5.4` → Responses API
- [ ] `llmcli infer` (auto) keeps `openai/gpt-4o` on Chat Completions
- [ ] `llmcli -v infer` shows `api: <type>` in `── stream started ──`
- [ ] All events on all three OpenRouter paths carry `provider = "openrouter"`
- [ ] Anthropic provider error events carry the correct `ProviderName` (bug fix)
- [ ] All existing tests pass; `go build ./...` and `go vet ./...` pass
