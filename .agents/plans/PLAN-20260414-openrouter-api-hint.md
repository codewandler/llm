# Plan: ApiType — Multi-Backend Dispatch for OpenRouter

**Reference**: `.agents/plans/DESIGN-20260414-openrouter-api-hint.md`  
**Date**: 2026-07-14  
**Estimated total**: ~55 minutes

---

## Dependency order

```
T1 (ApiType type + codec)
 ├─► T2 (Request.ApiTypeHint field + validation)
 │    └─► T5 (RequestBuilder) ──► T13 (llmcli --api flag)
 ├─► T3 (event fields: RequestEvent.ResolvedApiType + StreamStartedEvent.Provider)
 │    ├─► T4 (ParseOpts.UpstreamProvider + onMessageStart + PublishRequestParams)
 │    │    └─► T6 (anthropic.CreateStream sets ResolvedApiType)
 │    ├─► T7 (RespStreamMeta export + pub.Started Provider in openai)
 │    └─► T8 (pub.Started Provider in completions / bedrock / ollama / fake)
 └─► T9 (openrouter helpers: selectAPI, orUseResponsesAPI, upstreamProviderFromModel)
      └─► T10 (openrouter: refactor CreateStream → dispatch + streamCompletions)
           ├─► T11 (api_responses.go — also requires T7)
           └─► T12 (api_messages.go — also requires T4)

Tests: T14 after T1+T2, T15 after T9, T16 after T11, T17 after T4
```

---

## Task 1: Add `ApiType` type, constants, and codec

**Files modified**: `request.go`, `request_codec.go`  
**Estimated time**: 3 minutes

**`request.go`** — insert after the `OutputFormat` type block (find `type OutputFormat string`):

```go
// ApiType identifies a wire protocol for LLM API requests.
// Used as a hint on Request.ApiTypeHint and as the resolved value on RequestEvent.ResolvedApiType.
type ApiType string

const (
	// ApiTypeAuto is the zero value. The provider selects the best API.
	ApiTypeAuto ApiType = ""
	// ApiTypeOpenAIChatCompletion selects the OpenAI Chat Completions API (/v1/chat/completions).
	ApiTypeOpenAIChatCompletion ApiType = "openai-chat"
	// ApiTypeOpenAIResponses selects the OpenAI Responses API (/v1/responses).
	// Required for models that use the phase field (gpt-5.3-codex, gpt-5.4-*).
	ApiTypeOpenAIResponses ApiType = "openai-responses"
	// ApiTypeAnthropicMessages selects the Anthropic Messages API (/v1/messages).
	// Provides native cache_control, thinking blocks, and anthropic-beta headers.
	ApiTypeAnthropicMessages ApiType = "anthropic-messages"
)

// Valid returns true if t is a known constant or the zero value (auto).
func (t ApiType) Valid() bool {
	switch t {
	case ApiTypeAuto, ApiTypeOpenAIChatCompletion, ApiTypeOpenAIResponses, ApiTypeAnthropicMessages:
		return true
	default:
		return false
	}
}
```

**`request_codec.go`** — append after the `OutputFormat` codec block:

```go
// --- ApiType codec ---

// MarshalText maps the zero value (ApiTypeAuto = "") to the user-visible string "auto",
// matching the ThinkingMode convention.
func (t ApiType) MarshalText() ([]byte, error) {
	if t == ApiTypeAuto {
		return []byte("auto"), nil
	}
	return []byte(t), nil
}

// UnmarshalText maps "auto" → ApiTypeAuto (the zero value "").
// An empty input is also accepted as ApiTypeAuto.
func (t *ApiType) UnmarshalText(b []byte) error {
	s := string(b)
	if s == "auto" || s == "" {
		*t = ApiTypeAuto
		return nil
	}
	v := ApiType(s)
	if !v.Valid() {
		return fmt.Errorf("invalid api type %q; must be one of: auto, openai-chat, openai-responses, anthropic-messages", s)
	}
	*t = v
	return nil
}
```

**Verification**:
```
go build ./...
go vet ./...
```

---

## Task 2: Add `ApiTypeHint` field to `Request`

**Files modified**: `request.go`  
**Estimated time**: 2 minutes

In the `Request` struct, add after the `CacheHint` field:

```go
// ApiTypeHint expresses a preferred wire protocol. Providers honour it when
// they support the requested API; otherwise they fall back to their default.
// The actual API used is always reported in RequestEvent.ResolvedApiType.
ApiTypeHint ApiType `json:"api_type_hint,omitempty"`
```

In `Request.Validate()`, add after the `Thinking` validation block (find `if !o.Thinking.Valid()`):

```go
if !o.ApiTypeHint.Valid() {
	return fmt.Errorf("invalid ApiTypeHint %q; valid values: auto, openai-chat, openai-responses, anthropic-messages", o.ApiTypeHint)
}
```

**Verification**:
```
go build ./...
go test ./...
```

---

## Task 3: Add `ResolvedApiType` to `RequestEvent` and `Provider` to `StreamStartedEvent`

**Files modified**: `event.go`  
**Estimated time**: 2 minutes

**`StreamStartedEvent`** — add `Provider` between `Model` and `Extra`:

```go
// Before:
StreamStartedEvent struct {
    RequestID string         `json:"request_id,omitempty"`
    Model     string         `json:"model,omitempty"`
    Extra     map[string]any `json:"extra,omitempty"`
}

// After:
StreamStartedEvent struct {
    RequestID string `json:"request_id,omitempty"`
    Model     string `json:"model,omitempty"`

    // Provider is the upstream provider that served the request.
    // For direct providers this equals the provider name.
    // For routing providers such as OpenRouter it is the actual backend
    // extracted from the response (e.g. "anthropic", "openai", "meta-llama").
    Provider string `json:"provider,omitempty"`

    Extra map[string]any `json:"extra,omitempty"`
}
```

**`RequestEvent`** — add `ResolvedApiType` after `ProviderRequest`:

```go
// Before:
RequestEvent struct {
    OriginalRequest Request         `json:"original_request"`
    ProviderRequest ProviderRequest `json:"provider_request"`
}

// After:
RequestEvent struct {
    OriginalRequest Request         `json:"original_request"`
    ProviderRequest ProviderRequest `json:"provider_request"`

    // ResolvedApiType is the wire protocol actually used for this request.
    // Always a concrete value (never ApiTypeAuto). Set by the provider before
    // the HTTP call is made.
    ResolvedApiType ApiType `json:"resolved_api_type,omitempty"`
}
```

**Verification**:
```
go build ./...
go test ./...
```

---

## Task 4: Update `ParseOpts`, `PublishRequestParams`, and `onMessageStart`

**Files modified**: `provider/anthropic/stream.go`, `provider/anthropic/stream_processor.go`  
**Estimated time**: 4 minutes

**`stream.go`** — add `UpstreamProvider` field to `ParseOpts` after `ProviderName`:

```go
// Before:
type ParseOpts struct {
    Model        string
    ProviderName string
    ResponseHeaders map[string]string
    // ...
}

// After:
type ParseOpts struct {
    Model        string
    ProviderName string

    // UpstreamProvider is used in StreamStartedEvent.Provider.
    // When empty, falls back to ProviderName.
    // Set for routing providers where billing provider ≠ upstream backend
    // (e.g. OpenRouter billing = "openrouter", upstream = "anthropic").
    UpstreamProvider string

    ResponseHeaders map[string]string
    // ...
}
```

**`stream.go`** — update `PublishRequestParams` to set `ResolvedApiType`. Replace the entire function body:

```go
// PublishRequestParams emits a RequestEvent. Always sets ResolvedApiType to
// ApiTypeAnthropicMessages: this function is only called by providers using
// the Anthropic Messages wire format (anthropic direct, claude, minimax).
func PublishRequestParams(pub llm.Publisher, opts ParseOpts) {
	pub.Publish(&llm.RequestEvent{
		OriginalRequest: opts.LLMRequest,
		ProviderRequest: opts.RequestParams,
		ResolvedApiType: llm.ApiTypeAnthropicMessages,
	})
}
```

**`stream_processor.go`** — surgical change to `onMessageStart`. Replace only the `p.pub.Started(...)` call (lines 143–147) plus insert 3 lines before it:

```go
// Before (lines 143–147):
	p.pub.Started(llm.StreamStartedEvent{
		Model:     evt.Message.Model,
		RequestID: evt.Message.ID,
		Extra:     extra,
	})

// After:
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

All other lines in `onMessageStart` (token accounting, `p.requestID`, `extra` map, `ModelResolved` event) are left untouched.

**Verification**:
```
go build ./...
go test ./provider/anthropic/...
go test ./provider/minimax/...
```

---

## Task 5: Add `ApiTypeHint` builder method and functional option

**Files modified**: `request_builder.go`  
**Estimated time**: 2 minutes

Add after the `OutputFormat` method (find `func (b *RequestBuilder) OutputFormat`):

```go
// ApiTypeHint sets the preferred wire protocol. The provider honours it when
// supported; falls back to its default otherwise.
func (b *RequestBuilder) ApiTypeHint(t ApiType) *RequestBuilder {
	b.req.ApiTypeHint = t
	return b
}

// WithApiTypeHint sets Request.ApiTypeHint.
func WithApiTypeHint(t ApiType) RequestOption {
	return func(r *Request) { r.ApiTypeHint = t }
}
```

**Verification**:
```
go build ./...
go test ./...
```

---

## Task 6: Set `ResolvedApiType` in anthropic direct `CreateStream`

**Files modified**: `provider/anthropic/anthropic.go`  
**Estimated time**: 2 minutes

Find the `pub.Publish(&llm.RequestEvent{...})` call in `CreateStream`. Add `ResolvedApiType`:

```go
// Before:
pub.Publish(&llm.RequestEvent{
    OriginalRequest: opts,
    ProviderRequest: llm.ProviderRequestFromHTTP(req, body),
})

// After:
pub.Publish(&llm.RequestEvent{
    OriginalRequest: opts,
    ProviderRequest: llm.ProviderRequestFromHTTP(req, body),
    ResolvedApiType: llm.ApiTypeAnthropicMessages,
})
```

Note: `provider/anthropic/claude/provider.go` calls `anthropic.PublishRequestParams` — covered by Task 4. `provider/minimax/minimax.go` also calls `anthropic.PublishRequestParams` — covered by Task 4. No other changes needed.

**Verification**:
```
go build ./...
go test ./provider/anthropic/...
```

---

## Task 7: Export `RespStreamMeta`/`RespParseStream`; add `UpstreamProvider`; set `Provider` in all `pub.Started()` calls

**Files modified**: `provider/openai/api_responses.go`  
**Estimated time**: 7 minutes

Three changes in one file.

### a) Replace `respStreamMeta` alias with exported `RespStreamMeta`

Delete line 277 (`type respStreamMeta = ccStreamMeta`) and replace with:

```go
// RespStreamMeta holds per-request metadata for the Responses API SSE parser.
// Exported so provider/openrouter can reuse RespParseStream.
//
// Caller-set fields (exported) are provided when calling RespParseStream.
// Parser-internal fields (unexported) are mutated during parsing and must not be set by callers.
type RespStreamMeta struct {
	// Caller-set:
	RequestedModel   string       // model ID sent in the request body
	StartTime        time.Time    // recorded at request start for latency tracking
	ProviderName     string       // used in errors and usage records; defaults to llm.ProviderNameOpenAI
	UpstreamProvider string       // used in StreamStartedEvent.Provider; falls back to ProviderName
	Logger           *slog.Logger

	// Parser-internal state (set by respHandleEvent, not by callers):
	responseID    string
	responseModel string
}

func (m *RespStreamMeta) provider() string {
	if m.ProviderName != "" {
		return m.ProviderName
	}
	return llm.ProviderNameOpenAI
}

func (m *RespStreamMeta) upstreamProvider() string {
	if m.UpstreamProvider != "" {
		return m.UpstreamProvider
	}
	return m.provider()
}
```

Update the one call site in `streamResponses` (in the same file) that constructs the meta:

```go
// Before:
go respParseStream(ctx, resp.Body, pub, respStreamMeta{
    requestedModel: opts.Model,
    startTime:      startTime,
    providerName:   p.Name(),
    logger:         p.opts.Logger,
})

// After:
go RespParseStream(ctx, resp.Body, pub, RespStreamMeta{
    RequestedModel: opts.Model,
    StartTime:      startTime,
    ProviderName:   p.Name(),
    Logger:         p.opts.Logger,
})
```

### b) Export `respParseStream` → `RespParseStream`

Rename the function declaration:
```go
// Before:
func respParseStream(ctx context.Context, body io.ReadCloser, pub llm.Publisher, meta respStreamMeta) {

// After:
// RespParseStream reads a Responses API SSE body and publishes events.
// Exported so provider/openrouter can reuse this parser for its /v1/responses path.
func RespParseStream(ctx context.Context, body io.ReadCloser, pub llm.Publisher, meta RespStreamMeta) {
```

Update `respHandleEvent`'s last parameter type (line 382):
```go
// Before:
	meta *respStreamMeta,

// After:
	meta *RespStreamMeta,
```

The call site `respHandleEvent(..., &meta)` at line 364 does not change — `meta` is already `RespStreamMeta` in the renamed `RespParseStream` function.

Fix two hardcoded error names in `RespParseStream` (the error paths at the end):
```go
// Before:
pub.Error(llm.NewErrContextCancelled(llm.ProviderNameOpenAI, err))
// ...
pub.Error(llm.NewErrStreamRead(llm.ProviderNameOpenAI, err))

// After:
pub.Error(llm.NewErrContextCancelled(meta.provider(), err))
// ...
pub.Error(llm.NewErrStreamRead(meta.provider(), err))
```

### c) Update renamed field references inside `respHandleEvent`

`respHandleEvent` accesses the old unexported fields by name. All references must be updated:

| Old field reference | New field reference |
|---|---|
| `meta.requestedModel` | `meta.RequestedModel` |
| `meta.logger` | `meta.Logger` |
| `meta.providerName` | *(accessed only via `meta.provider()` — method unchanged)* |

Note: `meta.startTime` does **not** appear inside `respHandleEvent`. It is only in the struct literal at line 80 (updated in Task 7a). No grep-and-replace needed for it.

Grep for the above in `api_responses.go` and update every occurrence. Known locations:
- Line ~514: `buildUsageTokenItems(..., meta.Logger, meta.provider(), meta.RequestedModel)`
- Line ~516: `Dims: usage.Dims{Provider: meta.provider(), Model: meta.RequestedModel, RequestID: meta.responseID}`
- Line ~520: `usage.Default().Calculate(meta.provider(), meta.RequestedModel, items)`

### d) Add `Provider` to all four `pub.Started()` calls inside `respHandleEvent`

There are exactly four `pub.Started(llm.StreamStartedEvent{...})` calls. Add `Provider: meta.upstreamProvider()` to each:

```go
// Before (repeat for all 4 occurrences):
pub.Started(llm.StreamStartedEvent{
    Model:     meta.responseModel,
    RequestID: meta.responseID,
})

// After:
pub.Started(llm.StreamStartedEvent{
    Model:     meta.responseModel,
    RequestID: meta.responseID,
    Provider:  meta.upstreamProvider(),
})
```

**Verification**:
```
go build ./provider/openai/...
go test ./provider/openai/...
```

---

## Task 8: Set `Provider` in remaining `pub.Started()` call sites; fix `ccParseStream` error names

**Files modified**: `provider/openai/api_completions.go`, `provider/bedrock/bedrock.go`, `provider/ollama/ollama.go`, `provider/fake/fake.go`  
**Estimated time**: 4 minutes

**`api_completions.go`** — two changes:

*1. Add `Provider` to `pub.Started()`:*
```go
// Before (line ~351):
pub.Started(llm.StreamStartedEvent{
    Model:     chunk.Model,
    RequestID: chunk.ID,
})

// After:
pub.Started(llm.StreamStartedEvent{
    Model:     chunk.Model,
    RequestID: chunk.ID,
    Provider:  meta.provider(),
})
```

*2. Fix hardcoded error names (same pattern as Task 7):*
```go
// Before (lines ~405/408):
pub.Error(llm.NewErrContextCancelled(llm.ProviderNameOpenAI, err))
pub.Error(llm.NewErrStreamRead(llm.ProviderNameOpenAI, err))

// After:
pub.Error(llm.NewErrContextCancelled(meta.provider(), err))
pub.Error(llm.NewErrStreamRead(meta.provider(), err))
```

**`bedrock.go`** — line 735:
```go
// Before:
pub.Started(llm.StreamStartedEvent{Model: meta.ResolvedModel})

// After:
pub.Started(llm.StreamStartedEvent{Model: meta.ResolvedModel, Provider: "bedrock"})
```

**`ollama.go`** — line 492:
```go
// Before:
pub.Started(llm.StreamStartedEvent{})

// After:
pub.Started(llm.StreamStartedEvent{Provider: "ollama"})
```

**`fake.go`** — lines 69–72:
```go
// Before:
pub.Started(llm.StreamStartedEvent{
    Model:     "fake-model-v1",
    RequestID: "fake-req-123",
})

// After:
pub.Started(llm.StreamStartedEvent{
    Model:     "fake-model-v1",
    RequestID: "fake-req-123",
    Provider:  "fake",
})
```

**Verification**:
```
go build ./...
go test ./provider/openai/...
go test ./provider/fake/...
```

---

## Task 9: Add openrouter dispatch helpers

**Files modified**: `provider/openrouter/openrouter.go`  
**Estimated time**: 3 minutes

Add before the `buildRequest` function (find `// --- Request building ---`). `openrouter.go` already imports `"strings"` — no new import needed.

```go
// --- API dispatch ---

type orAPIBackend int

const (
	orChatCompletions orAPIBackend = iota
	orResponses
	orMessages
)

// selectAPI resolves the effective API backend and the concrete ApiType to
// record on RequestEvent.ResolvedApiType.
//
// Auto-dispatch rules (ApiTypeHint == ""):
//   - openai/* models requiring Responses API  →  orResponses
//   - openai/* other                           →  orChatCompletions
//   - anthropic/*                              →  orMessages
//   - everything else                          →  orChatCompletions
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

// orUseResponsesAPI reports whether the bare model ID (without "openai/" prefix)
// requires the Responses API on OpenRouter.
// Keep in sync with provider/openai.useResponsesAPI — see that function for the canonical list.
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

// upstreamProviderFromModel extracts the provider prefix from an OpenRouter
// model ID (e.g. "anthropic/claude-opus-4-5" → "anthropic").
// Returns providerName ("openrouter") as fallback when the ID has no slash.
func upstreamProviderFromModel(model string) string {
	if i := strings.IndexByte(model, '/'); i > 0 {
		return model[:i]
	}
	return providerName
}
```

**Verification**:
```
go build ./provider/openrouter/...
```

---

## Task 10: Refactor openrouter `CreateStream` → dispatch + `streamCompletions`

**Files modified**: `provider/openrouter/openrouter.go`  
**Estimated time**: 6 minutes

**Behavior note**: Currently retriable HTTP errors from the Chat Completions path return `(nil, error)` from `CreateStream`. After this refactor all three paths are goroutines and all HTTP-level failures become stream error events. The `llm.IsRetriableHTTPStatus` check is preserved — retriable errors still result in a silent `pub.Close()` (no error event), matching existing intent.

**Event ordering change**: Currently the event order is `ModelResolved → RequestEvent → TokenEstimates`. After this refactor it becomes `ModelResolved → TokenEstimates → RequestEvent` (token estimates stay in `CreateStream`; `RequestEvent` moves to each sub-method because it contains path-specific `ProviderRequest.URL` and `ResolvedApiType`). This is a minor observable change in verbose mode only.

### New `CreateStream`

Replace everything from `body, err := buildRequest(opts)` (line 155) through `return ch, nil` (line 215) with the dispatch block. Keep lines 135–154 unchanged:

```go
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	opts, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOpenRouter, err)
	}

	requestedModel := opts.Model
	opts.Model = p.normalizeRequestModel(opts.Model)
	if err := opts.Validate(); err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOpenRouter, err)
	}

	apiKey, err := p.opts.ResolveAPIKey(ctx)
	if err != nil || apiKey == "" {
		return nil, llm.NewErrMissingAPIKey(llm.ProviderNameOpenRouter)
	}

	pub, ch := llm.NewEventPublisher()

	if opts.Model != requestedModel {
		pub.ModelResolved(providerName, requestedModel, opts.Model)
	}

	// Token estimates are path-independent; emit before dispatch.
	if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
		Model: opts.Model, Messages: opts.Messages, Tools: opts.Tools,
	}); err == nil {
		for _, rec := range tokencount.EstimateRecords(est, providerName, opts.Model, "heuristic", usage.Default()) {
			pub.TokenEstimate(rec)
		}
	}

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

### New `streamCompletions`

Extract the old Chat Completions HTTP body into this new method. The `pub` and `ch` are no longer created here. Key changes from the old code: (1) add `ResolvedApiType` to `RequestEvent`, (2) `Provider` in `pub.Started()` is computed inside `parseStream` from `chunk.Model`:

```go
func (p *Provider) streamCompletions(
	ctx context.Context, opts llm.Request,
	resolvedApiType llm.ApiType, apiKey string, pub llm.Publisher,
) {
	body, err := buildRequest(opts)
	if err != nil {
		pub.Error(llm.NewErrBuildRequest(providerName, err))
		pub.Close()
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		p.opts.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		pub.Error(llm.NewErrBuildRequest(providerName, err))
		pub.Close()
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	pub.Publish(&llm.RequestEvent{
		OriginalRequest: opts,
		ProviderRequest: llm.ProviderRequestFromHTTP(httpReq, body),
		ResolvedApiType: resolvedApiType,
	})

	resp, err := p.client.Do(httpReq)
	if err != nil {
		pub.Error(llm.NewErrRequestFailed(providerName, err))
		pub.Close()
		return
	}
	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		apiErr := llm.NewErrAPIError(providerName, resp.StatusCode, string(errBody))
		if llm.IsRetriableHTTPStatus(resp.StatusCode) {
			pub.Close()
			return
		}
		pub.Error(apiErr)
		pub.Close()
		return
	}

	go parseStream(ctx, resp.Body, pub, opts.Model, p.opts.Logger)
}
```

### Update `parseStream` to set `Provider`

Find the `pub.Started(...)` call inside `parseStream` (the existing function in the same file). Add `Provider`:

```go
// Before:
pub.Started(llm.StreamStartedEvent{
    Model:     chunk.Model,
    RequestID: chunk.ID,
})

// After:
pub.Started(llm.StreamStartedEvent{
    Model:     chunk.Model,
    RequestID: chunk.ID,
    Provider:  upstreamProviderFromModel(chunk.Model),
})
```

No signature change to `parseStream`. `upstreamProviderFromModel` is available in the same package (added in Task 9).

**Verification**:
```
go build ./provider/openrouter/...
go test ./provider/openrouter/...
```

---

## Task 11: New `provider/openrouter/api_responses.go`

**Files created**: `provider/openrouter/api_responses.go`  
**Estimated time**: 7 minutes

The file has two parts: `streamResponses` and `orRespBuildRequest`.

```go
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/sortmap"
)

func (p *Provider) streamResponses(
	ctx context.Context, opts llm.Request,
	resolvedApiType llm.ApiType, apiKey string, pub llm.Publisher,
) {
	startTime := time.Now()

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

	pub.Publish(&llm.RequestEvent{
		OriginalRequest: opts,
		ProviderRequest: llm.ProviderRequestFromHTTP(req, body),
		ResolvedApiType: resolvedApiType,
	})

	resp, err := p.client.Do(req)
	if err != nil {
		pub.Error(llm.NewErrRequestFailed(providerName, err))
		pub.Close()
		return
	}
	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		apiErr := llm.NewErrAPIError(providerName, resp.StatusCode, string(errBody))
		if llm.IsRetriableHTTPStatus(resp.StatusCode) {
			pub.Close()
			return
		}
		pub.Error(apiErr)
		pub.Close()
		return
	}

	// RespParseStream closes pub when done (via defer pub.Close() inside it).
	openai.RespParseStream(ctx, resp.Body, pub, openai.RespStreamMeta{
		RequestedModel:   opts.Model,   // "openai/gpt-5.4" — OpenRouter full form
		StartTime:        startTime,
		ProviderName:     providerName, // "openrouter" — for errors and usage
		UpstreamProvider: "openai",     // for StreamStartedEvent.Provider
		Logger:           p.opts.Logger,
	})
}

// --- Request building ---

// orRespRequest is the top-level JSON body for OpenRouter's /v1/responses endpoint.
// Identical to openai.respRequest except there is no prompt_cache_retention field:
// OpenRouter does not expose that knob.
// TODO: consolidate with openai.respBuildRequest when the openai package exports it.
type orRespRequest struct {
	Model           string              `json:"model"`
	Input           []orRespInput       `json:"input"`
	Instructions    string              `json:"instructions,omitempty"`
	Tools           []orRespTool        `json:"tools,omitempty"`
	ToolChoice      any                 `json:"tool_choice,omitempty"`
	Reasoning       *orRespReason       `json:"reasoning,omitempty"`
	MaxOutputTokens int                 `json:"max_output_tokens,omitempty"`
	Temperature     float64             `json:"temperature,omitempty"`
	TopP            float64             `json:"top_p,omitempty"`
	TopK            int                 `json:"top_k,omitempty"`
	ResponseFormat  *orRespFormat       `json:"response_format,omitempty"`
	Stream          bool                `json:"stream"`
}

type orRespFormat struct {
	Type string `json:"type"`
}

type orRespReason struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type orRespInput struct {
	// Message items (role-based)
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
	// function_call / function_call_output items
	Type      string `json:"type,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type orRespTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
}

// orRespBuildRequest builds the JSON body for OpenRouter's /v1/responses endpoint.
// Identical to openai.respBuildRequest except prompt_cache_retention is omitted and
// the model ID is sent as-is (OpenRouter uses the full "openai/gpt-5.4" form).
func orRespBuildRequest(opts llm.Request) ([]byte, error) {
	r := orRespRequest{
		Model:  opts.Model,
		Stream: true,
	}

	if opts.MaxTokens > 0 {
		r.MaxOutputTokens = opts.MaxTokens
	}
	if opts.Temperature > 0 {
		r.Temperature = opts.Temperature
	}
	if opts.TopP > 0 {
		r.TopP = opts.TopP
	}
	if opts.TopK > 0 {
		r.TopK = opts.TopK
	}
	if opts.OutputFormat == llm.OutputFormatJSON {
		r.ResponseFormat = &orRespFormat{Type: "json_object"}
	}

	// Convert messages. The first SystemMsg becomes top-level "instructions";
	// subsequent SystemMsg entries become "developer" role items.
	instructionsSet := false
	for _, m := range opts.Messages {
		switch m.Role {
		case msg.RoleSystem:
			if !instructionsSet {
				r.Instructions = m.Text()
				instructionsSet = true
			} else {
				r.Input = append(r.Input, orRespInput{Role: "developer", Content: m.Text()})
			}
		case msg.RoleUser:
			r.Input = append(r.Input, orRespInput{Role: "user", Content: m.Text()})
		case msg.RoleAssistant:
			if m.Text() != "" {
				r.Input = append(r.Input, orRespInput{Role: "assistant", Content: m.Text()})
			}
			for _, tc := range m.ToolCalls() {
				argsJSON, err := json.Marshal(tc.Args)
				if err != nil {
					return nil, fmt.Errorf("marshal tool call arguments: %w", err)
				}
				r.Input = append(r.Input, orRespInput{
					Type: "function_call", CallID: tc.ID,
					Name: tc.Name, Arguments: string(argsJSON),
				})
			}
		case msg.RoleTool:
			for _, tr := range m.ToolResults() {
				r.Input = append(r.Input, orRespInput{
					Type: "function_call_output", CallID: tr.ToolCallID, Output: tr.ToolOutput,
				})
			}
		}
	}

	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, orRespTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  sortmap.NewSortedMap(t.Parameters),
		})
	}

	if len(opts.Tools) > 0 {
		switch tc := opts.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			r.ToolChoice = "auto"
		case llm.ToolChoiceRequired:
			r.ToolChoice = "required"
		case llm.ToolChoiceNone:
			r.ToolChoice = "none"
		case llm.ToolChoiceTool:
			r.ToolChoice = map[string]any{"type": "function", "name": tc.Name}
		}
	}

	if !opts.Effort.IsEmpty() {
		r.Reasoning = &orRespReason{Effort: string(opts.Effort)}
	}

	return json.Marshal(r)
}
```

**Verification**:
```
go build ./provider/openrouter/...
go test ./provider/openrouter/...
```

---

## Task 12: New `provider/openrouter/api_messages.go`

**Files created**: `provider/openrouter/api_messages.go`  
**Estimated time**: 4 minutes

```go
package openrouter

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
)

func (p *Provider) streamMessages(
	ctx context.Context, opts llm.Request,
	resolvedApiType llm.ApiType, apiKey string, pub llm.Publisher,
) {
	// Strip "anthropic/" prefix: OpenRouter's /v1/messages expects bare model IDs
	// (e.g. "claude-opus-4-5", not "anthropic/claude-opus-4-5").
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
	req.Header.Set("Authorization", "Bearer "+apiKey) // OpenRouter uses Bearer, NOT x-api-key
	req.Header.Set("Anthropic-Version", anthropic.AnthropicVersion)
	req.Header.Set("Anthropic-Beta", anthropic.BetaInterleavedThinking)

	pub.Publish(&llm.RequestEvent{
		OriginalRequest: opts,
		ProviderRequest: llm.ProviderRequestFromHTTP(req, body),
		ResolvedApiType: resolvedApiType, // ApiTypeAnthropicMessages
	})

	resp, err := p.client.Do(req)
	if err != nil {
		pub.Error(llm.NewErrRequestFailed(providerName, err))
		pub.Close()
		return
	}
	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		apiErr := llm.NewErrAPIError(providerName, resp.StatusCode, string(errBody))
		if llm.IsRetriableHTTPStatus(resp.StatusCode) {
			pub.Close()
			return
		}
		pub.Error(apiErr)
		pub.Close()
		return
	}

	// ParseStreamWith spawns a goroutine internally and takes ownership of both
	// resp.Body and pub (closes pub when parsing completes).
	// ProviderName = "openrouter" labels all error events and usage records.
	// UpstreamProvider = "anthropic" sets StreamStartedEvent.Provider correctly.
	anthropic.ParseStreamWith(ctx, resp.Body, pub, anthropic.ParseOpts{
		Model:            opts.Model,
		ProviderName:     providerName,  // "openrouter"
		UpstreamProvider: "anthropic",
	})
}
```

**Verification**:
```
go build ./provider/openrouter/...
go test ./provider/openrouter/...
```

---

## Task 13: Add `--api` flag to `llmcli infer`

**Files modified**: `cmd/llmcli/cmds/infer.go`  
**Estimated time**: 3 minutes

Read the file to locate the exact struct, flag block, and builder chain. Make four targeted changes:

**1. Add to `inferOpts` struct** (alongside `Effort`, `Thinking`):
```go
ApiTypeHint llm.ApiType
```

**2. Add flag** (alongside other `TextVar` flags):
```go
f.TextVar(&opts.ApiTypeHint, "api", llm.ApiType(""),
	"API backend hint: auto, openai-chat, openai-responses, anthropic-messages")
```

**3. Thread to builder** (alongside `.Effort(...)`, `.Thinking(...)`):
```go
b = b.ApiTypeHint(opts.ApiTypeHint)
```

**4. Display `ResolvedApiType` and `Provider` in verbose output.**

`api_type_hint` appears in `── request params ──` automatically when set (it's part of `OriginalRequest`, marshalled by `mapFromStruct`). `resolved_api_type` is on `RequestEvent` itself and does NOT appear automatically. Add it via a surgical change to `printRequestParamsEvent` (lines 475–479):

```go
// Before (lines 475–479):
	if req := ev.OriginalRequest; req.Model != "" {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "%s── request params ──%s\n", ansiDim, ansiReset)
		printParamMap(mapFromStruct(req, "messages", "tools"))
	}

// After:
	if req := ev.OriginalRequest; req.Model != "" {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintf(os.Stderr, "%s── request params ──%s\n", ansiDim, ansiReset)
		params := mapFromStruct(req, "messages", "tools")
		if ev.ResolvedApiType != "" {
			if params == nil {
				params = make(map[string]any)
			}
			params["resolved_api_type"] = string(ev.ResolvedApiType)
		}
		printParamMap(params)
	}
```

In `printStreamStartedEvent` — add after the `RequestID` check:
```go
if ev.Provider != "" {
    fields = append(fields, kvField{"provider", ev.Provider})
}
```

**Verification**:
```
go build ./cmd/llmcli/...
go run ./cmd/llmcli infer --help 2>&1 | grep -A1 "\-\-api"
go test ./cmd/llmcli/...
```

---

## Task 14: Tests — `ApiType` codec and `Request.ApiTypeHint` validation

**Files modified**: `request_codec_test.go` (extend, same `package llm`)  
**Estimated time**: 3 minutes

```go
func TestApiType_TextRoundtrip(t *testing.T) {
	// This file is package llm (internal), so types are unqualified.
	tests := []struct {
		input   string
		want    ApiType
		wantStr string
	}{
		{"auto", ApiTypeAuto, "auto"},
		{"", ApiTypeAuto, "auto"},            // empty → auto
		{"openai-chat", ApiTypeOpenAIChatCompletion, "openai-chat"},
		{"openai-responses", ApiTypeOpenAIResponses, "openai-responses"},
		{"anthropic-messages", ApiTypeAnthropicMessages, "anthropic-messages"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var a ApiType
			require.NoError(t, a.UnmarshalText([]byte(tt.input)))
			assert.Equal(t, tt.want, a)

			b, err := a.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.wantStr, string(b))
		})
	}
}

func TestApiType_UnmarshalText_Invalid(t *testing.T) {
	var a ApiType
	err := a.UnmarshalText([]byte("bogus"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid api type")
}

func TestRequest_Validate_ApiTypeHint(t *testing.T) {
	// Valid hint: validation passes (any other errors are unrelated to ApiTypeHint)
	r := Request{Model: "m", ApiTypeHint: ApiTypeOpenAIResponses}
	err := r.Validate()
	if err != nil {
		assert.NotContains(t, err.Error(), "ApiTypeHint")
	}

	// Unknown hint: validation fails with ApiTypeHint in message
	r2 := Request{Model: "m", ApiTypeHint: ApiType("not-a-valid-type")}
	err2 := r2.Validate()
	require.Error(t, err2)
	assert.Contains(t, err2.Error(), "ApiTypeHint")
}
```

**Verification**:
```
go test . -run TestApiType
go test . -run TestRequest_Validate_ApiTypeHint
```

---

## Task 15: Tests — openrouter `selectAPI` and `upstreamProviderFromModel`

**Files modified**: `provider/openrouter/openrouter_test.go` (extend)  
**Estimated time**: 4 minutes

```go
func TestSelectAPI(t *testing.T) {
	tests := []struct {
		model    string
		hint     llm.ApiType
		wantBack orAPIBackend
		wantType llm.ApiType
	}{
		// Explicit hints override auto-dispatch for any model
		{"any", llm.ApiTypeOpenAIResponses, orResponses, llm.ApiTypeOpenAIResponses},
		{"any", llm.ApiTypeAnthropicMessages, orMessages, llm.ApiTypeAnthropicMessages},
		{"any", llm.ApiTypeOpenAIChatCompletion, orChatCompletions, llm.ApiTypeOpenAIChatCompletion},
		// Auto: openai codex and gpt-5.4-series → Responses
		{"openai/gpt-5.3-codex", llm.ApiTypeAuto, orResponses, llm.ApiTypeOpenAIResponses},
		{"openai/gpt-5.4", llm.ApiTypeAuto, orResponses, llm.ApiTypeOpenAIResponses},
		{"openai/gpt-5.4-mini", llm.ApiTypeAuto, orResponses, llm.ApiTypeOpenAIResponses},
		{"openai/gpt-5.4-pro", llm.ApiTypeAuto, orResponses, llm.ApiTypeOpenAIResponses},
		// Auto: other openai → Chat Completions
		{"openai/gpt-4o", llm.ApiTypeAuto, orChatCompletions, llm.ApiTypeOpenAIChatCompletion},
		{"openai/gpt-4-turbo", llm.ApiTypeAuto, orChatCompletions, llm.ApiTypeOpenAIChatCompletion},
		// Auto: anthropic/* → Messages
		{"anthropic/claude-opus-4-5", llm.ApiTypeAuto, orMessages, llm.ApiTypeAnthropicMessages},
		{"anthropic/claude-haiku-4-5", llm.ApiTypeAuto, orMessages, llm.ApiTypeAnthropicMessages},
		// Auto: unknown prefix → Chat Completions
		{"meta/llama-4-maverick", llm.ApiTypeAuto, orChatCompletions, llm.ApiTypeOpenAIChatCompletion},
		{"auto", llm.ApiTypeAuto, orChatCompletions, llm.ApiTypeOpenAIChatCompletion},
	}
	for _, tc := range tests {
		t.Run(tc.model+"/hint="+string(tc.hint), func(t *testing.T) {
			gotBack, gotType := selectAPI(tc.model, tc.hint)
			assert.Equal(t, tc.wantBack, gotBack, "backend")
			assert.Equal(t, tc.wantType, gotType, "resolved ApiType")
		})
	}
}

func TestUpstreamProviderFromModel(t *testing.T) {
	tests := []struct{ model, want string }{
		{"anthropic/claude-opus-4-5", "anthropic"},
		{"openai/gpt-4o", "openai"},
		{"meta-llama/llama-4-maverick", "meta-llama"},
		{"auto", providerName},   // no slash → "openrouter"
		{"gpt-4o", providerName}, // no slash → "openrouter"
	}
	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			assert.Equal(t, tc.want, upstreamProviderFromModel(tc.model))
		})
	}
}
```

**Verification**:
```
go test ./provider/openrouter/... -run TestSelectAPI
go test ./provider/openrouter/... -run TestUpstreamProviderFromModel
```

---

## Task 16: Tests — `orRespBuildRequest`

**Files created**: `provider/openrouter/api_responses_test.go`  
**Estimated time**: 3 minutes

```go
package openrouter

import (
	"encoding/json"
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrRespBuildRequest_Basic(t *testing.T) {
	opts := llm.Request{
		Model:    "openai/gpt-5.4",
		Messages: llm.Messages{llm.User("hello")},
	}
	body, err := orRespBuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))

	assert.Equal(t, "openai/gpt-5.4", req["model"],
		"model must not strip the openai/ prefix — OpenRouter uses full IDs")
	assert.Equal(t, true, req["stream"])
	assert.NotNil(t, req["input"], "input array must be present")
}

func TestOrRespBuildRequest_NoPromptCacheRetention(t *testing.T) {
	// orRespBuildRequest never sets prompt_cache_retention (OpenRouter doesn't
	// support it) so any request must produce a body without the field.
	opts := llm.Request{
		Model:    "openai/gpt-5.4",
		Messages: llm.Messages{llm.User("hi")},
	}
	body, err := orRespBuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))
	assert.Nil(t, req["prompt_cache_retention"],
		"OpenRouter does not support prompt_cache_retention — must not be set")
}
```

**Verification**:
```
go test ./provider/openrouter/... -run TestOrRespBuildRequest
```

---

## Task 17: Tests — `ParseOpts.UpstreamProvider` in `StreamStartedEvent.Provider`

**Files modified**: `provider/anthropic/stream_processor_test.go` (extend)  
**Estimated time**: 3 minutes

Add a `findStarted` helper alongside the existing `findError` and `findToolCall`:

```go
// findStarted returns the first StreamStartedEvent envelope, or nil.
func findStarted(envelopes []llm.Envelope) *llm.StreamStartedEvent {
	for _, env := range envelopes {
		if ev, ok := env.Data.(llm.StreamStartedEvent); ok {
			return &ev
		}
	}
	return nil
}
```

Note: `env.Data` is `llm.StreamStartedEvent` (value type, not pointer) — the type assertion is `.(llm.StreamStartedEvent)`, not `.(*llm.StreamStartedEvent)`.

Add two tests:

```go
func TestProcessor_Provider_FallsBackToProviderName(t *testing.T) {
	h := newHarness(ParseOpts{
		Model:        "claude-sonnet-4-5",
		ProviderName: "myrouter",
		// UpstreamProvider intentionally not set
	})
	envelopes := h.Send(
		MessageStartEvent{Message: MessageStartPayload{
			ID: "msg_01", Model: "claude-sonnet-4-5",
			Usage: MessageUsage{InputTokens: 5},
		}},
	)
	started := findStarted(envelopes)
	require.NotNil(t, started, "StreamStartedEvent must be emitted")
	assert.Equal(t, "myrouter", started.Provider,
		"Provider should fall back to ProviderName when UpstreamProvider is empty")
}

func TestProcessor_Provider_UpstreamOverridesProviderName(t *testing.T) {
	h := newHarness(ParseOpts{
		Model:            "claude-sonnet-4-5",
		ProviderName:     "openrouter",
		UpstreamProvider: "anthropic",
	})
	envelopes := h.Send(
		MessageStartEvent{Message: MessageStartPayload{
			ID: "msg_02", Model: "claude-sonnet-4-5",
			Usage: MessageUsage{InputTokens: 5},
		}},
	)
	started := findStarted(envelopes)
	require.NotNil(t, started, "StreamStartedEvent must be emitted")
	assert.Equal(t, "anthropic", started.Provider,
		"UpstreamProvider must override ProviderName in StreamStartedEvent.Provider")
}
```

**Verification**:
```
go test ./provider/anthropic/... -run TestProcessor_Provider
```

---

## Final verification

```bash
go build ./...
go vet ./...
go test ./...
```

Manual smoke test (requires `OPENROUTER_API_KEY`):
```bash
# Auto → Anthropic Messages; upstream shows "anthropic"
go run ./cmd/llmcli infer -v -m openrouter/anthropic/claude-opus-4-5 "Say hi"
# ── request params ──  api_type_hint: (absent)  resolved_api_type: anthropic-messages
# ── stream started ──  provider: anthropic

# Auto → Responses API
go run ./cmd/llmcli infer -v -m openrouter/openai/gpt-5.4 "Say hi"
# ── request params ──  resolved_api_type: openai-responses
# ── stream started ──  provider: openai

# Auto → Chat Completions (gpt-4o stays on chat path)
go run ./cmd/llmcli infer -v -m openrouter/openai/gpt-4o "Say hi"
# ── request params ──  resolved_api_type: openai-chat

# Explicit hint overrides auto
go run ./cmd/llmcli infer -v -m openrouter/anthropic/claude-opus-4-5 --api openai-chat "Say hi"
# ── request params ──  api_type_hint: openai-chat  resolved_api_type: openai-chat

# "auto" flag accepted (same as omitting --api)
go run ./cmd/llmcli infer -v -m openrouter/openai/gpt-4o --api auto "Say hi"
# (should not error; resolved_api_type: openai-chat)
```
