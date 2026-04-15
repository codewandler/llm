# API Layer Architecture (`/api`)

This directory contains the protocol-level API stack used by providers.

The core design goal is:

- keep wire protocol logic reusable and isolated
- avoid provider-to-provider imports
- centralize request/event normalization in one internal bridge layer

## Package layout

```text
api/
├── apicore/        # Generic HTTP+SSE client infrastructure (protocol-agnostic)
├── messages/       # Anthropic Messages wire client + types + parser
├── completions/    # OpenAI Chat Completions wire client + types + parser
├── responses/      # OpenAI Responses wire client + types + parser
└── unified/        # (Planned/in progress) canonical request/event bridge layer
```

## Layer responsibilities

### 1) `api/apicore` (shared transport)

`apicore` owns transport mechanics only:

- HTTP request construction
- static + dynamic header merging
- request transforms before JSON serialization
- SSE scanning and event dispatch loop
- response hooks (header/rate-limit extraction)
- error parsing hook for non-2xx responses
- retry transport (`NewRetryTransport`)

It is generic over request type (`Client[Req]`) and has no protocol semantics.

### 2) `api/messages|completions|responses` (wire protocol packages)

Each protocol package owns:

- exact wire structs (`types.go`)
- protocol constants (event names, headers, defaults)
- parser factory (`NewParser`) producing typed native events
- protocol `NewClient` wrapper around `apicore.Client[Request]`

These packages are intentionally wire-focused and reusable.

### 3) `api/unified` (canonical bridge)

`api/unified` is the normalization + bridge layer for provider internals:

- `llm.Request` ⇄ canonical `unified.Request`
- canonical request → protocol wire requests
- protocol native events → canonical unified events
- canonical unified events → `llm.Publisher`

This replaces older scattered adapter/conversion logic.

## End-to-end provider flow (target)

```text
provider.CreateStream
  -> llm.Request
  -> unified.RequestFromLLM
  -> unified.RequestTo<Protocol>
  -> <protocol>.Client.Stream (apicore transport)
  -> unified.EventFrom<Protocol>
  -> unified.Publish(pub, event)
  -> llm stream events
```

Providers should remain thin orchestrators: resolve model/options, select protocol path, and delegate conversion/publish behavior to `api/unified`.

## Dependency rules

- `apicore` must not depend on provider packages.
- `messages/completions/responses` must stay wire-only.
- provider packages must not import each other.
- `llm` must not import `api/unified` (avoid cycles); providers call unified.

## Testing seams

- `apicore`: transport tests via `RoundTripFunc` and SSE fixtures
- protocol packages: parser and wire-JSON tests
- unified package: pure conversion + publish mapping tests
- providers: parity/integration tests with behavior unchanged externally

## Related docs

- `.agents/plans/DESIGN-api-extraction.md`
- `.agents/plans/DESIGN-api-unified.md`
- `.agents/plans/PLAN-20260415-unified.md`
