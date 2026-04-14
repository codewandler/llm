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
model-native features. Recording which protocol was chosen in `RequestEvent`
and which upstream provider served the request in `StreamStartedEvent` gives
consumers (CLI, evaluation harnesses, routers) full observability.

A general `ApiTypeHint` field on `llm.Request` lets callers express a
preference without coupling to provider internals. Providers are free to
honour or ignore the hint.

---

## Scope

**In scope:**

- New `ApiType` type and `ApiTypeHint ApiType` field on `llm.Request` (with
  validation + `UnmarshalText` for cobra)
- `ResolvedApiType ApiType` field on `RequestEvent` — set before the HTTP call
  so consumers know which API was chosen even before streaming begins
- `Provider string` field on `StreamStartedEvent` — the upstream provider that
  served the request; for routing providers (OpenRouter) this is extracted from
  the response and differs from the routing provider name
- `--api` flag on `llmcli infer`
- `ApiTypeHint()` fluent setter on `RequestBuilder` + `WithApiTypeHint` functional option
- OpenRouter: dispatch to Responses API, Anthropic Messages API, or Chat
  Completions based on `ApiTypeHint`; smart auto-dispatch in auto mode
- New `provider/openrouter/api_responses.go`
- New `provider/openrouter/api_messages.go`
- **Bug fix** (identified during this design): `parseStream` and `onError` in
  `provider/anthropic` hardcode `llm.ProviderNameAnthropic` in error
  constructors instead of using `opts.ProviderName` — already being fixed by
  AUTO-4/AUTO-5 sub-agents; this design depends on those fixes

**Out of scope (future work):**

- OpenAI provider respecting `ApiTypeHint` to force Chat Completions downgrade
- Bedrock or Anthropic direct providers reading `ApiTypeHint` from `Request`
  (they have a fixed protocol; they ignore the hint on input but always set
  `ResolvedApiType` correctly on output)
- `phase` field round-trip in `llm.Message` / `msg.Part`

---

## Current State

| Layer | Status |
|---|---|
| OpenRouter provider | Chat Completions only |
| OpenRouter `/v1/responses` | Supported by OpenRouter; unused |
| OpenRouter `/v1/messages` | Supported by OpenRouter; unused |
| `llm.Request` | No API hint field |
| `RequestEvent` | No `ResolvedApiType` field |
| `StreamStartedEvent` | No `Provider` field |
| `llmcli infer` | No `--api` flag |
| OpenAI provider | Auto-dispatches Chat Completions ↔ Responses; no caller hint |
| `anthropic.ParseStreamWith` | ✅ already exported; used by MiniMax |
| `anthropic.BuildRequestBytes` | ✅ already exported |
| `anthropic.ParseOpts.ProviderName` | ✅ exists; used by MiniMax |
| `anthropic.parseStream` error constructors | ❌ hardcode `ProviderNameAnthropic` (being fixed by AUTO-4/5) |

---

## Design

### 1. `ApiType` type — `request.go`

```go
// ApiType identifies a wire protocol for LLM API requests.
// Used both as a hint on llm.Request and as a resolved value on RequestEvent.
type ApiType string

const (
    // ApiTypeAuto is the zero value — the provider selects the best API.
    ApiTypeAuto ApiType = ""
    // ApiTypeOpenAIChatCompletion is the OpenAI Chat Completions API
    // (/v1/chat/completions).
    ApiTypeOpenAIChatCompletion ApiType = "openai-chat"
    // ApiTypeOpenAIResponses is the OpenAI Responses API (/v1/responses).
    // Required for models that use the phase field (gpt-5.3-codex, gpt-5.4-*).
    ApiTypeOpenAIResponses ApiType = "openai-responses"
    // ApiTypeAnthropicMessages is the Anthropic Messages API (/v1/messages).
    // Provides native cache_control, thinking blocks, and anthropic-beta headers.
    ApiTypeAnthropicMessages ApiType = "anthropic-messages"
)

// Valid returns true if t is a known value or the zero value.
func (t ApiType) Valid() bool {
    switch t {
    case ApiTypeAuto, ApiTypeOpenAIChatCompletion, ApiTypeOpenAIResponses, ApiTypeAnthropicMessages:
        return true
    default:
        return false
    }
}

// UnmarshalText implements encoding.TextUnmarshaler for cobra TextVar binding.
func (t *ApiType) UnmarshalText(b []byte) error {
    v := ApiType(b)
    if !v.Valid() {
        return fmt.Errorf("invalid api type %q; must be one of: openai-chat, openai-responses, anthropic-messages, auto", v)
    }
    *t = v
    return nil
}
```

### 2. `Request.ApiTypeHint` — `request.go`

Field name is `ApiTypeHint` — explicitly a *hint*, not a binding instruction.
Providers that support multiple backends treat it as a preference. Providers
with a fixed protocol ignore it entirely.

```go
// ApiTypeHint expresses a preferred wire protocol. Providers honour it when
// they support the requested API; otherwise they fall back to their default.
// The actual API used is always reported in RequestEvent.ResolvedApiType.
ApiTypeHint ApiType `json:"api_type_hint,omitempty"`
```

Add to `Request.Validate()`:

```go
if !o.ApiTypeHint.Valid() {
    return fmt.Errorf("invalid ApiTypeHint %q; valid values: openai-chat, openai-responses, anthropic-messages, auto", o.ApiTypeHint)
}
```

Codec in `request_codec.go` (follows `Effort`, `ThinkingMode` pattern):

```go
func (t ApiType) MarshalText() ([]byte, error)  { return []byte(t), nil }
func (t *ApiType) UnmarshalText(b []byte) error { /* as above */ }
```

### 3. `RequestEvent.ResolvedApiType` — `event.go`

The resolved API is known before the HTTP call (it determines the URL). Record
it on `RequestEvent` where it sits alongside the `ProviderRequest` URL that
already implies the choice:

```go
RequestEvent struct {
    OriginalRequest Request         `json:"original_request"`
    ProviderRequest ProviderRequest `json:"provider_request"`

    // ResolvedApiType is the wire protocol actually used for this request.
    // Always set to a concrete value (never ApiTypeAuto). Set by the provider
    // when it publishes RequestEvent, before the HTTP call is made.
    ResolvedApiType ApiType `json:"resolved_api_type,omitempty"`
}
```

Every provider sets `ResolvedApiType` when publishing `RequestEvent`:

| Provider | Value |
|---|---|
| `provider/anthropic` | `ApiTypeAnthropicMessages` (always) |
| `provider/anthropic/claude` | `ApiTypeAnthropicMessages` (always) |
| `provider/minimax` | `ApiTypeAnthropicMessages` (always, via `PublishRequestParams`) |
| `provider/openai` | `ApiTypeOpenAIResponses` or `ApiTypeOpenAIChatCompletion` (based on internal dispatch) |
| `provider/bedrock` | `ApiTypeAnthropicMessages` (always) |
| `provider/ollama` | `ApiTypeOpenAIChatCompletion` (always) |
| `provider/fake` | `ApiTypeOpenAIChatCompletion` (always) |
| `provider/openrouter` — Chat Completions | `ApiTypeOpenAIChatCompletion` |
| `provider/openrouter` — Responses | `ApiTypeOpenAIResponses` |
| `provider/openrouter` — Messages | `ApiTypeAnthropicMessages` |

**`anthropic.PublishRequestParams` hardcodes the value:**

```go
// PublishRequestParams emits a RequestEvent. Always sets ResolvedApiType to
// ApiTypeAnthropicMessages because this function is only ever called for
// providers that use the Anthropic Messages wire format.
func PublishRequestParams(pub llm.Publisher, opts ParseOpts) {
    pub.Publish(&llm.RequestEvent{
        OriginalRequest:  opts.LLMRequest,
        ProviderRequest:  opts.RequestParams,
        ResolvedApiType:  llm.ApiTypeAnthropicMessages,
    })
}
```

No `ApiType` field is needed on `ParseOpts` — the value is always known at
the call site and the parser is always the Anthropic Messages parser. The
user's point stands: *it would always be `anthropic-messages`*.

For providers that publish `RequestEvent` directly (rather than via
`PublishRequestParams`), each sets its own concrete constant at the call site.

### 4. `StreamStartedEvent.Provider` — `event.go`

```go
StreamStartedEvent struct {
    RequestID string `json:"request_id,omitempty"`

    // Model is the model identifier echoed back by the upstream API.
    Model string `json:"model,omitempty"`

    // Provider is the upstream provider that served the request.
    // For simple (non-routing) providers this equals the provider name.
    // For routing providers such as OpenRouter it is the upstream backend
    // extracted from the response (e.g. "anthropic", "openai", "meta-llama")
    // and will differ from the routing provider's own name.
    Provider string `json:"provider,omitempty"`

    // Extra holds provider-specific data such as rate-limit headers.
    Extra map[string]any `json:"extra,omitempty"`
}
```

#### How `Provider` is set

**Direct providers** (non-routing): `Provider = providerName` — the routing
and upstream providers are the same thing.

**OpenRouter** has three paths, each with a different source:

| Path | Source of `Provider` |
|---|---|
| Chat Completions | Extracted from `chunk.Model` prefix in the first SSE chunk.<br>`"anthropic/claude-opus-4-5"` → `"anthropic"`<br>`"openai/gpt-4o"` → `"openai"`<br>`"meta-llama/llama-4-maverick"` → `"meta-llama"`<br>No prefix (`"auto"`) → `"openrouter"` (fallback) |
| Responses API | Hardcoded `"openai"` — only ever used for `openai/*` models |
| Anthropic Messages API | Hardcoded `"anthropic"` — only ever used for `anthropic/*` models |

Helper in `provider/openrouter/openrouter.go`:

```go
// upstreamProviderFromModel extracts the provider prefix from an OpenRouter
// model ID. Returns providerName as fallback when no slash is present.
func upstreamProviderFromModel(model string) string {
    if i := strings.IndexByte(model, '/'); i > 0 {
        return model[:i]
    }
    return providerName // "openrouter" fallback
}
```

**Anthropic Messages parser** (`onMessageStart`): needs to set `Provider` from
context. The existing `ParseOpts.ProviderName` field is for error attribution
(always `"openrouter"` when called from OpenRouter). We add a separate
`UpstreamProvider` field to `ParseOpts` for the `StreamStartedEvent.Provider`:

```go
type ParseOpts struct {
    Model            string
    ProviderName     string  // used in errors and usage records
    UpstreamProvider string  // used in StreamStartedEvent.Provider; falls back to ProviderName when empty
    ResponseHeaders  map[string]string
    RequestParams    llm.ProviderRequest
    LLMRequest       llm.Request
}
```

In `onMessageStart`:

```go
provider := p.meta.UpstreamProvider
if provider == "" {
    provider = p.meta.ProviderName
}
p.pub.Started(llm.StreamStartedEvent{
    Model:     evt.Message.Model,
    RequestID: evt.Message.ID,
    Provider:  provider,
    Extra:     extra,
})
```

For all existing callers (anthropic direct, minimax, claude), `UpstreamProvider`
is left empty → falls back to `ProviderName` → correct. Only OpenRouter's
Messages path sets it:

```go
anthropic.ParseStreamWith(ctx, resp.Body, pub, anthropic.ParseOpts{
    Model:            opts.Model,
    ProviderName:     providerName,      // "openrouter" — for errors/usage
    UpstreamProvider: "anthropic",        // for StreamStartedEvent.Provider
})
```

Similarly, **`RespStreamMeta`** (OpenAI Responses parser) gains an
`UpstreamProvider` field for the same purpose:

```go
type RespStreamMeta struct {
    RequestedModel   string
    StartTime        time.Time
    ProviderName     string       // for errors / usage
    UpstreamProvider string       // for StreamStartedEvent.Provider; falls back to ProviderName
    Logger           *slog.Logger
}
```

OpenRouter's Responses path passes `UpstreamProvider: "openai"`.

---

### 5. `RequestBuilder` changes — `request_builder.go`

Follows existing `Effort`/`Thinking`/`OutputFormat` pattern:

```go
func (b *RequestBuilder) ApiTypeHint(t ApiType) *RequestBuilder {
    b.req.ApiTypeHint = t
    return b
}

func WithApiTypeHint(t ApiType) RequestOption {
    return func(r *Request) { r.ApiTypeHint = t }
}
```

---

### 6. `llmcli infer` changes — `cmd/llmcli/cmds/infer.go`

```go
// in inferOpts:
ApiTypeHint llm.ApiType

// flag:
f.TextVar(&opts.ApiTypeHint, "api", llm.ApiType(""),
    "API backend hint: openai-chat, openai-responses, anthropic-messages, auto")

// builder:
b = b.ApiTypeHint(opts.ApiTypeHint)
```

Verbose output — `RequestEvent` is already printed; `ResolvedApiType` will
appear via `mapFromStruct` if non-zero. Additionally, `StreamStartedEvent`
gains `Provider`:

```go
// in printStreamStartedEvent:
if ev.Provider != "" {
    fields = append(fields, kvField{"provider", ev.Provider})
}
```

---

### 7. OpenRouter dispatch — `provider/openrouter/openrouter.go`

#### `selectAPI`

```go
type orAPIBackend int

const (
    orChatCompletions orAPIBackend = iota
    orResponses
    orMessages
)

// selectAPI resolves the effective API backend and the ResolvedApiType to
// record on RequestEvent.
//
// Auto-dispatch rules (ApiTypeHint == ""):
//   - openai/* models requiring Responses API  →  Responses
//   - openai/* other                           →  Chat Completions
//   - anthropic/*                              →  Anthropic Messages
//   - everything else                          →  Chat Completions
func selectAPI(model string, hint llm.ApiType) (orAPIBackend, llm.ApiType) {
    switch hint {
    case llm.ApiTypeOpenAIResponses:
        return orResponses, llm.ApiTypeOpenAIResponses
    case llm.ApiTypeAnthropicMessages:
        return orMessages, llm.ApiTypeAnthropicMessages
    case llm.ApiTypeOpenAIChatCompletion:
        return orChatCompletions, llm.ApiTypeOpenAIChatCompletion
    }
    // Auto: infer from model prefix.
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
```

#### Refactored `CreateStream`

The HTTP request construction and `RequestEvent` publication move into each
sub-method (each path has a different URL and `ResolvedApiType`). `CreateStream`
handles what is common: request building, normalisation, API key resolution,
publisher creation, model resolution event, and token estimates.

```go
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
    opts, err := src.BuildRequest(ctx)
    // ...normalise, validate...
    apiKey, err := p.opts.ResolveAPIKey(ctx)
    // ...

    pub, ch := llm.NewEventPublisher()
    if opts.Model != requestedModel {
        pub.ModelResolved(providerName, requestedModel, opts.Model)
    }
    // ...token estimates...

    backend, resolvedApiType := selectAPI(opts.Model, opts.ApiTypeHint)
    switch backend {
    case orResponses:
        go p.streamResponses(ctx, opts, resolvedApiType, apiKey, pub)
    case orMessages:
        go p.streamMessages(ctx, opts, resolvedApiType, apiKey, pub)
    default:
        go p.streamCompletions(ctx, opts, resolvedApiType, apiKey, pub)
    }
    return ch, nil
}
```

The existing `CreateStream` body that builds the Chat Completions httpReq and
calls `go parseStream(...)` becomes `streamCompletions`.

---

### 8. OpenRouter Responses API — `provider/openrouter/api_responses.go`

**Endpoint**: `POST {baseURL}/v1/responses`  
**Wire format**: identical to OpenAI Responses API  
**Auth**: `Authorization: Bearer <key>`

#### Changes to `provider/openai/api_responses.go`

Export the stream metadata type and parser:

```go
// RespStreamMeta holds per-request metadata for the Responses API SSE parser.
type RespStreamMeta struct {
    RequestedModel   string
    StartTime        time.Time
    ProviderName     string       // used in errors and usage records
    UpstreamProvider string       // used in StreamStartedEvent.Provider; falls back to ProviderName
    Logger           *slog.Logger
}

// RespParseStream is the exported Responses API SSE parser.
func RespParseStream(ctx context.Context, body io.ReadCloser, pub llm.Publisher, meta RespStreamMeta)
```

Inside `RespParseStream`, `pub.Started()` sets:
```go
Provider: cmp.Or(meta.UpstreamProvider, meta.ProviderName),
```

No `ApiType` or `ResolvedApiType` threading through this parser — the
`RequestEvent` (with `ResolvedApiType`) is published by the calling code before
the HTTP call, not inside the parser.

#### `streamResponses`

```go
func (p *Provider) streamResponses(
    ctx context.Context, opts llm.Request,
    resolvedApiType llm.ApiType, apiKey string, pub llm.Publisher,
) {
    body, err := orRespBuildRequest(opts)
    // ...
    req, _ := http.NewRequestWithContext(ctx, "POST",
        p.opts.BaseURL+"/v1/responses", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+apiKey)

    pub.Publish(&llm.RequestEvent{
        OriginalRequest:  opts,
        ProviderRequest:  llm.ProviderRequestFromHTTP(req, body),
        ResolvedApiType:  resolvedApiType, // ApiTypeOpenAIResponses
    })

    resp, err := p.client.Do(req)
    // ...error handling...

    openai.RespParseStream(ctx, resp.Body, pub, openai.RespStreamMeta{
        RequestedModel:   opts.Model,
        ProviderName:     providerName,  // "openrouter"
        UpstreamProvider: "openai",
        Logger:           p.opts.Logger,
    })
}
```

`orRespBuildRequest`: copy of `openai.respBuildRequest` minus
`prompt_cache_retention`. Model ID sent as-is (`"openai/gpt-5.4"`). Annotated
with `// TODO: consolidate with openai.respBuildRequest`.

---

### 9. OpenRouter Anthropic Messages API — `provider/openrouter/api_messages.go`

**Endpoint**: `POST {baseURL}/v1/messages`  
**Wire format**: Anthropic Messages API  
**Auth**: `Authorization: Bearer <key>` (NOT `x-api-key`)

```go
func (p *Provider) streamMessages(
    ctx context.Context, opts llm.Request,
    resolvedApiType llm.ApiType, apiKey string, pub llm.Publisher,
) {
    opts.Model = strings.TrimPrefix(opts.Model, "anthropic/")

    body, err := anthropic.BuildRequestBytes(anthropic.RequestOptions{LLMRequest: opts})
    // ...
    req, _ := http.NewRequestWithContext(ctx, "POST",
        p.opts.BaseURL+"/v1/messages", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+apiKey)
    req.Header.Set("Anthropic-Version", anthropic.AnthropicVersion)
    req.Header.Set("Anthropic-Beta", anthropic.BetaInterleavedThinking)

    pub.Publish(&llm.RequestEvent{
        OriginalRequest:  opts,
        ProviderRequest:  llm.ProviderRequestFromHTTP(req, body),
        ResolvedApiType:  resolvedApiType, // ApiTypeAnthropicMessages
    })

    resp, err := p.client.Do(req)
    // ...error handling...

    anthropic.ParseStreamWith(ctx, resp.Body, pub, anthropic.ParseOpts{
        Model:            opts.Model,
        ProviderName:     providerName,  // "openrouter" — errors/usage
        UpstreamProvider: "anthropic",   // StreamStartedEvent.Provider
    })
}
```

`provider/openrouter` imports `provider/anthropic` — same as `provider/minimax`
already does. No import cycle.

---

### 10. OpenRouter Chat Completions — `streamCompletions`

The existing `CreateStream` body extracted into `streamCompletions`. Key
addition: `RequestEvent.ResolvedApiType` and `StreamStartedEvent.Provider`:

```go
func (p *Provider) streamCompletions(
    ctx context.Context, opts llm.Request,
    resolvedApiType llm.ApiType, apiKey string, pub llm.Publisher,
) {
    // ...build httpReq (existing code)...

    pub.Publish(&llm.RequestEvent{
        OriginalRequest:  opts,
        ProviderRequest:  llm.ProviderRequestFromHTTP(httpReq, body),
        ResolvedApiType:  resolvedApiType, // ApiTypeOpenAIChatCompletion
    })

    resp, err := p.client.Do(httpReq)
    // ...existing error handling...

    go parseStream(ctx, resp.Body, pub, opts.Model, p.opts.Logger)
}
```

In `parseStream`, the existing `pub.Started(...)` call gains `Provider`:

```go
pub.Started(llm.StreamStartedEvent{
    Model:     chunk.Model,
    RequestID: chunk.ID,
    Provider:  upstreamProviderFromModel(chunk.Model), // NEW
})
```

---

### 11. Auto-dispatch summary for OpenRouter

| `Request.ApiTypeHint` | Model prefix | API Used | `RequestEvent.ResolvedApiType` | `StreamStartedEvent.Provider` |
|---|---|---|---|---|
| `openai-responses` | any | Responses | `openai-responses` | `"openai"` |
| `anthropic-messages` | any | Messages | `anthropic-messages` | `"anthropic"` |
| `openai-chat` | any | Chat Completions | `openai-chat` | from model prefix |
| `""` (auto) | `openai/gpt-5.4*`, `openai/*-codex*` | Responses | `openai-responses` | `"openai"` |
| `""` (auto) | `openai/*` other | Chat Completions | `openai-chat` | `"openai"` |
| `""` (auto) | `anthropic/*` | **Messages** | `anthropic-messages` | `"anthropic"` |
| `""` (auto) | anything else | Chat Completions | `openai-chat` | from model prefix |

**Behaviour change**: existing OpenRouter users calling `anthropic/*` models
move from Chat Completions to Anthropic Messages API in auto mode. Output is
semantically equivalent or better. The change is visible via
`RequestEvent.ResolvedApiType`. Explicit `--api openai-chat` override is always
available.

---

## Files Changed

| File | Change |
|---|---|
| `request.go` | Add `ApiType` type + constants + `Valid()` + `UnmarshalText()`; add `ApiTypeHint ApiType` field on `Request`; add validation |
| `request_codec.go` | Add `ApiType.MarshalText()` |
| `request_builder.go` | Add `ApiTypeHint()` fluent method, `WithApiTypeHint()` functional option |
| `event.go` | Add `ResolvedApiType ApiType` to `RequestEvent`; add `Provider string` to `StreamStartedEvent` |
| `provider/anthropic/stream.go` | Add `UpstreamProvider string` to `ParseOpts`; update `PublishRequestParams` to hardcode `ResolvedApiType: llm.ApiTypeAnthropicMessages`; fix `parseStream` error constructors (already done by AUTO-5) |
| `provider/anthropic/stream_processor.go` | Use `UpstreamProvider \|\| ProviderName` for `StreamStartedEvent.Provider`; fix `onError` (already done by AUTO-4) |
| `provider/anthropic/anthropic.go` | Set `ResolvedApiType: llm.ApiTypeAnthropicMessages` in `RequestEvent` publish |
| `provider/anthropic/claude/provider.go` | Set `ResolvedApiType: llm.ApiTypeAnthropicMessages` in `RequestEvent` publish |
| `provider/minimax/minimax.go` | No changes needed — `PublishRequestParams` now sets `ResolvedApiType` automatically |
| `provider/openai/api_responses.go` | Export `RespStreamMeta` (add `UpstreamProvider`), export `RespParseStream`; set `Provider` in `pub.Started()` |
| `provider/openai/openai.go` | Set `ResolvedApiType` (`ApiTypeOpenAIResponses` / `ApiTypeOpenAIChatCompletion`) in `RequestEvent`; set `Provider: "openai"` in `pub.Started()` |
| `provider/bedrock/bedrock.go` | Set `ResolvedApiType: llm.ApiTypeAnthropicMessages` in `RequestEvent`; set `Provider: "bedrock"` in `pub.Started()` |
| `provider/ollama/ollama.go` | Set `ResolvedApiType: llm.ApiTypeOpenAIChatCompletion` in `RequestEvent`; set `Provider: "ollama"` in `pub.Started()` |
| `provider/fake/fake.go` | Set `ResolvedApiType: llm.ApiTypeOpenAIChatCompletion` in `RequestEvent`; set `Provider: "fake"` in `pub.Started()` |
| `provider/openrouter/openrouter.go` | Add `selectAPI`, `orUseResponsesAPI`, `upstreamProviderFromModel`; refactor `CreateStream` → dispatch + `streamCompletions`; add `Provider` to `pub.Started()` in `parseStream` |
| `provider/openrouter/api_responses.go` | New: `streamResponses`, `orRespBuildRequest` |
| `provider/openrouter/api_messages.go` | New: `streamMessages` |
| `cmd/llmcli/cmds/infer.go` | Add `ApiTypeHint` to `inferOpts`; `--api` flag; builder thread-through; verbose display of `provider` in `── stream started ──` |

**No changes to**: `event_delta.go`, `msg/`, `tool/`, `provider/auto/`, `provider/router/`.

---

## Testing Strategy

### Unit tests

**`request_test.go`**:
- `TestApiType_Valid` — all known values → true; unknown → false.
- `TestApiType_UnmarshalText` — valid values parse; unknown returns error.
- `TestRequest_Validate_ApiTypeHint` — valid hint passes; unknown fails.

**`provider/openrouter/openrouter_test.go`** — `selectAPI` table:

| Model | Hint | Backend | `ResolvedApiType` |
|---|---|---|---|
| any | `openai-responses` | `orResponses` | `openai-responses` |
| any | `anthropic-messages` | `orMessages` | `anthropic-messages` |
| any | `openai-chat` | `orChatCompletions` | `openai-chat` |
| `openai/gpt-5.3-codex` | `""` | `orResponses` | `openai-responses` |
| `openai/gpt-5.4` | `""` | `orResponses` | `openai-responses` |
| `openai/gpt-4o` | `""` | `orChatCompletions` | `openai-chat` |
| `anthropic/claude-opus-4-5` | `""` | `orMessages` | `anthropic-messages` |
| `meta/llama-4-maverick` | `""` | `orChatCompletions` | `openai-chat` |

**`provider/openrouter/openrouter_test.go`** — `upstreamProviderFromModel`:
- `"anthropic/claude-opus-4-5"` → `"anthropic"`
- `"openai/gpt-4o"` → `"openai"`
- `"meta-llama/llama-4-maverick"` → `"meta-llama"`
- `"auto"` (no slash) → `"openrouter"` (fallback)

**`provider/openrouter/api_responses_test.go`**:
- `TestOrRespBuildRequest_Basic`, `_NoPromptCacheRetention`, `_Tools`, `_Reasoning`

**`provider/openrouter/api_messages_test.go`**:
- `TestStreamMessages_StripAnthropicPrefix` — body model is `"claude-opus-4-5"` not `"anthropic/claude-opus-4-5"`

**`provider/anthropic/stream_processor_test.go`** (extend):
- `TestParseOpts_UpstreamProvider_InStreamStarted` — when `UpstreamProvider = "openrouter-upstream"`, `StreamStartedEvent.Provider` equals that value.
- `TestParseOpts_UpstreamProvider_FallbackToProviderName` — when `UpstreamProvider = ""`, `Provider` equals `ProviderName`.

**`provider/openai/api_responses_test.go`** (extend):
- `TestRespStreamMeta_UpstreamProvider` — `UpstreamProvider` flows to `StreamStartedEvent.Provider`.

**`cmd/llmcli/cmds/infer_test.go`** (extend):
- `TestInferCmd_ApiFlag` — `--api openai-responses` sets `inferOpts.ApiTypeHint`; `--api bad` errors.

### Integration tests (manual)

```bash
# Auto → Anthropic Messages; provider shows "anthropic"
go run ./cmd/llmcli infer -v -m openrouter/anthropic/claude-opus-4-5 "Hi"
# ── request params ── resolved_api_type: anthropic-messages
# ── stream started ── provider: anthropic

# Auto → Responses; provider shows "openai"
go run ./cmd/llmcli infer -v -m openrouter/openai/gpt-5.4 "Hi"
# ── request params ── resolved_api_type: openai-responses
# ── stream started ── provider: openai

# Auto → Chat Completions
go run ./cmd/llmcli infer -v -m openrouter/openai/gpt-4o "Hi"
# ── request params ── resolved_api_type: openai-chat

# Explicit override
go run ./cmd/llmcli infer -v -m openrouter/openai/gpt-4o --api openai-responses "Hi"
# ── request params ── api_type_hint: openai-responses | resolved_api_type: openai-responses

# Override to Chat Completions for comparison
go run ./cmd/llmcli infer -v -m openrouter/anthropic/claude-opus-4-5 --api openai-chat "Hi"
# ── request params ── resolved_api_type: openai-chat
# ── stream started ── provider: anthropic  (extracted from model prefix in response)
```

---

## Open Questions

1. **Does OpenRouter's `/v1/messages` accept `anthropic/claude-*` model IDs or
   bare `claude-*`?** We strip the prefix regardless. Verify during integration.

2. **Does OpenRouter's `/v1/messages` require `Anthropic-Version`?** Sent
   defensively. Drop if OpenRouter rejects it.

3. **Does OpenRouter surface `phase` in its Responses API stream?** Out of
   scope; motivates a follow-up design.

4. **Does thinking work through OpenRouter's Messages endpoint?** The
   `Anthropic-Beta` header is sent. OpenRouter likely proxies it through.
   Validate during integration.

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Auto-dispatch to Anthropic Messages breaks existing anthropic/* users | Semantically equivalent output; `ResolvedApiType` in `RequestEvent` makes the change visible; `--api openai-chat` override always available |
| OpenRouter `/v1/responses` SSE format differs from OpenAI's | Caught immediately in integration tests; fall back to `--api openai-chat` |
| `orRespBuildRequest` drifts from `openai.respBuildRequest` | Cross-reference `// TODO` comment in both files |
| `PublishRequestParams` hardcodes `ApiTypeAnthropicMessages` — wrong if ever reused for a non-Messages parser | Function name and doc comment make the invariant explicit; not a generic "publish request" utility |
| `provider/openrouter` imports `provider/openai` + `provider/anthropic` | No cycle. Same pattern as `provider/minimax` → `provider/anthropic` today. Binary size increase is acceptable. |

---

## Acceptance Criteria

- [ ] `ApiType` type with four constants, `Valid()`, `MarshalText()`, `UnmarshalText()`
- [ ] `Request.ApiTypeHint` field with validation in `Validate()`
- [ ] `RequestEvent.ResolvedApiType` always set to a concrete value (never `""`) by every provider
- [ ] `StreamStartedEvent.Provider` set by every provider
- [ ] `RequestBuilder.ApiTypeHint()` and `WithApiTypeHint()` work
- [ ] `llmcli infer --api openai-responses` routes OpenRouter to `/v1/responses`
- [ ] `llmcli infer --api anthropic-messages` routes OpenRouter to `/v1/messages`
- [ ] `llmcli infer` (auto) dispatches `anthropic/*` → Messages; `openai/gpt-5.4` → Responses; `openai/gpt-4o` → Chat Completions
- [ ] `llmcli -v infer` shows `resolved_api_type` in `── request params ──` and `provider` in `── stream started ──`
- [ ] For all three OpenRouter paths, events carry `provider = "openrouter"` on errors/usage and `Provider = <upstream>` on `StreamStartedEvent`
- [ ] `go build ./...` and `go vet ./...` pass; all existing tests pass
