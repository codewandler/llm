# Design: `api/unified` — Canonical + Adapter Layer (Merged Plan)

> **Scope**: Introduce `api/unified` as the canonical interchange schema **and** the home for protocol bridges that replace the old `api/adapt` plan.
> **Out of scope**: Replacing user-facing `llm.Request` / `msg.*` API.
> **Related**: `DESIGN-api-extraction.md`.

---

## Decision summary

We are merging the former `api/adapt` responsibilities into the unified design.

- `api/messages|completions|responses` remain pure wire layers.
- `api/unified` becomes the single normalization + bridge layer.
- The previous standalone adapt plan is retired; all required work is now planned under the unified plan.

---

## Problem

Current translation logic is split across multiple places:

- per-protocol wire conversion code
- per-protocol event adaptation code
- provider glue code

This duplicates mapping logic and makes protocol-to-protocol conversion hard.

---

## Canonical vs user-facing model (important)

There are three layers:

1. **`llm.Request` + `msg.*` (public user-facing API)**
   - ergonomic SDK surface
   - remains public contract

2. **`api/unified` (internal canonical interchange)**
   - normalization hub between domain and wire protocols
   - optimized for conversion correctness and parity

3. **`api/messages|completions|responses` (wire protocols)**
   - transport-native request/event structs

`api/unified` is canonical for internal interchange, **not** a direct replacement for `llm.Request`.

---

## Target architecture (after migration)

```text
llm / provider packages
    │
    │ (llm.Request + msg.*)
    ▼
api/unified
    ├── core schema (Request, Message, Part, StreamEvent, Usage, StopReason)
    ├── protocol converters (messages/completions/responses)
    └── publisher bridge (unified event -> llm.Publisher)
    │
    ▼
api/messages | api/completions | api/responses
    │
    ▼
apicore.Client[Req] + SSE parser
```

---

## Package layout

```text
api/unified/
├── types_request.go        # canonical Request/Message/Part/Tool schema
├── types_event.go          # canonical stream event schema
├── types_extensions.go     # protocol/provider extension containers
├── validate.go             # canonical validation helpers
│
├── llm_bridge.go           # llm.Request <-> unified.Request
├── publisher_bridge.go     # unified.StreamEvent -> llm.Publisher
│
├── messages_api.go         # unified <-> messages + stream event conversion
├── completions_api.go      # unified <-> completions + stream event conversion
└── responses_api.go        # unified <-> responses + stream event conversion
```

Notes:
- `api/unified` is internal; it may import `llm` for bridge/publisher functions.
- `llm` must not import `api/unified` directly to avoid cycles.
- Public API in `llm` stays unchanged.

---

## Conversion contracts

### Requests

```go
func RequestFromLLM(req llm.Request) (Request, error)
func RequestToLLM(req Request) (llm.Request, error) // optional helper; best-effort

func RequestToMessages(r Request, opts ...MessagesOption) (*messages.Request, error)
func RequestToCompletions(r Request, opts ...CompletionsOption) (*completions.Request, error)
func RequestToResponses(r Request, opts ...ResponsesOption) (*responses.Request, error)

func RequestFromMessages(r messages.Request) (Request, error)
func RequestFromCompletions(r completions.Request) (Request, error)
func RequestFromResponses(r responses.Request) (Request, error)
```

### Events

```go
func EventFromMessages(ev any) (StreamEvent, bool, error)
func EventFromCompletions(ev any) (StreamEvent, bool, error)
func EventFromResponses(ev any) (StreamEvent, bool, error)

func Publish(pub llm.Publisher, ev StreamEvent) error
```

`bool` means intentionally ignored/no-op event.

---

## Event handling policy

For each protocol bridge in `api/unified/*_api.go`:

1. Handle all currently documented wire events explicitly.
2. Known non-actionable events use explicit no-op branches.
3. `default` is only for future unknown events.
4. Preserve unknown raw details in `StreamEvent.Extras.RawEvent` when available.

---

## Roundtrip guarantees

Short answer: **semantic roundtrip is required**, byte-perfect roundtrip is not.

### Levels

1. **Semantic roundtrip (required)**
   - model, roles, text/thinking/tool semantics, usage, stop reason survive conversion.

2. **Structural roundtrip (best effort)**
   - ordering/index/block boundaries preserved where protocols expose them.

3. **Byte-perfect roundtrip (not required)**
   - JSON key order/whitespace/raw formatting not guaranteed.

### Preservation mechanisms

- `Request.Extras` / `StreamEvent.Extras` for protocol/provider details
- `Part.Native` for non-canonical block payloads
- `Extras.RawEvent` for unknown future events

Contract:
- `wire -> unified -> same wire` should preserve semantics.
- `wireA -> unified -> wireB` is best-effort and may be lossy when features differ.

---

## Provider code after migration

End-user API remains unchanged:

```go
req := llm.Request{Model: "fast", Messages: llm.Messages{msg.User("hi").Build()}}
stream, _ := provider.CreateStream(ctx, req)
```

Provider internals become thin orchestrators:

```go
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	req, err := src.BuildRequest(ctx)
	if err != nil { return nil, err }

	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()

		uReq, err := unified.RequestFromLLM(req)
		if err != nil { pub.Error(err); return }

		wireReq, err := unified.RequestToResponses(uReq)
		if err != nil { pub.Error(err); return }

		h, err := p.responsesClient.Stream(ctx, wireReq)
		if err != nil { pub.Error(err); return }

		for r := range h.Events {
			if r.Err != nil { pub.Error(r.Err); return }
			uEv, ignored, err := unified.EventFromResponses(r.Event)
			if err != nil { pub.Error(err); return }
			if ignored { continue }
			_ = unified.Publish(pub, uEv)
		}
	}()

	return ch, nil
}
```

---

## Migration sequence

### Phase 1 (non-breaking): introduce unified core + request bridges
- add canonical request types
- add llm<->unified request bridge
- add unified->wire request converters
- parity tests vs existing per-protocol adapt logic

### Phase 2: unify event conversion
- add canonical event types
- implement EventFromMessages/Completions/Responses
- preserve unknown raw events in extras
- parity tests vs current adapter event streams

### Phase 3: replace old adapter paths
- update providers to use unified bridge path
- remove duplicated conversion logic
- keep provider external behavior unchanged

### Phase 4: retire old adapt plan/docs
- remove stale references to the retired adapt plan
- keep `llm` public API unchanged

---

## Risks and mitigations

| Risk | Mitigation |
|------|------------|
| Over-normalization loses details | Extensions + Native + RawEvent fields |
| Behavior drift | parity/golden tests at adapter boundary |
| Import cycles | `llm` must not import `api/unified` directly |
| Migration blast radius | phased rollout; requests first, events second |

---

## Acceptance criteria

1. `api/unified` exists and includes both canonical schema + bridge functions.
2. Existing provider behavior remains unchanged (parity tests pass).
3. Old adapt-plan responsibilities are fully represented in unified plan tasks.
4. `api/messages|completions|responses` remain pure wire layers.
5. Public `llm.Request` / `msg.*` API remains stable.
