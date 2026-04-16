# Plan: migrate provider/codex to providercore

## Context

`provider/codex/provider.go` currently hand-rolls everything that `provider/providercore`
already provides centrally: HTTP execution, stream parsing, token estimation,
event publishing, and error handling.  Other providers (openrouter, minimax,
dockermr, ollama, anthropic/claude) have all migrated to `providercore.Client`.
This plan describes the equivalent migration for codex.

## What providercore replaces

| Current codex code | providercore equivalent |
|-|-|
| `transport` struct + `RoundTrip` | `Config.HeaderFunc` + `Config.BasePath` |
| `injectBodyFields` (store/max_tokens) | `Config.TransformWireRequest` |
| `openai.EnrichRequest` + `openai.BuildResponsesBody` | handled internally by `providercore.Client.Stream` |
| `openai.RespParseStream` goroutine | handled internally via `unified.ForwardResponses` |
| manual `llm.NewEventPublisher` / token estimates / HTTP execution | handled internally by `providercore.Client.Stream` |

---

## Steps

### Step 1 — Add `core *providercore.Client` to `Provider` and a `buildCore()` method

```go
type Provider struct {
    auth         *Auth
    opts         *llm.Options
    core         *providercore.Client   // ← new
    defaultModel string
    modelOnce    sync.Once
    models       llm.Models
}
```

Add a `buildCore()` method that constructs a `providercore.Config` (see steps
2–5 below) and calls `providercore.New(cfg)`, storing the result in `p.core`.
Call `buildCore()` at the end of `New()`.

---

### Step 2 — Replace `transport` with `Config.HeaderFunc` + `Config.BasePath`

The custom `transport.RoundTrip` does three things.  Each maps cleanly:

**a) Auth headers**

Move to `Config.HeaderFunc`:

```go
HeaderFunc: func(ctx context.Context, _ *llm.Request) (http.Header, error) {
    token, err := p.auth.Token(ctx)
    if err != nil {
        return nil, fmt.Errorf("codex transport: get token: %w", err)
    }
    return http.Header{
        "Authorization":    {"Bearer " + token},
        accountIDHeader:    {p.auth.AccountID()},
        codexBetaHeader:    {codexBetaValue},
        "originator":       {codexOriginator},
    }, nil
},
```

**b) URL rewrite** (`/v1/responses` → `/codex/responses`)

The rewrite can be eliminated entirely by setting the correct path up front:

```go
BasePath: "/codex/responses",
```

`providercore` appends `BasePath` to `BaseURL`, so the full URL becomes
`https://chatgpt.com/backend-api/codex/responses` directly — no
`MutateRequest` needed.

After this, the entire `transport` struct and its `RoundTrip` method can be
deleted.

---

### Step 3 — Replace `injectBodyFields` with `Config.TransformWireRequest`

`injectBodyFields` mutates the raw JSON body to set `store: false` and strip
`max_tokens`, `max_output_tokens`, `prompt_cache_retention`.  With
`TransformWireRequest` the same adjustment is done on the typed wire object
before marshalling:

```go
TransformWireRequest: func(api llm.ApiType, wire any) (any, error) {
    if api != llm.ApiTypeOpenAIResponses {
        return wire, nil
    }
    req, ok := wire.(*responses.Request)
    if !ok {
        return nil, fmt.Errorf("codex: unexpected responses payload %T", wire)
    }
    store := false
    req.Store = &store
    req.MaxOutputTokens = nil  // field names may differ — check responses.Request
    // strip any other codex-incompatible fields
    return req, nil
},
```

> **Note:** confirm exact field names by inspecting `api/responses.Request`.
> If fields are not present on the struct, use `Config.MutateRequest` to
> post-process the serialised body instead (last resort).

After this, `injectBodyFields` can be deleted.

---

### Step 4 — Replace the body of `CreateStream` with `p.core.Stream`

The current `CreateStream` method manually builds the wire request, fires the
HTTP call, publishes events, and launches a goroutine for stream parsing.
Replace it entirely:

```go
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
    return p.core.Stream(ctx, src)
}
```

All of the following can be deleted from `provider.go`:
- `openai.EnrichRequest` / `openai.BuildResponsesBody` call
- manual `http.NewRequestWithContext` + `p.client.Do`
- `llm.NewEventPublisher` / `pub.Publish` / `pub.TokenEstimate` calls
- `go openai.RespParseStream(...)` goroutine
- the `p.client *http.Client` field on `Provider` (providercore owns the client)

---

### Step 5 — Wire token estimation through `Config.TokenCounter`

The token counter in `token_counter.go` is already a standalone method.
Connect it via the config:

```go
TokenCounter: tokencount.TokenCounterFunc(p.CountTokens),
```

This replaces the manual `p.CountTokens` / `tokencount.EstimateRecords` call
that currently lives inside `CreateStream`.

---

### Step 6 — Clean up `provider.go` imports

After the above steps the following imports are no longer needed in
`provider.go` and should be removed:

- `github.com/codewandler/llm/provider/openai`
- `github.com/codewandler/llm/usage` (if no longer used directly)
- `bytes`, `io`, `sort` (moved into providercore internals)
- `sync` (if `modelOnce` is the only remaining use, keep it; otherwise remove)

Add:
- `github.com/codewandler/llm/provider/providercore`
- `github.com/codewandler/llm/api/responses` (for `TransformWireRequest`)

---

### Step 7 — Leave `FetchModels` unchanged

`FetchModels` is an out-of-band HTTP call (not streamed, not unified).  It
already builds its own `http.Request` with auth headers applied directly.
No change needed.

---

### Step 8 — Verify with integration tests

```
go test -v -run 'TestCodex.*' ./provider/codex/ -timeout 120s
```

All five integration tests must remain green after the migration.

---

## Expected diff summary

| File | Change |
|-|-|
| `provider.go` | Replace `transport`, `injectBodyFields`, manual `CreateStream` body with `buildCore()` + `p.core.Stream` |
| `provider.go` | Drop `p.client` field; add `p.core *providercore.Client` |
| `provider.go` | Slim imports |
| `token_counter.go` | No change |
| `auth.go` | No change |
| `models.go` | No change |
| `source.go` | No change |
