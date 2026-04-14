# Plan: Fix all review defects in provider/anthropic

## Problem Statement

The full code review of `provider/anthropic` identified 4 red defects, 6 yellow
concerns, and 3 green minors. One is also an active regression: the test suite
deadlocks at `TestParseStream_ContextCancellation` because `sse.ForEachDataLine`
does not honour context cancellation during a blocking `scanner.Scan()` read.
This plan fixes every item in severity order, with one structural refactor
(prefix pricing) grouped with its test impact.

---

## File Map

**Modified:**
- `provider/internal/sse/lines.go` — fix context cancellation during blocking read
- `provider/anthropic/stream_error_test.go` — fix test now that SSE layer is context-aware
- `provider/anthropic/models.go` — eliminate duplicate pricing table; derive prefix list from registry; fix comments
- `provider/anthropic/stream_processor.go` — single `[]byte(data)` allocation per dispatch; improve dispatch comment
- `provider/anthropic/request.go` — add `CacheControl` to `toolUseBlock`; drop inline anonymous struct; collapse `hasPerMessageCacheHints` type-switch
- `provider/anthropic/claude/provider.go` — store token-provider init errors; surface them in `CreateStream`; panic on `crypto/rand` failure; deduplicate `anthropicVersion`

**New:**
- `provider/internal/sse/lines_test.go` — test context cancellation during blocking read

**Deleted:** none

---

## Risk Register

| Risk | Impact | Mitigation |
|---|---|---|
| SSE context fix changes `ForEachDataLine` signature or behaviour for non-cancel paths | High | Add test that verifies normal (non-cancel) streaming still works; existing provider tests are the regression guard |
| `matchPricingByPrefix` rebuild changes fallback lookup order | Medium | New test asserts same results for all existing prefix cases before and after; keep longest-prefix-first sort order |
| Adding `CacheControl` to `toolUseBlock` changes JSON output for cached assistant tool calls | Low | Existing cache tests cover this path; `omitempty` ensures no change for the non-cache case |
| Storing init error on Provider leaks to `Name()` / `Models()` callers | None | Error is only surfaced in `CreateStream`, not in other methods |
| `cacheHinter` interface in `hasPerMessageCacheHints` requires `CacheHint()` to be on the interface | Low | Verify `llm.Message` subtypes all implement it before collapsing; if not, keep the switch |

---

## Tasks

### Task 1 — Fix context cancellation in `sse.ForEachDataLine`

**Files:** `provider/internal/sse/lines.go`, `provider/internal/sse/lines_test.go`

**Problem:** The `select { case <-ctx.Done(): default: }` check fires only between
`scanner.Scan()` calls. When the pipe has no data, `Scan()` blocks indefinitely
and the context is never observed, causing `TestParseStream_ContextCancellation`
to deadlock.

**Fix:** Replace the buffered scanner with a context-aware read loop. Use
`io.Pipe` internally (or simply check `ctx.Done()` in a select with a goroutine
that reads from `r` and forwards to a channel). The simplest correct approach:
run `scanner.Scan()` in a goroutine; send results on a channel; select on both
the channel and `ctx.Done()` in the main loop.

**Steps:**
1. Rewrite `ForEachDataLine` to run the scanner in a background goroutine that
   sends `(line string, err error)` pairs on a channel. The outer loop selects
   on the channel and `ctx.Done()`.
2. Add `provider/internal/sse/lines_test.go` with:
   - `TestForEachDataLine_Normal`: feeds valid SSE lines, asserts all are received.
   - `TestForEachDataLine_ContextCancellation`: cancels ctx while scanner is blocked
     on a pipe with no data; asserts the function returns `context.Canceled` promptly
     (within 100ms).
   - `TestForEachDataLine_ReadError`: feeds a reader that returns an error; asserts
     the error is returned.

**Verify:** `go test -timeout 10s ./provider/internal/sse/...` → all pass, no deadlock

---

### Task 2 — Fix `TestParseStream_ContextCancellation` now that SSE layer is fixed

**Files:** `provider/anthropic/stream_error_test.go`

**Problem:** With Task 1 done, the pipe-close-on-cancel goroutine trick is no
longer needed. The test can be simplified: cancel the context, start
`ParseStream` with a pipe that has no writer activity, and the stream should
terminate with a context-cancelled error without any manual pipe manipulation.

**Steps:**
1. Remove the background goroutine that calls `pw.CloseWithError`.
2. Cancel the context before calling `ParseStream`.
3. Keep the assertion unchanged (expects `ErrContextCancelled` or
   `context.Canceled` in the error chain).
4. Add `t.Cleanup(func() { pw.Close() })` to avoid pipe leak if test fails.

**Verify:** `go test -timeout 10s -run TestParseStream ./provider/anthropic/` → passes, no deadlock

---

### Task 3 — Eliminate duplicate pricing table in `matchPricingByPrefix`

**Files:** `provider/anthropic/models.go`

**Problem:** `matchPricingByPrefix` hardcodes a separate pricing slice that is
already out of sync with `modelPricingRegistry`. `claude-sonnet-4` and
`claude-opus-4-1` are missing from the prefix list; `claude-haiku-4-5` has
different pricing in the two tables.

**Fix:** Build the prefix list at package init time by iterating
`modelPricingRegistry`, extracting the undated key (strip trailing `-YYYYMMDD`),
and storing in a sorted slice (longest prefix first to prevent shorter prefixes
masking longer ones). The registry is the single source of truth.

**Steps:**
1. Add `var pricingPrefixes []prefixEntry` and `type prefixEntry struct { prefix string; pricing modelPricing }`.
2. Add an `init()` that populates `pricingPrefixes` from `modelPricingRegistry`:
   - For each key, check if the last segment is 8 digits; if so, strip it for the prefix key.
   - Deduplicate by prefix (keep the entry already seen — all dated variants of the
     same model should have identical pricing; if they differ, that's a data error
     caught by the existing registry).
   - Sort by descending prefix length.
3. Replace the hardcoded slice in `matchPricingByPrefix` with a loop over `pricingPrefixes`.
4. Fix the two stale comments: `// Model ToolCallID constants` → `// Model ID constants`;
   `OutputPrice float64 // ToolOutput tokens` → `// Output tokens`.
5. Update `TestMatchPricingByPrefix_NoMatch` and add
   `TestMatchPricingByPrefix_DerivedFromRegistry` to assert that every key in
   `modelPricingRegistry` has a working prefix fallback for a synthetic future date variant.

**Verify:** `go test -run TestCalculateCost\|TestMatchPricing\|TestFillCost ./provider/anthropic/` → all pass

---

### Task 4 — Single `[]byte` allocation per `dispatch` call

**Files:** `provider/anthropic/stream_processor.go`

**Problem:** `dispatch` converts `data string` to `[]byte` twice per call — once
for the type-sniff unmarshal and once for the typed-event unmarshal.

**Steps:**
1. Add `b := []byte(data)` at the top of `dispatch` (after the empty-string guard).
2. Replace all `json.Unmarshal([]byte(data), ...)` calls with `json.Unmarshal(b, ...)`.
3. Add a clarifying comment on the `"error"` case: `// error was published by
   onError above; return false to stop the loop without a second error event`.

**Verify:** `go test ./provider/anthropic/` → all pass; `go vet ./provider/anthropic/` → clean

---

### Task 5 — Add `CacheControl` to `toolUseBlock`; drop anonymous struct

**Files:** `provider/anthropic/request.go`

**Problem:** `BuildRequest` uses an inline anonymous struct when appending a
cached assistant tool-use block. `toolUseBlock` exists for this purpose but
lacks a `CacheControl` field.

**Steps:**
1. Add `CacheControl *cacheControl \`json:"cache_control,omitempty"\`` to `toolUseBlock`.
2. In `BuildRequest`, replace the anonymous struct literal (lines 240–246) with:
   ```go
   tub.CacheControl = buildCacheControl(m.CacheHint())
   blocks = append(blocks, tub)
   ```
3. Verify the existing cache hint tests still pass.

**Verify:** `go test -run TestBuildRequest ./provider/anthropic/` → all pass

---

### Task 6 — Collapse `hasPerMessageCacheHints` type-switch

**Files:** `provider/anthropic/request.go`

**Problem:** Four identical branches in a type-switch — all call `CacheHint()`
and check `Enabled`. If `llm.Message` subtypes all implement `CacheHint()`, an
interface assertion collapses this to 3 lines.

**Steps:**
1. Check whether `llm.SystemMessage`, `llm.UserMessage`, `llm.AssistantMessage`,
   and `llm.ToolMessage` all expose `CacheHint() *llm.CacheHint` — confirm via
   `grep` on the `llm` package.
2. If yes: define a local `cacheHinter` interface and rewrite `hasPerMessageCacheHints`
   to use a single `msg.(cacheHinter)` assertion.
3. If no (partial coverage): keep the switch for types that have it, drop the
   unreachable cases.

**Verify:** `go test -run TestBuildRequest\|TestCache ./provider/anthropic/` → all pass

---

### Task 7 — Store token-provider init errors; fix `WithLocalTokenProvider` / `WithClaudeDir`

**Files:** `provider/anthropic/claude/provider.go`

**Problem:** When `NewLocalTokenProvider()` or `NewLocalTokenProviderWithDir()`
fails, the error is silently dropped and `tokenProvider` stays nil. `CreateStream`
then reports `ErrMissingAPIKey` — misleading for a credentials file problem.

**Steps:**
1. Add `initErr error` field to `Provider`.
2. In `WithLocalTokenProvider` and `WithClaudeDir`, on error: set `p.initErr`
   instead of returning silently. Keep `p.tokenProvider` nil.
3. In `CreateStream`, add a check at the top:
   ```go
   if p.initErr != nil {
       return nil, llm.NewErrProviderMsg(providerName, p.initErr.Error())
   }
   ```
4. Update `TestBuildRequest_PrependsBillingAndSystemBlocks` in
   `claude/provider_unit_test.go` if it sets `tokenProvider` directly (it does —
   no change needed since it bypasses `WithLocalTokenProvider`).

**Verify:** `go test ./provider/anthropic/claude/...` → all pass

---

### Task 8 — Panic on `crypto/rand` failure; deduplicate `anthropicVersion`

**Files:** `provider/anthropic/claude/provider.go`, `provider/anthropic/anthropic.go`

**Steps:**
1. In `randomUUID`: replace `_, _ = rand.Read(b[:])` with:
   ```go
   if _, err := rand.Read(b[:]); err != nil {
       panic("anthropic: crypto/rand unavailable: " + err.Error())
   }
   ```
2. Export `AnthropicVersion = "2023-06-01"` from `provider/anthropic/anthropic.go`
   (rename from unexported `anthropicVersion`).
3. In `claude/provider.go` `newAPIRequest`: replace the hardcoded string with
   `anthropic.AnthropicVersion`.
4. Update `TestNewAPIRequestHeaders` in `anthropic_test.go` to reference
   `AnthropicVersion` (or the string directly — either is fine).

**Verify:** `go build ./provider/anthropic/...` → clean; `go test ./provider/anthropic/...` → all pass

---

### Task 9 — Decide on `stream_harness.go` visibility

**Files:** `provider/anthropic/stream_harness.go` (and potentially a new
`provider/anthropic/anthropictest/` sub-package)

**Problem:** `Harness`, `NewHarness`, and `BuildSSEBody` are exported from
`package anthropic`, adding test scaffolding to the production API surface and
binary size of every consumer.

**Decision required before acting:**
- Option A: Move to a new `provider/anthropic/anthropictest` package. External
  consumers (minimax tests) import `anthropictest` explicitly.
- Option B: Suffix the file `stream_harness_test.go` and keep unexported. Only
  works if `Harness` is only used within `package anthropic` tests.

Check: `grep -r "anthropic\.NewHarness\|anthropic\.BuildSSEBody" --include="*.go" .`
to find all external callers. If only minimax uses them, Option A is correct.

**Steps:**
1. Run the grep to determine all callers outside `provider/anthropic`.
2. If only minimax: create `provider/anthropic/anthropictest/harness.go` with
   `package anthropictest`, move `Harness`, `NewHarness`, `BuildSSEBody`,
   `sseWithTypeField` there; update minimax import.
3. If no external callers at all: rename to `stream_harness_test.go` and make
   types unexported (`harness`, `newHarness`, `buildSSEBody`); update all
   internal test files.
4. Keep the `stream_harness.go` file deleted from the main package in either case.

**Verify:** `go build ./...` → clean (no unexported-symbol-in-test-binary errors)

---

### Task 10 — Full verification gate

**Files:** none (verification only)

**Steps:**
1. `go test -race -timeout 30s ./provider/anthropic/... ./provider/internal/sse/...`
2. `go vet ./...`
3. `go build ./...`
4. Re-run coverage: `go test -coverprofile=coverage.out ./provider/anthropic/... && go tool cover -func=coverage.out | grep total`
   → core package must remain ≥ 85%.

**Verify:** All commands exit 0, no race conditions, coverage target met.

---

## Open Questions

None. All decisions are resolvable from the codebase without user input except
Task 9's caller check — which is itself a step in that task.
