# Plan: Improve test coverage for provider/anthropic

## Problem Statement

The `provider/anthropic` core package is at 67.3% statement coverage with several
production-critical paths at 0%: the entire `CreateStream` HTTP lifecycle, the
`parseStream` error branches (context cancellation, read errors), and the
`CalculateCost` function. A secondary set of gaps exists in `BuildRequest`
(reasoning effort, JSON output format, MaxTokens fallback) and `dispatch`
(malformed/empty/unknown SSE events). The `claude` sub-package sits at 58.2%
with all of `buildRequest`, `normalizeModel`, and the pure utility functions
untested despite requiring no network access.

The goal is to bring the core package to ≥85% and the `claude` sub-package to
≥70%, closing every HIGH and MEDIUM risk gap identified in the coverage review.

---

## File Map

**New files:**
- `provider/anthropic/create_stream_test.go` — HTTP-level tests for `CreateStream`
- `provider/anthropic/stream_error_test.go` — `parseStream` error path tests
- `provider/anthropic/models_test.go` — `CalculateCost` and `matchPricingByPrefix` tests
- `provider/anthropic/dispatch_test.go` — `dispatch` defensive-branch tests
- `provider/anthropic/request_extra_test.go` — `BuildRequest` gaps (reasoning, JSON output, MaxTokens, helpers)
- `provider/anthropic/claude/provider_unit_test.go` — pure-function tests for `buildRequest`, `normalizeModel`, `stainlessOS/Arch`

**Modified files:** none (no production code changes required)

**Deleted files:** none

---

## Risk Register

| Risk | Impact | Mitigation |
|---|---|---|
| `httptest` server races with stream goroutine | Low | Use `t.Cleanup` to close server; drain stream channel in test |
| `parseStream` context-cancel test is timing-sensitive | Low | Use `io.Pipe` with a controlled write side; cancel ctx before any write |
| Tests import internal package symbols (`parseStream`) | None | All test files use `package anthropic` (same package) |
| `claude` tests need `buildRequest` which is unexported | None | `claude/provider_unit_test.go` uses `package claude` (same package) |
| Coverage target may not be met if a branch is in dead generated code | Low | Re-run `go tool cover -func` after each task to verify |

---

## Tasks

### Task 1 — `CreateStream` HTTP lifecycle tests

**Files:** `provider/anthropic/create_stream_test.go`

**Steps:**
1. Create the file with `package anthropic`.
2. Write `TestCreateStream_MissingAPIKey`: construct `New()` with no key, call
   `CreateStream` with a minimal valid request, assert `llm.ErrMissingAPIKey`.
3. Write `TestCreateStream_ValidateError`: pass an `llm.Request{}` with no model/messages, assert `llm.ErrBuildRequest`.
4. Write `TestCreateStream_NonOKResponse`: start an `httptest.NewServer` that
   returns `429` with a body, call `CreateStream`, assert `llm.ErrAPIError` with
   status 429 in the message.
5. Write `TestCreateStream_NetworkError`: use an `llm.WithHTTPClient` that has a
   transport which always returns an error, assert `llm.ErrRequestFailed`.
6. Write `TestCreateStream_HappyPath`: start an `httptest.NewServer` that serves
   a minimal valid Anthropic SSE response (one text delta + message_stop),
   call `CreateStream`, drain the channel, assert a text delta event is received.

**Verify:** `go test -v -run TestCreateStream ./provider/anthropic/` → all pass, `CreateStream` at 100%

---

### Task 2 — `parseStream` error path tests

**Files:** `provider/anthropic/stream_error_test.go`

**Steps:**
1. Create the file with `package anthropic`.
2. Write `TestParseStream_ContextCancellation`:
   - Create an `io.Pipe`; cancel the context *before* writing anything to the pipe.
   - Call `ParseStream` with the cancelled context and the pipe reader.
   - Drain the channel; assert a `StreamEventError` envelope is present and
     `errors.Is(err, context.Canceled)` or the error string contains "context".
3. Write `TestParseStream_ReadError`:
   - Use an `io.NopCloser` wrapping a reader that returns `io.ErrUnexpectedEOF`
     after one successful SSE event (use `BuildSSEBody` for the first event,
     then splice in a broken reader).
   - Alternatively: use a `failReader` that immediately returns an error.
   - Drain the channel; assert a `StreamEventError` envelope is present with
     a `ErrStreamRead`-style error.

**Verify:** `go test -v -run TestParseStream_Context\|TestParseStream_Read ./provider/anthropic/` → both pass, `parseStream` branches at 100%

---

### Task 3 — `dispatch` defensive-branch tests

**Files:** `provider/anthropic/dispatch_test.go`

**Steps:**
1. Create the file with `package anthropic`.
2. Write `TestDispatch_EmptyDataLine`: use `NewHarness` and call
   `h.proc.dispatch("")` directly; assert it returns `true` (stream continues)
   and no event is emitted.
3. Write `TestDispatch_MalformedJSON`: call `h.proc.dispatch("not json")`;
   assert returns `true`, no error envelope emitted.
4. Write `TestDispatch_UnknownEventType`: call
   `h.proc.dispatch(`{"type":"ping"}`)`;
   assert returns `true`, no error envelope emitted.
   
   Note: `proc` is unexported; since this is `package anthropic` (same package)
   it is accessible via `h.proc`.

**Verify:** `go test -v -run TestDispatch ./provider/anthropic/` → all pass, `dispatch` at 100%

---

### Task 4 — `CalculateCost` and pricing tests

**Files:** `provider/anthropic/models_test.go`

**Steps:**
1. Create the file with `package anthropic`.
2. Write `TestCalculateCost_NilUsage`: assert `CalculateCost("any", nil) == 0`.
3. Write `TestCalculateCost_UnknownModel`: assert `CalculateCost("gpt-4", &llm.Usage{InputTokens: 100}) == 0`.
4. Write `TestCalculateCost_KnownModel`: use `"claude-sonnet-4-5-20250929"`,
   set `InputTokens=1_000_000`, `OutputTokens=1_000_000`, assert exact USD values
   matching the known pricing row (input $3.00/M, output $15.00/M).
5. Write `TestCalculateCost_NegativeRegularInput`: set `InputTokens=10`,
   `CacheReadTokens=20` (more than InputTokens) — regularInput would be negative;
   assert cost is not negative (clamp to 0 fires).
6. Write `TestCalculateCost_PrefixFallback`: use a dated variant not in the
   registry (e.g. `"claude-sonnet-4-5-20991231"`) — should match prefix and
   return non-zero cost.
7. Write `TestMatchPricingByPrefix_NoMatch`: call `matchPricingByPrefix("unknown-model")`,
   assert `ok == false`.

**Verify:** `go test -v -run TestCalculateCost\|TestMatchPricing ./provider/anthropic/` → all pass, `models.go` ≥ 90%

---

### Task 5 — `BuildRequest` gap tests

**Files:** `provider/anthropic/request_extra_test.go`

**Steps:**
1. Create the file with `package anthropic`.
2. Write `TestBuildRequest_ReasoningEffort` with subtests for `low`, `medium`,
   `high`: unmarshal the body, assert `thinking.budget_tokens` equals 1024, 5000,
   16000 respectively.
3. Write `TestBuildRequest_ReasoningEffort_ForcedToolChoiceDowngrade`: set
   `ReasoningEffort = llm.ReasoningEffortHigh` and `ToolChoice = llm.ToolChoiceTool{Name:"fn"}`;
   assert `tool_choice.type == "auto"` (forced choice is downgraded).
4. Write `TestBuildRequest_OutputFormatJSON`: set `OutputFormat = llm.OutputFormatJSON`;
   assert `output_config.format.type == "json_schema"` in the body.
5. Write `TestBuildRequest_MaxTokensFallback` with three subtests:
   - `RequestOptions.MaxTokens` set → used as-is
   - Only `StreamOptions.MaxTokens` set → used
   - Neither set → defaults to 16384
6. Write `TestPrependSystemBlocks` and `TestNewSystemBlock` for the two uncovered
   helper functions.
7. Write `TestFindPrecedingAssistant_NoPreceding`: call with a slice that has no
   assistant message before the given index; assert returns `nil`.

**Verify:** `go test -v -run TestBuildRequest_Reason\|TestBuildRequest_Output\|TestBuildRequest_Max\|TestPrepend\|TestNewSystem\|TestFindPreceding ./provider/anthropic/` → all pass, `request.go` ≥ 90%

---

### Task 6 — `claude` sub-package pure-function tests

**Files:** `provider/anthropic/claude/provider_unit_test.go`

**Steps:**
1. Create the file with `package claude`.
2. Write `TestBuildRequest_PrependsBillingHeaders`: call `p.buildRequest` on a
   provider built with `New(WithManagedTokenProvider(stubProvider))`, passing a
   simple `llm.Request`; unmarshal the body; assert the `system` array starts
   with the billing header block and system core blocks.
3. Write `TestNormalizeModel_Known`: test that a known short alias (e.g.
   `"claude-sonnet-4-6"`) returns itself unchanged.
4. Write `TestNormalizeModel_Default`: test that `""` or `"default"` resolves to
   a non-empty model string.
5. Write `TestStainlessOS` and `TestStainlessArch`: assert they return non-empty
   strings; no panic on the current platform.

**Verify:** `go test -v -run TestBuildRequest_Prepends\|TestNormalize\|TestStainless ./provider/anthropic/claude/` → all pass, `claude/provider.go` ≥ 30% (pure functions covered)

---

### Task 7 — Coverage gate check

**Files:** none (verification only)

**Steps:**
1. Run full coverage for both packages.
2. Confirm core package ≥ 85%, claude sub-package ≥ 70%.
3. Run with race detector to catch any goroutine issues introduced by the new tests.

**Verify:**
```
go test -race -coverprofile=coverage.out ./provider/anthropic/...
go tool cover -func=coverage.out | grep -E "^total|anthropic\.go|stream\.go|models\.go|request\.go|stream_processor\.go"
```
→ all target files at or above threshold, race detector clean.

---

## Open Questions

None.
