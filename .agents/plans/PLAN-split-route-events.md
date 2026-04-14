# Plan: Split RouteInfoEvent into ModelResolvedEvent + ProviderFailoverEvent

## Goal

Replace the single `RouteInfoEvent` (fired once by the router after a
successful provider selection) with two focused events:

- **`ModelResolvedEvent`** — fired by any code that translates one model
  name to another (router alias lookup, OpenRouter normalization, OpenRouter
  auto-model revealed by the API response).
- **`ProviderFailoverEvent`** — fired only by the router, once per skipped
  provider, when a retriable error causes it to advance to the next target.

---

## Files to change

```
event.go                               core type definitions
event_publisher.go                     Publisher interface + eventPub impl
provider/router/router.go              failover loop + model-resolved emit
provider/openrouter/openrouter.go      normalizeRequestModel + parseStream resolution
provider/anthropic/stream.go           ParseOpts.ProviderName
provider/anthropic/stream_processor.go onMessageStart resolution
cmd/llmcli/cmds/infer.go              verbose handlers + print helpers
```

---

## Step 1 — `event.go`

### 1a. EventType constants

Add `StreamEventModelResolved` and `StreamEventProviderFailover`.
Remove `StreamEventRouted`.

```go
// before
StreamEventRouted EventType = "routed"

// after
StreamEventModelResolved   EventType = "model_resolved"
StreamEventProviderFailover EventType = "provider_failover"
```

### 1b. Remove `RouteInfo` and `RouteInfoEvent`

Delete both structs entirely.

### 1c. Add `ModelResolvedEvent`

Flat struct — no intermediate wrapper type.

```go
// ModelResolvedEvent is emitted whenever a requested model name is
// translated to a different resolved name: by router alias lookup,
// by OpenRouter's default-model normalization, or by OpenRouter
// revealing the actual model chosen for an "auto" request.
ModelResolvedEvent struct {
    Resolver string `json:"resolver"`
    Name     string `json:"name,omitempty"`
    Resolved string `json:"resolved,omitempty"`
}
```

### 1d. Add `ProviderFailoverEvent`

```go
// ProviderFailoverEvent is emitted by the router each time a provider
// attempt fails with a retriable error and the next provider is tried.
// It is NOT emitted when the last provider in the list fails (that is
// terminal, surfaced as an error return, not an event).
ProviderFailoverEvent struct {
    Provider         string `json:"provider"`          // failed provider
    FailoverProvider string `json:"failover_provider"` // next provider
    Error            error  `json:"-"`
}
```

### 1e. Type() methods

```go
func (e ModelResolvedEvent) Type() EventType    { return StreamEventModelResolved }
func (e ProviderFailoverEvent) Type() EventType { return StreamEventProviderFailover }
```

Remove:
```go
func (e RouteInfoEvent) Type() EventType { return StreamEventRouted }
```

### 1f. Publisher interface

Replace `Routed(routed RouteInfo)` with two methods:

```go
ModelResolved(resolver, name, resolved string)
Failover(from, to string, err error)
```

Full updated interface block:

```go
Publisher interface {
    Publish(payload Event)

    Started(started StreamStartedEvent)
    ModelResolved(resolver, name, resolved string)
    Failover(from, to string, err error)
    Delta(d *DeltaEvent)
    ToolCall(tc tool.Call)
    ContentBlock(evt ContentPartEvent)

    Usage(usage Usage)
    Completed(completed CompletedEvent)

    Error(err error)
    Debug(msg string, data any)

    Close()
}
```

---

## Step 2 — `event_publisher.go`

Remove `Routed`. Add `ModelResolved` and `Failover`:

```go
func (s *eventPub) ModelResolved(resolver, name, resolved string) {
    s.Publish(&ModelResolvedEvent{
        Resolver: resolver,
        Name:     name,
        Resolved: resolved,
    })
}

func (s *eventPub) Failover(from, to string, err error) {
    s.Publish(&ProviderFailoverEvent{
        Provider:         from,
        FailoverProvider: to,
        Error:            err,
    })
}
```

---

## Step 3 — `provider/router/router.go`

### Current shape of CreateStream (simplified)

```go
var triedErrors []error
for _, target := range targets {
    stream, err := target.provider.CreateStream(ctx, streamOpts)
    if err != nil {
        if isRetriableError(pe) {
            triedErrors = append(triedErrors, pe)
            continue
        }
        return nil, pe
    }

    pub, ch := llm.NewEventPublisher()
    pub.Routed(llm.RouteInfo{
        Provider:       target.providerName,
        ModelRequested: opts.Model,
        ModelResolved:  target.fullID,
        Errors:         triedErrors,
    })
    go func() { /* forward */ }()
    return ch, nil
}
```

### New shape

Collect failover records during the loop (publisher is still created only
after a successful provider — avoids creating a channel that is never
returned to the caller in the all-failed case).

```go
type failoverRecord struct {
    from string
    to   string
    err  error
}

var triedErrors []error
var failovers   []failoverRecord

for i, target := range targets {
    streamOpts := opts
    streamOpts.Model = target.modelID

    stream, err := target.provider.CreateStream(ctx, streamOpts)
    if err != nil {
        pe := llm.AsProviderError(target.providerName, err)
        if isRetriableError(pe) {
            triedErrors = append(triedErrors, pe)
            // Only record a failover when there IS a next target.
            if i+1 < len(targets) {
                failovers = append(failovers, failoverRecord{
                    from: target.providerName,
                    to:   targets[i+1].providerName,
                    err:  pe,
                })
            }
            continue
        }
        return nil, pe
    }

    pub, ch := llm.NewEventPublisher()

    // Replay failover events in order before the model-resolved event.
    for _, f := range failovers {
        pub.Failover(f.from, f.to, f.err)
    }
    pub.ModelResolved(p.name, opts.Model, target.fullID)

    go func() {
        defer pub.Close()
        for evt := range stream {
            if evt.Type == llm.StreamEventCreated {
                continue
            }
            if started, ok := evt.Data.(*llm.StreamStartedEvent); ok {
                // Only fill in model when the provider did not supply one.
                // Preserves the resolved model when OpenRouter reports a
                // real model for an "auto" request.
                if started.Model == "" {
                    started.Model = target.modelID
                }
            }
            pub.Publish(evt.Data.(llm.Event))
        }
    }()
    return ch, nil
}

if len(triedErrors) > 0 {
    return nil, llm.NewErrAllProvidersFailed(llm.ProviderNameRouter, triedErrors)
}
return nil, llm.NewErrNoProviders(llm.ProviderNameRouter)
```

The `failoverRecord` type is a file-private type — declare it at the top
of `router.go`, not exported.

---

## Step 4 — `provider/openrouter/openrouter.go`

`normalizeRequestModel` runs before the publisher exists. Save the original
model name, then emit `ModelResolved` after the publisher is created if the
names differ.

```go
func (p *Provider) CreateStream(ctx context.Context, opts llm.Request) (llm.Stream, error) {
    requestedModel := opts.Model                    // save before mutation
    opts.Model = p.normalizeRequestModel(opts.Model)

    if err := opts.Validate(); err != nil { ... }

    // ... build body, http request ...

    pub, ch := llm.NewEventPublisher()

    // Emit model resolution if normalizeRequestModel changed the name.
    if opts.Model != requestedModel {
        pub.ModelResolved(llm.ProviderNameOpenRouter, requestedModel, opts.Model)
    }

    pub.Publish(&llm.RequestEvent{ ... })

    // ... send HTTP request, start parseStream goroutine ...
}
```

---

## Step 5 — Stream-based model resolution (all providers)

This is a general pattern: whenever a provider's stream parser learns the
actual model from the API response, it should compare it against the model
that was requested. If they differ, emit `ModelResolvedEvent` (with
`Resolver` = the provider name) **before** `StreamStartedEvent`.

Two providers are currently affected.

### 5a. Anthropic — `provider/anthropic/stream_processor.go`

`streamProcessor` already has everything it needs:
- `p.meta.Model` — the requested model (from `ParseOpts.Model`)
- `evt.Message.Model` — the model the API actually used (from `message_start`)

However, `ParseOpts` does not currently carry a provider name, and the
package is shared between `provider/anthropic` (direct) and
`provider/anthropic/claude` (OAuth), which have different provider names.

Add `ProviderName string` to `ParseOpts`:

```go
type ParseOpts struct {
    Model          string
    ProviderName   string   // NEW — "anthropic", "claude", etc.
    ResponseHeaders http.Header
    LLMRequest     llm.Request
    CostFn         CostFn
}
```

Each caller of `ParseStream` / `ParseStreamWith` already passes `ParseOpts`
and sets the provider-specific fields; they just need to add `ProviderName`.

In `onMessageStart`, emit `ModelResolvedEvent` before `Started` when the
model differs:

```go
func (p *streamProcessor) onMessageStart(evt MessageStartEvent) {
    // ... existing usage fields ...

    if evt.Message.Model != "" && evt.Message.Model != p.meta.Model {
        p.pub.ModelResolved(p.meta.ProviderName, p.meta.Model, evt.Message.Model)
    }

    p.pub.Started(llm.StreamStartedEvent{
        Model:     evt.Message.Model,
        RequestID: evt.Message.ID,
        Extra:     extra,
    })
}
```

### 5b. OpenRouter — `provider/openrouter/openrouter.go`

OpenRouter uses OpenAI-compatible SSE format. There is no `message_start`
event type — the resolved model is in the first chunk's `chunk.Model`
field, already handled by the `!startEmitted` guard.

Thread `requestedModel` into `parseStream` (the model after client-side
normalization, i.e. `opts.Model` as it stands when the goroutine is
launched):

```go
// before
go parseStream(ctx, resp.Body, pub)

// after
go parseStream(ctx, resp.Body, pub, opts.Model)
```

```go
func parseStream(ctx context.Context, body io.ReadCloser, pub llm.Publisher, requestedModel string) {
```

Inside the `!startEmitted` block:

```go
if !startEmitted {
    startEmitted = true
    if chunk.Model != "" && chunk.Model != requestedModel {
        pub.ModelResolved(providerName, requestedModel, chunk.Model)
    }
    pub.Started(llm.StreamStartedEvent{
        Model:     chunk.Model,
        RequestID: chunk.ID,
    })
}
```

`providerName` is the package-level `const providerName = "openrouter"` at
line 21 of `openrouter.go`.

---

## Step 6 — `cmd/llmcli/cmds/infer.go`

### 6a. Add `ProviderFailoverEvent` handler in the processor chain

Insert before the existing `ModelResolvedEvent` handler:

```go
OnEvent(llm.TypedEventHandler[*llm.ProviderFailoverEvent](func(ev *llm.ProviderFailoverEvent) {
    if verbose {
        printProviderFailoverEvent(ev)
        verboseOutputPrinted = true
    }
})).
OnEvent(llm.TypedEventHandler[*llm.ModelResolvedEvent](func(ev *llm.ModelResolvedEvent) {
    if verbose {
        printModelResolvedEvent(ev)
        verboseOutputPrinted = true
    }
})).
```

### 6b. Rename `printRouteInfoEvent` → `printModelResolvedEvent`

Update the function signature and header string:

```go
func printModelResolvedEvent(ev *llm.ModelResolvedEvent) {
    fmt.Fprintln(os.Stderr)
    fmt.Fprintf(os.Stderr, "%s── model resolved ──%s\n", ansiDim, ansiReset)
    var fields []kvField
    if ev.Resolver != "" {
        fields = append(fields, kvField{"resolver", ev.Resolver})
    }
    if ev.Name != "" {
        fields = append(fields, kvField{"name", ev.Name})
    }
    if ev.Resolved != "" {
        fields = append(fields, kvField{"resolved", ev.Resolved})
    }
    printFields(fields)
}
```

Note: field access changes from `ev.RouteInfo.Provider` to `ev.Resolver`
because the struct is now flat.

### 6c. Add `printProviderFailoverEvent`

```go
func printProviderFailoverEvent(ev *llm.ProviderFailoverEvent) {
    fmt.Fprintln(os.Stderr)
    fmt.Fprintf(os.Stderr, "%s── provider failover ──%s\n", ansiDim, ansiReset)
    var fields []kvField
    fields = append(fields, kvField{"from", ev.Provider})
    fields = append(fields, kvField{"to", ev.FailoverProvider})
    if ev.Error != nil {
        fields = append(fields, kvField{"error", ev.Error.Error()})
    }
    printFields(fields)
}
```

---

## Event ordering on the consumer channel

For a request that fails over once before succeeding, and where OpenRouter
resolves "auto" to a real model, consumers see:

```
StreamCreatedEvent          (always first, from NewEventPublisher)
ProviderFailoverEvent       (from router — 0..N, one per skipped provider)
ModelResolvedEvent          (from router — alias/fullID resolution, Resolver=p.name)
ModelResolvedEvent          (from openrouter — "" → "auto", Resolver=openrouter)   [if applicable]
RequestEvent                (from openrouter — HTTP wire params)
ModelResolvedEvent          (from openrouter stream — "auto" → actual model, Resolver=openrouter)
StreamStartedEvent          (from openrouter stream)
```

For a direct Anthropic call where the API returns a different model version
than requested:

```
StreamCreatedEvent
ModelResolvedEvent          (from anthropic stream_processor — Resolver=anthropic)
StreamStartedEvent
DeltaEvent...
```
DeltaEvent...               (content)
StreamDoneEvent / Completed
StreamClosedEvent           (always last, from pub.Close)
```

When no failover occurs and no model normalization is needed, the first
three events after `StreamCreatedEvent` collapse to a single
`ModelResolvedEvent` from the router.

---

## Tests to add / update

### `event_http_test.go` (existing)
No changes needed.

### `provider/router/router_test.go`
- Update any test that asserts on `RouteInfoEvent` to use `ModelResolvedEvent`
  with direct field access (`ev.Resolver`, not `ev.RouteInfo.Provider`).
- Add test: single provider, no failover → one `ModelResolvedEvent`, zero
  `ProviderFailoverEvent`.
- Add test: two providers, first fails with retriable error, second succeeds
  → one `ProviderFailoverEvent{Provider: "p1", FailoverProvider: "p2"}` then
  one `ModelResolvedEvent`.
- Add test: two providers, both fail → `NewErrAllProvidersFailed`, zero
  events on any channel (channel never returned).

### `provider/anthropic/stream_processor.go`
- Add test: `message_start.model` differs from `ParseOpts.Model` →
  `ModelResolvedEvent` emitted before `StreamStartedEvent`.
- Add test: `message_start.model` equals `ParseOpts.Model` → no
  `ModelResolvedEvent` emitted.

### `provider/openrouter/openrouter_test.go`
- Add test: model = `""` (default) → `ModelResolvedEvent{Name: "",
  Resolved: "auto"}` emitted before `RequestEvent`.
- Add test: model already set → no `ModelResolvedEvent` from `CreateStream`.
- Add test: first chunk `model` differs from `requestedModel` →
  `ModelResolvedEvent` emitted before `StreamStartedEvent`.
- Add test: first chunk `model` equals `requestedModel` → no extra
  `ModelResolvedEvent`.

---

## What does NOT change

- `StreamStartedEvent`, `RequestEvent`, all delta/tool/usage events
- `provider/anthropic`, `provider/minimax`, `provider/bedrock`,
  `provider/ollama` — only `provider/anthropic` changes (via
  `stream_processor.go`); minimax, bedrock, ollama are unaffected
- `llmtest` package — add helpers for the two new event types if needed
- Wire format of existing event types — only `"routed"` is removed and
  replaced; all others unchanged
