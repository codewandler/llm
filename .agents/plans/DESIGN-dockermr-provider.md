# DESIGN: Docker Model Runner Provider

**Date**: 2025-07  
**Status**: Draft v2 (refined)

---

## Problem

The codebase has no provider for Docker Model Runner (DMR), which is now built into Docker
Desktop 4.40+ and available as a Docker Engine plugin on Linux. Developers already using
Docker are likely to have DMR available without any additional setup, making it a natural
local inference option alongside the existing Ollama provider.

---

## What is Docker Model Runner?

DMR is Docker's native local LLM inference engine, backed by llama.cpp (default), vLLM
(Linux/NVIDIA), and Diffusers (image generation). Key properties:

- **OpenAI-compatible API** — `/engines/{engine}/v1/chat/completions` uses the identical
  SSE wire format as `api.openai.com/v1/chat/completions`.
- **No API key** — local inference, no authentication header.
- **OCI model distribution** — models live in Docker Hub's `ai/` namespace as OCI
  artifacts, e.g. `ai/smollm2`, `ai/qwen2.5:7B-Q4_K_M`.
- **Three endpoint contexts**:
  - Host (Docker Desktop TCP mode): `http://localhost:12434`
  - Inside Docker Desktop containers: `http://model-runner.docker.internal`
  - Inside Docker CE containers: `http://172.17.0.1:12434`

---

## Goals

1. A `provider/dockermr` package that implements `llm.Provider` and is usable standalone.
2. Auto-detection in `provider/auto` — detect DMR by probing its model list endpoint.
3. An explicit `auto.WithDockerModelRunner(...)` opt-in option for non-default addresses.
4. Full streaming, tool calling, and token estimation — feature parity with the Ollama provider.
5. No new dependencies.

## Non-Goals

- vLLM or Diffusers backends (llama.cpp only for now).
- Docker socket transport (`/var/run/docker.sock`) — TCP only.
- Model pull / management (`docker model pull`) — that belongs to the CLI, not this library.
- Embeddings API — future work.
- Image generation — future work.

---

## Architecture

### Package: `provider/dockermr`

A self-contained package. It does **not** import `provider/openai` — even though the wire
format is identical, coupling to another provider package would be fragile. Instead it
shares the already-internal `internal/sse` package (same as the OpenAI provider does) and
independently implements the request builder and stream parser.

```
provider/dockermr/
  dockermr.go        – Provider struct, New(), Name(), Models(), FetchModels(), CreateStream()
  request.go         – buildRequest(): OpenAI-format JSON body
  stream.go          – parseStream(): sse.ForEachDataLine callback, tool accumulation
  models.go          – Curated ai/ model constants and static list
  token_counter.go   – CountTokens() via cl100k_base (same as Ollama)
  dockermr_test.go   – Table-driven unit tests (no real DMR required)
```

`FetchModels` lives in `dockermr.go` rather than a separate file — it is a single method
and the Ollama provider follows the same convention.

### Wire format reuse

The stream parser mirrors `ccParseStream` in `provider/openai/api_completions.go`:

- Uses `sse.ForEachDataLine` from `internal/sse`
- Calls `pub.Started(llm.StreamStartedEvent{Model: chunk.Model, RequestID: chunk.ID})` on
  the first non-empty chunk
- Emits `pub.Delta(llm.TextDelta(...))` for text content deltas
- Accumulates tool call fragments in `map[int]*toolAccum` keyed by
  `delta.tool_calls[].index`, streaming each fragment as
  `pub.Delta(llm.ToolDelta(...).WithIndex(...))` for real-time progress
- Captures `stopReason` from `finish_reason` when it is non-nil; emits `pub.ToolCall()`
  (sorted by index, with accumulated args JSON-unmarshalled) when
  `finish_reason == "tool_calls"`
- On `[DONE]`: emits `pub.UsageRecord()` using `usage.TokenItems{...}.NonZero()`, then
  `pub.Completed(llm.CompletedEvent{StopReason: stopReason})`

`finish_reason` and `[DONE]` are separate events (as in OpenAI): `stopReason` is
captured from the former, `Completed()` is only emitted when the latter arrives.

DMR/llama.cpp will not emit `cached_tokens` or `reasoning_tokens`, so those fields are
omitted from the chunk struct — less noise, no behavioural difference.

### Request builder

Identical to the OpenAI Chat Completions request shape:
- `model`, `messages`, `tools`, `stream: true`
- `max_tokens`, `temperature`, `top_p`
- Tool results as `role: "tool"` messages with `tool_call_id`
- Assistant tool calls serialised as `tool_calls[].function.arguments` (JSON string)

No `tool_choice` — DMR/llama.cpp does not support it; silently ignored (same as Ollama).

### Model listing / detection (dual-purpose probe)

```
GET {baseURL}/engines/llama.cpp/v1/models
→ {"data": [{"id": "ai/smollm2", ...}, ...]}
```

This is the **same request** used for both:
1. **Auto-detection**: a successful 200 response means DMR is running.
2. **`FetchModels()`**: returns the live list of locally pulled models.

Detection creates a probe client that **reuses the shared transport** (to respect any proxy
or TLS configuration) but **overrides the timeout to 500 ms** so it doesn't block
`auto.New()` on machines without DMR:

```go
probeClient := &http.Client{
    Transport: sharedClient.Transport, // nil = http.DefaultTransport
    Timeout:   500 * time.Millisecond,
}
```

A 200 response from `GET {baseURL}/engines/llama.cpp/v1/models` is sufficient to confirm
DMR is operational. The response body is discarded at detection time; callers that need
the live model list call `FetchModels()` explicitly.

### Auto-detection placement

DMR is inserted **last** in `detectProviders()` — after all cloud providers and after
Ollama is eventually added. Rationale: cloud providers (Claude, Bedrock, OpenAI) are
preferred when available; local inference is a fallback for offline/cost-sensitive use.

### `ProviderNameDockerMR` constant

Added to `errors.go` alongside the existing `ProviderNameOllama`, `ProviderNameOpenAI`,
etc. This constant is used throughout the provider for error construction and usage records.

---

## API Design

### `dockermr.New(opts ...llm.Option) *Provider`

```go
// Default: localhost:12434, engine: llama.cpp, model: ai/smollm2
p := dockermr.New()

// Inside a Docker container (Docker Desktop)
p := dockermr.New(llm.WithBaseURL(dockermr.ContainerBaseURL))

// Custom engine
p := dockermr.New().WithEngine("vllm")
```

### `provider/auto` integration

```go
// Auto-detected (probes localhost:12434):
r, _ := auto.New(ctx)

// Explicit opt-in — default address:
r, _ := auto.New(ctx, auto.WithDockerModelRunner())

// Explicit opt-in — container address (Docker Desktop):
r, _ := auto.New(ctx,
    auto.WithDockerModelRunner(llm.WithBaseURL(dockermr.ContainerBaseURL)),
)

// Explicit opt-in — Docker CE container address:
r, _ := auto.New(ctx,
    auto.WithDockerModelRunner(llm.WithBaseURL("http://172.17.0.1:12434")),
)

// Opt-out of auto-detection:
r, _ := auto.New(ctx, auto.WithoutProvider(auto.ProviderDockerMR))
```

`WithDockerModelRunner(opts ...llm.Option)` is variadic, matching the signature of
`dockermr.New()`. This lets callers pass any `llm.Option` (e.g. custom HTTP client,
logger) without a separate API surface.

### `FetchModels(ctx) ([]llm.Model, error)`

Returns all models currently pulled onto the local machine. Model IDs match the Docker Hub
format (`ai/smollm2:360M-Q4_K_M`). Useful for building dynamic model selectors.

---

## Key Design Decisions

| Decision | Rationale |
|---|---|
| No import of `provider/openai` | Avoids cross-provider coupling; `internal/sse` is the right sharing point |
| Probe = model list GET | Dual purpose: detection + validation that the API works end-to-end |
| Probe reuses shared transport | Respects proxy/TLS settings; overrides timeout to 500 ms only |
| `llama.cpp` default engine | Only engine supported on all platforms (macOS, Windows, Linux) |
| No `tool_choice` support | llama.cpp via DMR does not honour it; consistent with Ollama behaviour |
| cl100k_base token estimate | Same approximation as Ollama; DMR uses GGUF models with mixed tokenizers |
| `ai/smollm2` as default model | Smallest available `ai/` model (360M); succeeds on low-memory machines |
| Static `Models()` list | Curated known-good models; `FetchModels()` for live local list |

---

## Acceptance Criteria

- [ ] `dockermr.New()` builds a provider; `p.Name()` returns `"dockermr"`.
- [ ] `p.Models()` returns the curated static list.
- [ ] `p.FetchModels(ctx)` queries the live endpoint and parses the response.
- [ ] `p.CreateStream(ctx, req)` sends a Chat Completions request and streams text deltas.
- [ ] Tool calls work end-to-end: fragments accumulated + streamed as `ToolDelta`, `pub.ToolCall()` emitted on `finish_reason: "tool_calls"`.
- [ ] Context cancellation via `sse.ForEachDataLine` is propagated as `ErrContextCancelled`.
- [ ] `pub.Started()` carries `Model` and `RequestID` from the first stream chunk.
- [ ] Auto-detection in `auto.New()` adds the provider when `localhost:12434` responds.
- [ ] `auto.WithDockerModelRunner()` adds the provider unconditionally.
- [ ] `auto.WithoutProvider(auto.ProviderDockerMR)` suppresses detection.
- [ ] All tests pass with `-race`; no real DMR process required.
- [ ] `go vet ./...` and `go build ./...` clean.

---

## Out of Scope (future issues)

- `WithEngine("vllm")` path tested (vllm models use safetensors; different Hub tags)
- Embeddings via `/engines/llama.cpp/v1/embeddings`
- Image generation via Diffusers engine
- Docker socket (`/var/run/docker.sock`) transport
- Context-size configuration (`docker model configure --context-size`)
