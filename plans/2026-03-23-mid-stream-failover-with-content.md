# Plan: Mid-Stream Failover After Content Has Been Emitted

## Problem Statement

The current failover implementation (added in this session) handles retriable in-stream
errors only when **no content has been emitted yet** (e.g. Anthropic's "Overloaded" error
arrives before the first `message_start` or `content_block_delta`).

If an "Overloaded" (or network-level stream-read error) arrives **after the model has
already streamed several tokens**, the router cannot transparently retry: the consumer
has already received a partial response and simply switching to another provider and
starting fresh would produce a duplicate or inconsistent output.

This plan covers what needs to change to support graceful mid-stream failover **with
content already emitted**, including the design decisions that must be resolved first.

---

## Core Design Decision (must be resolved before implementation)

There are two recovery strategies for a mid-stream error after content has been emitted.
The choice affects the entire design.

### Option A — Restart on the fallback, signal the break to the consumer

The router injects a `StreamEventRetry` (or a new flag on `StreamEventRouted`) into the
output channel. The consumer sees:

```
StreamEventStart      (from prov1)
StreamEventDelta      "Hello, here is " …
StreamEventRetry      {Reason: "Overloaded", Provider: "prov1", FallbackProvider: "prov2"}
StreamEventStart      (from prov2, fresh request)
StreamEventDelta      "Hello, here is the answer you…"   ← full response, from the top
StreamEventDone
```

Consumer responsibility: decide whether to replace or concatenate partial output.
Use-case fit: good for display layers that can show "retried on prov2" and discard
the partial; bad for agentic loops that have already passed partial text downstream.

### Option B — Continuation prompt: resume where we left off

The router accumulates the tokens it has forwarded so far. When a retriable error
arrives, it crafts a new request to the next provider with the partial assistant turn
injected as a new assistant message and a system/user note asking the model to
"continue from: …". The output stream from the fallback is appended seamlessly.

Consumer responsibility: none — the stream appears continuous.
Use-case fit: best for most agentic and chat consumers; harder to implement correctly
(prompt engineering, token counting, tool-call state reconstruction).

### Option C — Silent restart with no-content guard (current scope, simplest)

Only allow failover if **zero content events have been forwarded**. If content was
emitted, forward the error to the consumer as today.

This is the current behaviour after the fix in this session. It is the safe baseline
that costs nothing to implement.

**Recommended: implement Option A first (it is simpler than B and more transparent
than C). Option B can follow if consumers need seamless output.**

---

## File Map

### New files
- `llm/stream.go` — add `StreamEventRetry` event type and `StreamRetry` payload struct

### Modified files
- `provider/router/router.go` — track whether content has been forwarded in `pipeStream`;
  emit `StreamEventRetry` and connect to fallback when error arrives post-content
- `provider/router/router_test.go` — tests for mid-content failover
- `stream_response.go` — `Process()` / `StreamResponse` need to handle `StreamEventRetry`
  gracefully (ignore or surface to the handler)
- `llmtest/events.go` — add `RetryEvent()` helper for tests

### Deleted files
None.

---

## Risk Register

| Risk | Impact | Mitigation |
|---|---|---|
| Consumer receives two `StreamEventStart` events | Medium — most consumers expect exactly one | Document the contract; update `StreamResponse.Process()` to handle it |
| Tool-call state is split across two providers | High — a `content_block_start` for a tool on prov1 will never get its `content_block_stop` | Guard: only failover if **no tool delta has been emitted**; if a tool is in flight, surface the error |
| Reasoning/thinking blocks partially emitted | Medium — same as tool-call state problem | Include reasoning deltas in the "content emitted" guard |
| Duplicate `StreamEventDone` | Low — second `Done` from fallback, first never arrives | Only one `Done` is ever forwarded; router suppresses the first partial `Done` if it arrives |
| `StreamRetry` breaks existing consumers that do `switch evt.Type` | Low — unknown types are ignored by most range-over loops | Add `StreamEventRetry` as a new constant; consumers that don't handle it silently skip it |
| Restart increases cost (full prompt re-sent) | Low — user is paying for the overloaded provider's tokens anyway | Log a warning in the Routed event |

---

## Tasks

### Task 1 — Add `StreamEventRetry` / `StreamRetry` to `stream.go`

**Files:** `llm/stream.go`

**Steps:**
1. Add `StreamEventRetry StreamEventType = "retry"` to the const block.
2. Add `StreamRetry` struct:
   ```go
   type StreamRetry struct {
       // FailedProvider is the provider that emitted the retriable error.
       FailedProvider string
       // FallbackProvider is the provider the router is switching to.
       FallbackProvider string
       // Reason is the original error from the failed provider.
       Reason *ProviderError
       // TokensEmitted is the number of text/reasoning delta events forwarded
       // before the retry. Useful for consumers that want to truncate display.
       TokensEmitted int
   }
   ```
3. Add `Retry *StreamRetry` field to `StreamEvent`.
4. Add `es.Retry(r StreamRetry)` helper to `EventStream`.

**Verify:** `go build ./...` → exit 0

---

### Task 2 — Track forwarded-content state in `pipeStream`

**Files:** `provider/router/router.go`

**Steps:**
1. Add a `contentEmitted bool` local variable in `pipeStream` (and the inner failover
   loop), set to `true` when the first `StreamEventDelta` of type `DeltaTypeText`
   or `DeltaTypeReasoning` is forwarded, OR when any `StreamEventToolCall` is forwarded.
2. On retriable in-stream error:
   - If `!contentEmitted`: current behaviour — silent failover, no Retry event.
   - If `contentEmitted`: emit `StreamEventRetry`, connect to next target, continue
     piping (Option A). If no remaining targets: surface error as today.
3. Guard: if a `DeltaTypeTool` (partial tool argument stream) has been seen but
   `StreamEventToolCall` has NOT been emitted yet (tool in-flight), do **not** failover —
   surface the error. Track with an `toolInFlight bool`.

**Verify:** `go test ./provider/router/` → all tests pass

---

### Task 3 — Tests for post-content failover

**Files:** `provider/router/router_test.go`

**Steps:**
1. Add `TestCreateStream_MidStreamFailover/failover_after_content_emitted`: prov1 emits
   two text deltas then an Overloaded error; assert prov2 is called, `StreamEventRetry`
   is present, prov2's deltas are forwarded.
2. Add `TestCreateStream_MidStreamFailover/no_failover_during_tool_stream`: prov1 emits
   a `ToolDelta` then an Overloaded error; assert error is forwarded, prov2 is not called.
3. Add `TestCreateStream_MidStreamFailover/all_fallbacks_exhausted_post_content`: prov1
   emits content then Overloaded; prov2 also errors with Overloaded; assert final error
   event is `ErrNoProviders`.

**Verify:** `go test -v -run TestCreateStream_MidStreamFailover ./provider/router/` → all pass

---

### Task 4 — Update `StreamResponse.Process()` to handle `StreamEventRetry`

**Files:** `stream_response.go`

**Steps:**
1. In the main event dispatch loop inside `Process()`, add a case for
   `StreamEventRetry`: call any registered `RetryHandler` if set, otherwise ignore.
2. Add `OnRetry(func(StreamRetry))` option to `StreamResponse` builder.

**Verify:** `go test ./...` (unit tests only, skip integration) → exit 0

---

### Task 5 — Add `RetryEvent` test helper

**Files:** `llmtest/events.go`

**Steps:**
1. Add `RetryEvent(failed, fallback string, reason *ProviderError) StreamEvent`
   helper alongside existing helpers.

**Verify:** `go build ./llmtest/` → exit 0

---

### Task 6 — Documentation and CHANGELOG

**Files:** `CHANGELOG.md`

**Steps:**
1. Add an entry under the next unreleased version describing mid-stream failover
   behaviour, the `StreamEventRetry` signal, and the guard conditions (no failover
   during in-flight tool calls).

**Verify:** manual review

---

## Open Questions

1. **Option A vs B**: Should the router attempt a continuation prompt (Option B) to
   produce a seamless stream, or is it acceptable to restart on the fallback and let
   the consumer see `StreamEventRetry`? This decision must be made before Task 2.

2. **Token accumulation for Option B**: If Option B is chosen, `pipeStream` needs to
   buffer all forwarded text deltas. How large can responses get? Is a 1 MB in-memory
   buffer acceptable, or do we need a streaming approach that builds the continuation
   prompt lazily?

3. **Reasoning blocks**: When extended thinking is active, a partial reasoning block
   before the error makes the continuation prompt complex. Should reasoning blocks be
   treated like tool-in-flight (no failover) or dropped (restart the thinking on
   the fallback)?

4. **`StreamResponse.Process()` retry callback**: Is an `OnRetry` handler the right
   API, or should consumers just check for `StreamEventRetry` in a raw event loop?
